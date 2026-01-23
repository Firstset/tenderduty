package tenderduty

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// altValopers is used to get a bech32 prefix for chains using non-standard naming
var altValopers = &valoperOverrides{
	Prefixes: map[string]string{
		"ival": "ica", // Iris hub

		// TODO: was told tgrade also has a custom prefix, but not sure what the pair is
		// "tval": "tvalcons",
	},
}

type valoperOverrides struct {
	sync.RWMutex
	Prefixes    map[string]string `json:"prefixes"`
	LastUpdated time.Time         `json:"last_updated"`
}

func (vo *valoperOverrides) getAltPrefix(oper string) (prefix string, ok bool) {
	split := strings.Split(oper, "1")
	if len(split) == 0 {
		return "", false
	}
	vo.RLock()
	defer vo.RUnlock()
	return altValopers.Prefixes[split[0]], altValopers.Prefixes[split[0]] != ""
}

// cosmosPaths holds a mapping useful for finding public nodes from the cosmos directory from eco_stake
// it will be refreshed periodically.
var cosmosPaths = map[string]string{
	"Antora":                     "idep",
	"Oraichain":                  "oraichain",
	"agoric-3":                   "agoric",
	"akashnet-2":                 "akash",
	"arkh":                       "arkh",
	"axelar-dojo-1":              "axelar",
	"bitcanna-1":                 "bitcanna",
	"bitsong-2b":                 "bitsong",
	"bostrom":                    "bostrom",
	"carbon-1":                   "carbon",
	"cerberus-chain-1":           "cerberus",
	"cheqd-mainnet-1":            "cheqd",
	"chihuahua-1":                "chihuahua",
	"colosseum-1":                "firmachain",
	"columbus-5":                 "terra",
	"comdex-1":                   "comdex",
	"core-1":                     "persistence",
	"cosmoshub-4":                "cosmoshub",
	"crescent-1":                 "crescent",
	"cronosmainnet_25-1":         "cronos",
	"crypto-org-chain-mainnet-1": "cryptoorgchain",
	"cudos-1":                    "cudos",
	"darchub":                    "konstellation",
	"desmos-mainnet":             "desmos",
	"dig-1":                      "dig",
	"echelon_3000-3":             "echelon",
	"emoney-3":                   "emoney",
	"evmos_9001-2":               "evmos",
	"fetchhub-4":                 "fetchhub",
	"galaxy-1":                   "galaxy",
	"genesis_29-2":               "genesisl1",
	"gravity-bridge-3":           "gravitybridge",
	"impacthub-3":                "impacthub",
	"injective-1":                "injective",
	"iov-mainnet-ibc":            "starname",
	"irishub-1":                  "irisnet",
	"juno-1":                     "juno",
	"kava_2222-10":               "kava",
	"kichain-2":                  "kichain",
	"laozi-mainnet":              "bandchain",
	"likecoin-mainnet-2":         "likecoin",
	"logos_7002-1":               "logos",
	"lum-network-1":              "lumnetwork",
	"mainnet-3":                  "decentr",
	"mantle-1":                   "assetmantle",
	"meme-1":                     "meme",
	"microtick-1":                "microtick",
	"morocco-1":                  "chronicnetwork",
	"mythos_7001-1":              "mythos",
	"nomic-stakenet-2":           "nomic",
	"octa":                       "octa",
	"odin-mainnet-freya":         "odin",
	"osmosis-1":                  "osmosis",
	"panacea-3":                  "panacea",
	"phoenix-1":                  "terra2",
	"pio-mainnet-1":              "provenance",
	"regen-1":                    "regen",
	"secret-4":                   "secretnetwork",
	"sentinelhub-2":              "sentinel",
	"shentu-2.2":                 "shentu",
	"sifchain-1":                 "sifchain",
	"sommelier-3":                "sommelier",
	"stargaze-1":                 "stargaze",
	"thorchain-mainnet-v1":       "thorchain",
	"titan-1":                    "rizon",
	"umee-1":                     "umee",
	"vidulum-1":                  "vidulum",
}
var pathMux sync.Mutex

const (
	registryJson = "https://chains.cosmos.directory/"
	publicRpcUrl = "https://rpc.cosmos.directory:443/"
)

// a trimmed down version only holding the info we need to create a lookup map
type registryResults struct {
	Chains []struct {
		Path    string `json:"path"`
		ChainId string `json:"chain_id"`
	} `json:"chains"`
}

// refreshRegistry updates the path map for public RPC endpoints for @eco_stake's public RPC proxy
func refreshRegistry() error {
	res, err := http.Get(registryJson)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	chains := &registryResults{}
	err = json.Unmarshal(body, chains)
	if err != nil {
		return err
	}
	if len(chains.Chains) == 0 {
		return errors.New("response had no chains")
	}
	pathMux.Lock()
	defer pathMux.Unlock()
	if cosmosPaths == nil {
		cosmosPaths = make(map[string]string)
	}
	for _, c := range chains.Chains {
		cosmosPaths[c.ChainId] = c.Path
	}
	return nil
}

func getRegistryUrl(chainid string) (url string, ok bool) {
	pathMux.Lock()
	defer pathMux.Unlock()
	return publicRpcUrl + cosmosPaths[chainid], cosmosPaths[chainid] != ""
}

// getRegistryUrlByChainName returns the cosmos.directory RPC proxy URL for a given chain name
func getRegistryUrlByChainName(chainName string) string {
	return publicRpcUrl + chainName
}

// CosmosDirectoryResponse wraps the top-level API response from cosmos.directory
type CosmosDirectoryResponse struct {
	Chain CosmosDirectoryChainData `json:"chain"`
}

// CosmosDirectoryChainData holds chain information from cosmos.directory API
type CosmosDirectoryChainData struct {
	ChainID   string    `json:"chain_id"`
	Path      string    `json:"path"`
	ChainName string    `json:"chain_name"`
	Symbol    string    `json:"symbol"`
	Decimals  int       `json:"decimals"`
	Denom     string    `json:"denom"`
	Params    CDParams  `json:"params"`
	Assets    []CDAsset `json:"assets"`
}

// CDParams holds chain parameters from cosmos.directory
type CDParams struct {
	// Top-level params fields
	Authz               bool    `json:"authz"`
	ActualBlockTime     float64 `json:"actual_block_time"`
	ActualBlocksPerYear float64 `json:"actual_blocks_per_year"`
	CurrentBlockHeight  string  `json:"current_block_height"`
	UnbondingTime       int64   `json:"unbonding_time"`
	MaxValidators       int     `json:"max_validators"`
	CommunityTax        float64 `json:"community_tax"`
	BondedTokens        string  `json:"bonded_tokens"`
	AnnualProvision     string  `json:"annual_provision"`
	EstimatedAPR        float64 `json:"estimated_apr"`
	CalculatedAPR       float64 `json:"calculated_apr"`

	// Nested parameter objects
	Staking      CDStakingParams      `json:"staking"`
	Slashing     CDSlashingParams     `json:"slashing"`
	Distribution CDDistributionParams `json:"distribution"`
}

// CDStakingParams holds staking parameters
type CDStakingParams struct {
	UnbondingTime     string `json:"unbonding_time"`
	MaxValidators     int    `json:"max_validators"`
	MaxEntries        int    `json:"max_entries"`
	HistoricalEntries int    `json:"historical_entries"`
	BondDenom         string `json:"bond_denom"`
	MinCommissionRate string `json:"min_commission_rate"`
}

// CDSlashingParams holds slashing parameters
type CDSlashingParams struct {
	SignedBlocksWindow      string `json:"signed_blocks_window"`
	MinSignedPerWindow      string `json:"min_signed_per_window"`
	DowntimeJailDuration    string `json:"downtime_jail_duration"`
	SlashFractionDoubleSign string `json:"slash_fraction_double_sign"`
	SlashFractionDowntime   string `json:"slash_fraction_downtime"`
}

// CDDistributionParams holds distribution parameters
type CDDistributionParams struct {
	CommunityTax        string `json:"community_tax"`
	BaseProposerReward  string `json:"base_proposer_reward"`
	BonusProposerReward string `json:"bonus_proposer_reward"`
	WithdrawAddrEnabled bool   `json:"withdraw_addr_enabled"`
}

// CDAssetDenomInfo holds denomination info for base/display in assets
type CDAssetDenomInfo struct {
	Denom    string `json:"denom"`
	Exponent int    `json:"exponent"`
}

// CDAsset holds asset information from cosmos.directory
type CDAsset struct {
	Base        CDAssetDenomInfo `json:"base"`
	Symbol      string           `json:"symbol"`
	Display     CDAssetDenomInfo `json:"display"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	DenomUnits  []CDDenomUnit    `json:"denom_units"`
}

// CDDenomUnit holds denomination unit information
type CDDenomUnit struct {
	Denom    string   `json:"denom"`
	Exponent int      `json:"exponent"`
	Aliases  []string `json:"aliases"`
}

const (
	chainDataCacheKey = "cosmos_directory_chain_data_"
	chainDataCacheTTL = 30 * time.Minute
)

// fetchCosmosDirectoryChainData fetches chain data from cosmos.directory API
// chainName is the cosmos.directory path (e.g., "babylon", "osmosis")
func fetchCosmosDirectoryChainData(chainName string) (*CosmosDirectoryChainData, error) {
	cacheKey := chainDataCacheKey + chainName

	// Try to get from cache first
	if cached, ok := td.tenderdutyCache.Get(cacheKey); ok {
		if data, ok := cached.(*CosmosDirectoryChainData); ok {
			return data, nil
		}
	}

	// Fetch from cosmos.directory API with timeout
	url := registryJson + chainName
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("cosmos.directory returned status: " + resp.Status + " for chain: " + chainName)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response CosmosDirectoryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	chainData := response.Chain

	// Verify we got valid data
	if chainData.ChainID == "" {
		return nil, errors.New("cosmos.directory returned empty chain data for: " + chainName)
	}

	// Cache the result
	td.tenderdutyCache.Set(cacheKey, &chainData, chainDataCacheTTL)

	return &chainData, nil
}

// getEffectiveChainName returns the chain name to use for cosmos.directory lookups
// It uses chain_name if set, otherwise falls back to lowercase of the display name
func (cc *ChainConfig) getEffectiveChainName() string {
	if cc.ChainName != "" {
		return cc.ChainName
	}
	// Fall back to lowercase of the display name (cc.name)
	return strings.ToLower(cc.name)
}

// loadCosmosDirectoryData attempts to load chain data from cosmos.directory
// and caches it in the ChainConfig. Returns nil if the chain is not found.
func (cc *ChainConfig) loadCosmosDirectoryData() error {
	chainName := cc.getEffectiveChainName()
	data, err := fetchCosmosDirectoryChainData(chainName)
	if err != nil {
		return err
	} else {
		if data.ChainID != cc.ChainId {
			return fmt.Errorf("configured chain ID (%s) does not match the chain ID from CosmosDirectory (%s), you can ignore this error if the validator is running in testnet", cc.ChainId, data.ChainID)
		} else {
			cc.cosmosDirectoryData = data
		}
	}
	return nil
}

// hasCosmosDirectoryData returns true if cosmos.directory data is available
func (cc *ChainConfig) hasCosmosDirectoryData() bool {
	return cc.cosmosDirectoryData != nil
}

// getCosmosDirectoryRPCUrl returns the cosmos.directory RPC proxy URL for this chain
func (cc *ChainConfig) getCosmosDirectoryRPCUrl() string {
	return getRegistryUrlByChainName(cc.getEffectiveChainName())
}

// getDenomMetadataFromCosmosDirectory returns bank metadata from cosmos.directory data
// Returns nil if the chain doesn't have cosmos.directory data or no matching asset is found
func (cc *ChainConfig) getDenomMetadataFromCosmosDirectory(denom string) *CDAsset {
	if cc.cosmosDirectoryData == nil {
		return nil
	}

	// First try to find an exact match for the denom
	for _, asset := range cc.cosmosDirectoryData.Assets {
		if asset.Base.Denom == denom {
			return &asset
		}
	}

	// If no exact match, return the first asset (usually the native token)
	if len(cc.cosmosDirectoryData.Assets) > 0 {
		return &cc.cosmosDirectoryData.Assets[0]
	}

	return nil
}

// getChainInfoFromCosmosDirectory returns chain info from cosmos.directory data
// Returns (communityTax, calculatedAPR, ok)
func (cc *ChainConfig) getChainInfoFromCosmosDirectory() (communityTax float64, calculatedAPR float64, ok bool) {
	if cc.cosmosDirectoryData == nil {
		return 0, 0, false
	}

	// Get community tax from params (top-level, as float64)
	communityTax = cc.cosmosDirectoryData.Params.CommunityTax

	// Use calculated APR from cosmos.directory params
	calculatedAPR = cc.cosmosDirectoryData.Params.CalculatedAPR

	ok = true
	return
}

// getBankMetadataFromCosmosDirectory converts cosmos.directory asset data to bank.Metadata
// Returns nil if no matching asset is found
func (cc *ChainConfig) getBankMetadataFromCosmosDirectory(denom string) *bank.Metadata {
	cdAsset := cc.getDenomMetadataFromCosmosDirectory(denom)
	if cdAsset == nil {
		return nil
	}

	// Convert CDDenomUnit to bank.DenomUnit
	denomUnits := make([]*bank.DenomUnit, len(cdAsset.DenomUnits))
	for i, unit := range cdAsset.DenomUnits {
		// Ensure exponent is within valid uint32 range to avoid integer overflow
		// Denom exponents are typically small (0-18), but we check the full range for safety
		exponent := uint32(0)
		if unit.Exponent >= 0 && unit.Exponent <= 255 {
			exponent = uint32(unit.Exponent)
		}
		denomUnits[i] = &bank.DenomUnit{
			Denom:    unit.Denom,
			Exponent: exponent,
			Aliases:  unit.Aliases,
		}
	}

	return &bank.Metadata{
		Description: cdAsset.Description,
		DenomUnits:  denomUnits,
		Base:        cdAsset.Base.Denom,
		Display:     cdAsset.Display.Denom,
		Name:        cdAsset.Name,
		Symbol:      cdAsset.Symbol,
	}
}
