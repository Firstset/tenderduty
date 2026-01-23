package tenderduty

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	github_com_cosmos_cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashing "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	dash "github.com/firstset/tenderduty/v2/td2/dashboard"
	utils "github.com/firstset/tenderduty/v2/td2/utils"
	"github.com/go-yaml/yaml"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

const (
	showBLocks = 512
	staleHours = 24
)

func SeverityThresholdToSeverities(threhold string) []string {
	severities := []string{}

	switch strings.ToLower(threhold) {
	case "critical":
		severities = append(severities, "critical")
	case "warning":
		severities = append(severities, "critical", "warning")
	case "info":
		severities = append(severities, "critical", "warning", "info")
	default:
		severities = append(severities, "critical", "warning", "info")
	}

	return severities
}

// applyAlertDefaults copies zero-value fields from src to dst recursively.
func applyAlertDefaults(dst, src any) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for i := 0; i < dv.NumField(); i++ {
		df := dv.Field(i)
		sf := sv.Field(i)
		if !df.CanSet() {
			continue
		}

		switch df.Kind() {
		case reflect.Struct:
			applyAlertDefaults(df.Addr().Interface(), sf.Addr().Interface())
		case reflect.Pointer:
			if df.IsNil() {
				df.Set(sf)
			} else if df.Elem().Kind() == reflect.Struct && !sf.IsNil() {
				applyAlertDefaults(df.Interface(), sf.Interface())
			}
		default:
			if isZero(df) {
				df.Set(sf)
			}
		}
	}
}

func isZero(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

func boolVal(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

func intVal(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func floatVal(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// Config holds both the settings for tenderduty to monitor and state information while running.
type Config struct {
	alertChan           chan *alertMsg // channel used for outgoing notifications
	updateChan          chan *dash.ChainStatus
	logChan             chan dash.LogMessage
	statsChan           chan *promUpdate
	ctx                 context.Context
	cancel              context.CancelFunc
	alarms              *alarmCache
	coinMarketCapClient *utils.CoinMarketCapClient
	tenderdutyCache     *utils.TenderdutyCache // used for caching different kinds of data in memory, such as bank metadata quried from our GitHub repo

	// EnableDash enables the web dashboard
	EnableDash bool `yaml:"enable_dashboard"`
	// Listen is the URL for the dashboard to listen on, must be a valid/parsable URL
	Listen string `yaml:"listen_port"`
	// HideLogs controls whether logs are sent to the dashboard. It will also suppress many alarm details.
	// This is useful if the dashboard will be public.
	HideLogs bool `yaml:"hide_logs"`

	// NodeDownMin controls how long we wait before sending an alert that a node is not responding or has
	// fallen behind.
	NodeDownMin int `yaml:"node_down_alert_minutes"`
	// NodeDownSeverity controls the Pagerduty severity when notifying if a node is down.
	NodeDownSeverity string `yaml:"node_down_alert_severity"`

	// whether skip the TLS verification
	TLSSkipVerify bool `yaml:"tls_skip_verify"`

	// Prom controls if the prometheus exporter is enabled.
	Prom bool `yaml:"prometheus_enabled"`
	// PrometheusListenPort is the port number used by the prometheus web server
	PrometheusListenPort int `yaml:"prometheus_listen_port"`

	// DefaultAlertConfig defines the default alert settings which can be
	// overridden on a per chain basis in the `alerts` section.
	DefaultAlertConfig AlertConfig `yaml:"default_alert_config"`
	// Healthcheck information
	Healthcheck HealthcheckConfig `yaml:"healthcheck"`

	// When GovernanceAlerts is true, GovernanceAlertsReminderInterval defines how often to remind the user about unvoted proposals, every 6 hours by default
	GovernanceAlertsReminderInterval int `yaml:"governance_alerts_reminder_interval"`

	CoinMarketCapAPIToken string                `yaml:"coin_market_cap_api_token"`
	PriceConversion       PriceConversionConfig `yaml:"convert_to_fiat"`

	chainsMux sync.RWMutex // prevents concurrent map access for Chains
	// Chains has settings for each validator to monitor. The map's name does not need to match the chain-id.
	Chains map[string]*ChainConfig `yaml:"chains"`
}

// savedState is dumped to a JSON file at exit time, and is loaded at start. If successful it will prevent
// duplicate alerts, and will show old blocks in the dashboard.
type savedState struct {
	Alarms    *alarmCache                     `json:"alarms"`
	Blocks    map[string][]int                `json:"blocks"`
	NodesDown map[string]map[string]time.Time `json:"nodes_down"`
}

type ProviderConfig struct {
	Name    string         `yaml:"name"`
	Configs map[string]any `yaml:"configs"`
}

// ChainConfig represents a validator to be monitored on a chain, it is somewhat of a misnomer since multiple
// validators can be monitored on a single chain.
type ChainConfig struct {
	name              string
	wsclient          *TmConn            // custom websocket client to work around wss:// bugs in tendermint
	client            *rpchttp.HTTP      // legit tendermint client
	noNodes           bool               // tracks if all nodes are down
	valInfo           *ValInfo           // recent validator state, only refreshed every few minutes
	lastValInfo       *ValInfo           // use for detecting newly-jailed/tombstone
	totalBondedTokens float64            // total bonded tokens on the chain
	totalSupply       float64            // total supply of the chain, used for calculating APR
	communityTax      float64            // community tax rate, used for calculating APR
	inflationRate     float64            // inflation rate of the chain, used for calculating APR
	baseAPR           float64            // the base APR of a chain
	denomMetadata     *bank.Metadata     // chain denom metadata
	cryptoPrice            *utils.CryptoPrice        // coin price in a fiat currency
	cosmosDirectoryData    *CosmosDirectoryChainData // cached chain data from cosmos.directory

	minSignedPerWindow      float64 // instantly see the validator risk level
	blocksResults           []int
	lastError               string
	lastBlockTime           time.Time
	lastBlockAlarm          bool
	lastBlockNum            int64
	activeAlerts            int
	unvotedOpenGovProposals []gov.Proposal // the open proposals that the validator has not voted on

	statTotalSigns       float64
	statTotalProps       float64
	statTotalMiss        float64
	statPrevoteMiss      float64
	statPrecommitMiss    float64
	statConsecutiveMiss  float64
	statTotalPropsEmpty  float64
	statConsecutiveEmpty float64

	// ChainId is used to ensure any endpoints contacted claim to be on the correct chain. This is a weak verification,
	// no light client validation is performed, so caution is advised when using public endpoints.
	ChainId string `yaml:"chain_id"`
	// ValAddress is the validator operator address to be monitored. Tenderduty v1 required the consensus address,
	// this is no longer needed. The operator address is much easier to find in explorers etc.
	ValAddress string `yaml:"valoper_address"`
	// ValconsOverride allows skipping the lookup of the consensus public key and setting it directly.
	ValconsOverride string `yaml:"valcons_override"`
	// ExtraInfo will be appended to the alert data. This is useful for pagerduty because multiple tenderduty instances
	// can be pointed at pagerduty and duplicate alerts will be filtered by using a key. The first alert will win, this
	// can be useful for knowing what tenderduty instance sent the alert.
	ExtraInfo string `yaml:"extra_info"` // FIXME not used yet!
	// Alerts defines the types of alerts to send for this chain.
	Alerts AlertConfig `yaml:"alerts"`
	// PublicFallback determines if tenderduty should attempt to use public RPC endpoints in the situation that not
	// explicitly defined RPC servers are available. Not recommended.
	PublicFallback bool `yaml:"public_fallback"`
	// Nodes defines what RPC servers to connect to.
	Nodes []*NodeConfig `yaml:"nodes"`
	// Provider defines what implementation should be used for checking a chain's status
	// currently it supports two values: `default` or `namada`
	Provider ProviderConfig `yaml:"provider"`
	// The name/slug of this chain, used by CoinMarketCap API to convert the price
	Slug string `yaml:"slug"`
	// The inflation rate of the chain, if specified the value overrides the query result
	InflationRateOverriding float64 `yaml:"inflationRate"`
	// ChainName is the cosmos.directory path (e.g., "babylon", "osmosis").
	// If not set, defaults to lowercase of the chain's display name.
	// Used to fetch chain params from cosmos.directory and as RPC fallback.
	ChainName string `yaml:"chain_name"`
}

// mkUpdate returns the info needed by prometheus for a gauge.
func (cc *ChainConfig) mkUpdate(t metricType, v float64, node string) *promUpdate {
	return &promUpdate{
		metric:   t,
		counter:  v,
		name:     cc.name,
		chainId:  cc.ChainId,
		moniker:  cc.valInfo.Moniker,
		endpoint: node,
	}
}

// AlertConfig defines the type of alerts to send for a ChainConfig
type AlertConfig struct {
	// How many minutes to wait before alerting that no new blocks have been seen
	Stalled *int `yaml:"stalled_minutes"`
	// Whether to alert when no new blocks are seen
	StalledAlerts *bool `yaml:"stalled_enabled"`

	// How many missed blocks are acceptable before alerting
	ConsecutiveMissed *int `yaml:"consecutive_missed"`
	// Tag for pagerduty to set the alert priority
	ConsecutivePriority string `yaml:"consecutive_priority"`
	// Whether to alert on consecutive missed blocks
	ConsecutiveAlerts *bool `yaml:"consecutive_enabled"`
	// Whether to exclude prevote/precommit misses from consecutive miss counting
	ConsecutiveMissExcludePreactions *bool `yaml:"consecutive_missed_exclude_preactions"`

	// Window is how many blocks missed as a percentage of the slashing window to trigger an alert
	Window *int `yaml:"percentage_missed"`
	// PercentagePriority is a tag for pagerduty to route on priority
	PercentagePriority string `yaml:"percentage_priority"`
	// PercentageAlerts is whether to alert on percentage based misses
	PercentageAlerts *bool `yaml:"percentage_enabled"`

	// How many consecutive empty blocks are acceptable before alerting
	ConsecutiveEmpty *int `yaml:"consecutive_empty"`
	// Tag for pagerduty to set the alert priority for empty blocks
	ConsecutiveEmptyPriority string `yaml:"consecutive_empty_priority"`
	// Whether to alert on consecutive empty blocks
	ConsecutiveEmptyAlerts *bool `yaml:"consecutive_empty_enabled"`

	// EmptyWindow is how many blocks empty as a percentage of proposed blocks since tenderduty was started to trigger an alert
	EmptyWindow *int `yaml:"empty_percentage"`
	// EmptyPercentagePriority is a tag for pagerduty to route on priority
	EmptyPercentagePriority string `yaml:"empty_percentage_priority"`
	// EmptyPercentageAlerts is whether to alert on percentage based empty blocks
	EmptyPercentageAlerts *bool `yaml:"empty_percentage_enabled"`

	// AlertIfInactive decides if tenderduty send an alert if the validator is not in the active set?
	AlertIfInactive *bool `yaml:"alert_if_inactive"`
	// AlertIfNoServers: should an alert be sent if no servers are reachable?
	AlertIfNoServers *bool `yaml:"alert_if_no_servers"`

	// Whether to alert on unvoted governance proposals
	GovernanceAlerts *bool `yaml:"governance_alerts"`

	// Whether to alert when a validator's stake change goes beyond the threshold
	StakeChangeAlerts            *bool    `yaml:"stake_change_alerts"`
	StakeChangeDropThreshold     *float64 `yaml:"stake_change_drop_threshold"`
	StakeChangeIncreaseThreshold *float64 `yaml:"stake_change_increase_threshold"`

	// Whether to alert when a validator has more than the threhold value of unclaimed rewards
	UnclaimedRewardsAlerts    *bool    `yaml:"unclaimed_rewards_alerts"`
	UnclaimedRewardsThreshold *float64 `yaml:"unclaimed_rewards_threshold_in_fiat_currency"`

	// chain specific overrides for alert destinations.
	// Pagerduty configuration values
	Pagerduty PDConfig `yaml:"pagerduty"`
	// Discord webhook information
	Discord DiscordConfig `yaml:"discord"`
	// Telegram webhook information
	Telegram TeleConfig `yaml:"telegram"`
	// Slack webhook information
	Slack SlackConfig `yaml:"slack"`
	// Generic webhook information
	Webhook WebhookConfig `yaml:"webhook"`
}

// NodeConfig holds the basic information for a node to connect to.
type NodeConfig struct {
	Url         string `yaml:"url"`
	AlertIfDown bool   `yaml:"alert_if_down"`

	down      bool
	wasDown   bool
	syncing   bool
	lastMsg   string
	downSince time.Time
}

// PDConfig is the information required to send alerts to PagerDuty
type PDConfig struct {
	Enabled           *bool  `yaml:"enabled"`
	ApiKey            string `yaml:"api_key"`
	DefaultSeverity   string `yaml:"default_severity"`
	SeverityThreshold string `yaml:"severity_threshold"`
}

// DiscordConfig holds the information needed to publish to a Discord webhook for sending alerts
type DiscordConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	Webhook           string   `yaml:"webhook"`
	Mentions          []string `yaml:"mentions"`
	SeverityThreshold string   `yaml:"severity_threshold"`
}

// TeleConfig holds the information needed to publish to a Telegram webhook for sending alerts
type TeleConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	ApiKey            string   `yaml:"api_key"`
	Channel           string   `yaml:"channel"`
	Mentions          []string `yaml:"mentions"`
	SeverityThreshold string   `yaml:"severity_threshold"`
}

// SlackConfig holds the information needed to publish to a Slack webhook for sending alerts
type SlackConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	Webhook           string   `yaml:"webhook"`
	Mentions          []string `yaml:"mentions"`
	SeverityThreshold string   `yaml:"severity_threshold"`
}

// WebhookConfig holds the information needed to send alerts to a generic webhook endpoint
// The payload follows a Grafana-like format for broad compatibility
type WebhookConfig struct {
	Enabled               *bool  `yaml:"enabled"`
	URL                   string `yaml:"url"`
	SeverityThreshold     string `yaml:"severity_threshold"`
	DisableResolveMessage *bool  `yaml:"disable_resolve_message"`
}

// HealthcheckConfig holds the information needed to send pings to a healthcheck endpoint
type HealthcheckConfig struct {
	Enabled  bool          `yaml:"enabled"`
	PingURL  string        `yaml:"ping_url"`
	PingRate time.Duration `yaml:"ping_rate"`
}

type PriceConversionConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Currency        string `yaml:"currency"`
	CacheExpiration int    `yaml:"cache_expiration"`
}

// validateConfig is a non-exhaustive check for common problems with the configuration. Needs love.
func validateConfig(c *Config) (fatal bool, problems []string) {
	problems = make([]string, 0)
	var err error

	if c.EnableDash {
		_, err = url.Parse(c.Listen)
		if err != nil {
			fatal = true
			problems = append(problems, fmt.Sprintf("error: The listen URL %s does not appear to be valid", c.Listen))
		}
	}

	if boolVal(c.DefaultAlertConfig.Pagerduty.Enabled) {
		rex := regexp.MustCompile(`[+_-]`)
		if rex.MatchString(c.DefaultAlertConfig.Pagerduty.ApiKey) {
			fatal = true
			problems = append(problems, "error: The Pagerduty key provided appears to be an Oauth token, not a V2 Events API key.")
		}
	}

	if c.NodeDownMin < 3 {
		problems = append(problems, "warning: setting 'node_down_alert_minutes' to less than three minutes might result in false alarms")
	}

	// when undefined, or invalid, we set 6 as the default value
	if c.GovernanceAlertsReminderInterval <= 0 {
		c.GovernanceAlertsReminderInterval = 6
	}

	var wantsPublic bool
	for k, v := range c.Chains {
		if v.blocksResults == nil {
			v.blocksResults = make([]int, showBLocks)
			for i := range v.blocksResults {
				v.blocksResults[i] = -1
			}
		}
		if v.name == "" {
			v.name = k
		}
		if v.PublicFallback {
			wantsPublic = true
		}

		v.valInfo = &ValInfo{Moniker: "not connected"}

		applyAlertDefaults(&v.Alerts, &c.DefaultAlertConfig)

		if td.EnableDash {
			td.updateChan <- &dash.ChainStatus{
				MsgType:                 "status",
				Name:                    v.name,
				ChainId:                 v.ChainId,
				Moniker:                 v.valInfo.Moniker,
				Bonded:                  v.valInfo.Bonded,
				Jailed:                  v.valInfo.Jailed,
				Tombstoned:              v.valInfo.Tombstoned,
				Missed:                  v.valInfo.Missed,
				MinSignedPerWindow:      v.minSignedPerWindow,
				Window:                  v.valInfo.Window,
				Nodes:                   len(v.Nodes),
				HealthyNodes:            0,
				ActiveAlerts:            0,
				Blocks:                  v.blocksResults,
				UnvotedOpenGovProposals: len(v.unvotedOpenGovProposals),
				TotalBondedTokens:       v.totalBondedTokens,
				TotalSupply:             v.totalSupply,
				CommunityTax:            v.communityTax,
				InflationRate:           v.inflationRate,
				BaseAPR:                 v.baseAPR,
				VotingPowerPercent:      v.valInfo.VotingPowerPercent,
				DelegatedTokens:         v.valInfo.DelegatedTokens,
				CommissionRate:          v.valInfo.CommissionRate,
				ValidatorAPR:            v.valInfo.ValidatorAPR,
				SelfDelegationRewards:   v.valInfo.SelfDelegationRewards,
				Commission:              v.valInfo.Commission,
				CryptoPrice:             v.cryptoPrice,
				DenomMetadata:           v.denomMetadata,
				Projected30DRewards:     v.valInfo.Projected30DRewards,
			}
		}
	}

	// if public endpoints are enabled we do our best to keep the list refreshed. Immediate, then every 12 hours.
	if wantsPublic {
		go func() {
			e := refreshRegistry()
			if e != nil {
				l("could not fetch chain registry paths, using defaults")
			}
			for {
				time.Sleep(12 * time.Hour)
				l("refreshing cosmos.registry paths")
				e = refreshRegistry()
				if e != nil {
					l("could not refresh registry paths -", e)
				}
			}
		}()
	}
	return
}

func loadChainConfig(yamlFile string) (*ChainConfig, error) {
	//#nosec -- variable specified on command line
	f, e := os.OpenFile(yamlFile, os.O_RDONLY, 0600)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil {
		_ = f.Close()
		return nil, e
	}
	b := make([]byte, int(i.Size()))
	_, e = f.Read(b)
	_ = f.Close()
	if e != nil {
		return nil, e
	}
	c := &ChainConfig{}
	e = yaml.Unmarshal(b, c)
	if e != nil {
		return nil, e
	}
	return c, nil
}

// loadConfig creates a new Config from a file.
func loadConfig(yamlFile, stateFile, chainConfigDirectory string, password *string) (*Config, error) {
	c := &Config{}
	if strings.HasPrefix(yamlFile, "http://") || strings.HasPrefix(yamlFile, "https://") {
		if *password == "" {
			return nil, errors.New("a password is required if loading a remote configuration")
		}
		//#nosec -- url is specified on command line
		resp, err := http.Get(yamlFile)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		log.Printf("downloaded %d bytes from %s", len(b), yamlFile)
		decrypted, err := decrypt(b, *password)
		if err != nil {
			return nil, err
		}
		empty := ""
		password = &empty             // let gc get password out of memory, it's still referenced in main()
		_ = os.Setenv("PASSWORD", "") // also clear the ENV var
		err = yaml.Unmarshal(decrypted, c)
		if err != nil {
			return nil, err
		}
	} else {
		//#nosec -- variable specified on command line
		f, e := os.OpenFile(yamlFile, os.O_RDONLY, 0600)
		if e != nil {
			return nil, e
		}
		i, e := f.Stat()
		if e != nil {
			_ = f.Close()
			return nil, e
		}
		b := make([]byte, int(i.Size()))
		_, e = f.Read(b)
		_ = f.Close()
		if e != nil {
			return nil, e
		}
		e = yaml.Unmarshal(b, c)
		if e != nil {
			return nil, e
		}
	}

	// Load additional chain configuration files
	chainConfigFiles, e := os.ReadDir(chainConfigDirectory)
	if e != nil {
		l("Failed to scan chainConfigDirectory", e)
	}

	for _, chainConfigFile := range chainConfigFiles {
		if chainConfigFile.IsDir() {
			l("Skipping Directory: ", chainConfigFile.Name())
			continue
		}
		if !strings.HasSuffix(chainConfigFile.Name(), ".yml") {
			l("Skipping non .yml file: ", chainConfigFile.Name())
			continue
		}
		fmt.Println("Reading Chain Config File: ", chainConfigFile.Name())
		chainConfig, e := loadChainConfig(path.Join(chainConfigDirectory, chainConfigFile.Name()))
		if e != nil {
			l(fmt.Sprintf("Failed to read %s", chainConfigFile), e)
			return nil, e
		}

		chainName := strings.Split(chainConfigFile.Name(), ".")[0]

		// Create map if it didnt exist in config.yml
		if c.Chains == nil {
			c.Chains = make(map[string]*ChainConfig)
		}
		c.Chains[chainName] = chainConfig
		l(fmt.Sprintf("Added %s from ", chainName), chainConfigFile.Name())
	}

	if len(c.Chains) == 0 {
		return nil, errors.New("no chains configured")
	}

	c.alertChan = make(chan *alertMsg)
	c.logChan = make(chan dash.LogMessage)
	// buffer enough to get through validateConfig()
	c.updateChan = make(chan *dash.ChainStatus, len(c.Chains)*2)
	c.statsChan = make(chan *promUpdate, len(c.Chains)*2)
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// handle cached data. FIXME: incomplete.
	c.alarms = &alarmCache{
		SentPdAlarms:  make(map[string]alertMsgCache),
		SentTgAlarms:  make(map[string]alertMsgCache),
		SentDiAlarms:  make(map[string]alertMsgCache),
		SentSlkAlarms: make(map[string]alertMsgCache),
		SentWHAlarms:  make(map[string]alertMsgCache),
		AllAlarms:     make(map[string]map[string]alertMsgCache),
		notifyMux:     sync.RWMutex{},
	}

	//#nosec -- variable specified on command line
	sf, e := os.OpenFile(stateFile, os.O_RDONLY, 0600)
	if e != nil {
		l("could not load saved state", e.Error())
	}
	b, e := io.ReadAll(sf)
	_ = sf.Close()
	if e != nil {
		l("could not read saved state", e.Error())
	}
	saved := &savedState{}
	e = json.Unmarshal(b, saved)
	if e != nil {
		l("could not unmarshal saved state", e.Error())
	}
	for k, v := range saved.Blocks {
		if c.Chains[k] != nil {
			c.Chains[k].blocksResults = v
		}
	}

	// restore alarm state to prevent duplicate alerts
	if saved.Alarms != nil {
		if saved.Alarms.SentTgAlarms != nil {
			alarms.SentTgAlarms = saved.Alarms.SentTgAlarms
			clearStale(alarms.SentTgAlarms, "telegram", boolVal(c.DefaultAlertConfig.Pagerduty.Enabled), staleHours)
		}
		if saved.Alarms.SentPdAlarms != nil {
			alarms.SentPdAlarms = saved.Alarms.SentPdAlarms
			clearStale(alarms.SentPdAlarms, "PagerDuty", boolVal(c.DefaultAlertConfig.Pagerduty.Enabled), staleHours)
		}
		if saved.Alarms.SentDiAlarms != nil {
			alarms.SentDiAlarms = saved.Alarms.SentDiAlarms
			clearStale(alarms.SentDiAlarms, "Discord", boolVal(c.DefaultAlertConfig.Pagerduty.Enabled), staleHours)
		}
		if saved.Alarms.SentSlkAlarms != nil {
			alarms.SentSlkAlarms = saved.Alarms.SentSlkAlarms
			clearStale(alarms.SentSlkAlarms, "Slack", boolVal(c.DefaultAlertConfig.Pagerduty.Enabled), staleHours)
		}
		if saved.Alarms.SentWHAlarms != nil {
			alarms.SentWHAlarms = saved.Alarms.SentWHAlarms
			clearStale(alarms.SentWHAlarms, "Webhook", boolVal(c.DefaultAlertConfig.Webhook.Enabled), staleHours)
		}
		if saved.Alarms.AllAlarms != nil {
			alarms.AllAlarms = saved.Alarms.AllAlarms
			for _, alrm := range saved.Alarms.AllAlarms {
				clearStale(alrm, "dashboard", boolVal(c.DefaultAlertConfig.Pagerduty.Enabled), staleHours)
			}
		}
	}

	// we need to know if the node was already down to clear alarms
	if saved.NodesDown != nil {
		for k, v := range saved.NodesDown {
			for nodeUrl := range v {
				if !v[nodeUrl].IsZero() {
					if c.Chains[k] != nil {
						for j := range c.Chains[k].Nodes {
							if c.Chains[k].Nodes[j].Url == nodeUrl {
								c.Chains[k].Nodes[j].down = true
								c.Chains[k].Nodes[j].wasDown = true
								c.Chains[k].Nodes[j].downSince = v[nodeUrl]
							}
						}
					}
				}
			}
		}
		// now we need to know if all RPC endpoints were down.
		for k, v := range c.Chains {
			downCount := 0
			for j := range v.Nodes {
				if v.Nodes[j].down {
					downCount += 1
				}
			}
			if downCount == len(c.Chains[k].Nodes) {
				c.Chains[k].noNodes = true
			}
		}
	}

	c.tenderdutyCache = utils.NewCache()
	// init a CoinMarketCap client if needed
	if c.PriceConversion.Enabled {
		// Use ternary-like operation for currency selection
		currency := "USD"
		cacheExpiration := 8
		if c.PriceConversion.Currency != "" {
			currency = c.PriceConversion.Currency
		}
		if c.PriceConversion.CacheExpiration > 0 {
			cacheExpiration = c.PriceConversion.CacheExpiration
		}

		// Pre-allocate slice with known capacity
		slugs := make([]string, 0, len(c.Chains))
		for _, chain := range c.Chains {
			if chain.Slug != "" && !slices.Contains(slugs, strings.ToLower(chain.Slug)) {
				slugs = append(slugs, strings.ToLower(chain.Slug))
			}
		}

		c.coinMarketCapClient = utils.NewCoinMarketCapClient(c.CoinMarketCapAPIToken, currency, c.tenderdutyCache, cacheExpiration, slugs)
		_, err := c.coinMarketCapClient.GetPrices(c.ctx)
		if err == nil {
			l("💸 price conversion enabled")
		} else {
			c.PriceConversion.Enabled = false
			l("🛑 failed to enable price conversion, found error:", err)
		}
	}

	return c, nil
}

func clearStale(alarms map[string]alertMsgCache, what string, hasPagerduty bool, hours float64) {
	for k := range alarms {
		if time.Since(alarms[k].SentTime).Hours() >= hours {
			l(fmt.Sprintf("🗑 not restoring old alarm (%v >%.2f hours) from cache - %s", alarms[k], hours, k))
			if hasPagerduty && what == "pagerduty" {
				l("NOTE: stale alarms may need to be manually cleared from PagerDuty!")
			}
			delete(alarms, k)
			continue
		}
		l("📂 restored %s alarm state -", what, k)
	}
}

type ChainProvider interface {
	QueryUnvotedOpenProposals(ctx context.Context) ([]gov.Proposal, error)
	QueryChainInfo(ctx context.Context) (totalSupply float64, communityTax float64, inflationRate float64, err error)
	QueryValidatorInfo(ctx context.Context) (pub []byte, moniker string, jailed bool, bonded bool, delegatedTokens float64, commissionRate float64, err error)
	QuerySigningInfo(ctx context.Context) (*slashing.ValidatorSigningInfo, error)
	QuerySlashingParams(ctx context.Context) (*slashing.Params, error)
	QueryValidatorVotingPool(ctx context.Context) (votingPool *staking.Pool, err error)
	QueryValidatorSelfDelegationRewardsAndCommission(ctx context.Context) (rewards *github_com_cosmos_cosmos_sdk_types.DecCoins, commission *github_com_cosmos_cosmos_sdk_types.DecCoins, err error)
	QueryDenomMetadata(ctx context.Context, denom string) (medatada *bank.Metadata, err error)
}
