package tenderduty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	github_com_cosmos_cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	"github.com/firstset/tenderduty/v2/td2/utils"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type alertMsg struct {
	pd   bool
	disc bool
	tg   bool
	slk  bool
	wh   bool

	severity string
	resolved bool
	chain    string
	message  string
	uniqueId string
	key      string

	tgChannel  string
	tgKey      string
	tgMentions string

	discHook     string
	discMentions string

	slkHook     string
	slkMentions string

	whURL string

	alertConfig *AlertConfig
}

type notifyDest uint8

const (
	pd notifyDest = iota
	tg
	di
	slk
	wh
)

type alertMsgCache struct {
	Message  string    `json:"message"`
	SentTime time.Time `json:"sent_time"`
}

type alarmCache struct {
	// the key of an alertMsgCache is the unique ID of the alert
	// we use the following convention for the unique ID: <alert_name>_<val_address>_<other_info>
	SentPdAlarms   map[string]alertMsgCache            `json:"sent_pd_alarms"`
	SentTgAlarms   map[string]alertMsgCache            `json:"sent_tg_alarms"`
	SentDiAlarms   map[string]alertMsgCache            `json:"sent_di_alarms"`
	SentSlkAlarms  map[string]alertMsgCache            `json:"sent_slk_alarms"`
	SentWHAlarms   map[string]alertMsgCache            `json:"sent_wh_alarms"`
	AllAlarms      map[string]map[string]alertMsgCache `json:"sent_all_alarms"`
	flappingAlarms map[string]map[string]alertMsgCache
	notifyMux      sync.RWMutex
}

func (a *alarmCache) clearNoBlocks(cc *ChainConfig) {
	if a.AllAlarms == nil || a.AllAlarms[cc.name] == nil {
		return
	}
	for clearAlarm := range a.AllAlarms[cc.name] {
		if strings.HasPrefix(clearAlarm, "ChainStalled") {
			alertID := fmt.Sprintf("ChainStalled_%s", cc.ValAddress)
			td.alert(
				cc.name,
				fmt.Sprintf("stalled: have not seen a new block on %s in %d minutes", cc.ChainId, intVal(cc.Alerts.Stalled)),
				"critical",
				true,
				&alertID,
			)
		}
	}
}

func (a *alarmCache) getCount(chain string) int {
	if a.AllAlarms == nil || a.AllAlarms[chain] == nil {
		return 0
	}
	a.notifyMux.RLock()
	defer a.notifyMux.RUnlock()
	return len(a.AllAlarms[chain])
}

func (a *alarmCache) clearAll(chain string) {
	if a.AllAlarms == nil || a.AllAlarms[chain] == nil {
		return
	}
	a.notifyMux.Lock()
	defer a.notifyMux.Unlock()
	a.AllAlarms[chain] = make(map[string]alertMsgCache)
}

func (a *alarmCache) exist(chain string, alertID string) bool {
	if a.AllAlarms == nil || a.AllAlarms[chain] == nil {
		return false
	}
	a.notifyMux.RLock()
	defer a.notifyMux.RUnlock()

	_, ok := alarms.AllAlarms[chain][alertID]
	return ok
}

// alarms is used to prevent double notifications. TODO: save on exit / load on start
var alarms = &alarmCache{
	SentPdAlarms:   make(map[string]alertMsgCache),
	SentTgAlarms:   make(map[string]alertMsgCache),
	SentDiAlarms:   make(map[string]alertMsgCache),
	SentSlkAlarms:  make(map[string]alertMsgCache),
	SentWHAlarms:   make(map[string]alertMsgCache),
	AllAlarms:      make(map[string]map[string]alertMsgCache),
	flappingAlarms: make(map[string]map[string]alertMsgCache),
	notifyMux:      sync.RWMutex{},
}

func shouldNotify(msg *alertMsg, dest notifyDest) bool {
	alarms.notifyMux.Lock()
	defer alarms.notifyMux.Unlock()
	var whichMap map[string]alertMsgCache
	var service string
	switch dest {
	case pd:
		if !slices.Contains(SeverityThresholdToSeverities(msg.alertConfig.Pagerduty.SeverityThreshold), msg.severity) {
			return false
		}
		whichMap = alarms.SentPdAlarms
		service = "PagerDuty"
	case tg:
		if !slices.Contains(SeverityThresholdToSeverities(msg.alertConfig.Telegram.SeverityThreshold), msg.severity) {
			return false
		}
		whichMap = alarms.SentTgAlarms
		service = "Telegram"
	case di:
		if !slices.Contains(SeverityThresholdToSeverities(msg.alertConfig.Discord.SeverityThreshold), msg.severity) {
			return false
		}
		whichMap = alarms.SentDiAlarms
		service = "Discord"
	case slk:
		if !slices.Contains(SeverityThresholdToSeverities(msg.alertConfig.Slack.SeverityThreshold), msg.severity) {
			return false
		}
		whichMap = alarms.SentSlkAlarms
		service = "Slack"
	case wh:
		if !slices.Contains(SeverityThresholdToSeverities(msg.alertConfig.Webhook.SeverityThreshold), msg.severity) {
			return false
		}
		whichMap = alarms.SentWHAlarms
		service = "Webhook"
	}

	switch {
	case !whichMap[msg.uniqueId].SentTime.IsZero() && !msg.resolved:
		// TODO: this is a temporary solution for sending proposal reminders, ideally we should make this feature more general and configurable
		// Check if this is a proposal alert that should be re-sent
		if strings.HasPrefix(msg.uniqueId, "UnvotedGovernanceProposal") {
			// Check if it has been 6 hours since the last (re-)send
			if whichMap[msg.uniqueId].SentTime.Before(time.Now().Add(-1 * time.Duration(td.GovernanceAlertsReminderInterval) * time.Hour)) {
				l(fmt.Sprintf("🔄 RE-SENDING ALERT on %s (%s) - notifying %s", msg.chain, msg.message, service))
				cache := alertMsgCache{
					Message:  msg.message,
					SentTime: time.Now(),
				}
				whichMap[msg.uniqueId] = cache
				return true
			}
		}
		return false
	case !whichMap[msg.uniqueId].SentTime.IsZero() && msg.resolved:
		// alarm is cleared
		delete(whichMap, msg.uniqueId)
		l(fmt.Sprintf("💜 Resolved     alarm on %s (%s) - notifying %s", msg.chain, msg.message, service))
		return true
	case msg.resolved:
		// it looks like we got a duplicate resolution or suppressed it. Note it and move on:
		l(fmt.Sprintf("😕 Not clearing alarm on %s (%s) - no corresponding alert %s", msg.chain, msg.message, service))
		return false
	}

	// check if the alarm is flapping, if we sent the same alert in the last five minutes, show a warning but don't alert
	if alarms.flappingAlarms[msg.chain] == nil {
		alarms.flappingAlarms[msg.chain] = make(map[string]alertMsgCache)
	}

	// for pagerduty we perform some basic flap detection
	if dest == pd && msg.pd && alarms.flappingAlarms[msg.chain][msg.uniqueId].SentTime.After(time.Now().Add(-5*time.Minute)) {
		l("🛑 flapping detected - suppressing pagerduty notification:", msg.chain, msg.message)
		return false
	} else if dest == pd && msg.pd {
		cache := alertMsgCache{
			Message:  msg.message,
			SentTime: time.Now(),
		}
		alarms.flappingAlarms[msg.chain][msg.uniqueId] = cache
	}

	l(fmt.Sprintf("🚨 ALERT        new alarm on %s (%s) - notifying %s", msg.chain, msg.message, service))
	cache := alertMsgCache{
		Message:  msg.message,
		SentTime: time.Now(),
	}
	whichMap[msg.uniqueId] = cache
	return true
}

func notifySlack(msg *alertMsg) (err error) {
	if !msg.slk {
		return
	}
	data, err := json.Marshal(buildSlackMessage(msg))
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", msg.slkHook, bytes.NewBuffer(data))
	if err != nil {
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("could not notify slack for %s got %d response", msg.chain, resp.StatusCode)
	}

	return
}

type SlackMessage struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	Text      string `json:"text"`
	Color     string `json:"color"`
	Title     string `json:"title"`
	TitleLink string `json:"title_link"`
}

func buildSlackMessage(msg *alertMsg) *SlackMessage {
	prefix := "🚨 ALERT: "
	color := "danger"
	if msg.resolved {
		msg.message = "OK: " + msg.message
		prefix = "💜 Resolved: "
		color = "good"
	}
	return &SlackMessage{
		Text: msg.message,
		Attachments: []Attachment{
			{
				Title: fmt.Sprintf("TenderDuty %s %s %s", prefix, msg.chain, msg.slkMentions),
				Color: color,
			},
		},
	}
}

func notifyDiscord(msg *alertMsg) (err error) {
	if !msg.disc {
		return nil
	}
	if !shouldNotify(msg, di) {
		return nil
	}
	discPost := buildDiscordMessage(msg)
	client := &http.Client{}
	data, err := json.MarshalIndent(discPost, "", "  ")
	if err != nil {
		l("⚠️ Could not notify discord!", err)
		return err
	}

	req, err := http.NewRequest("POST", msg.discHook, bytes.NewBuffer(data))
	if err != nil {
		l("⚠️ Could not notify discord!", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		l("⚠️ Could not notify discord!", err)
		return err
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 204 {
		log.Println(resp)
		l("⚠️ Could not notify discord! Returned", resp.StatusCode)
		return err
	}
	return nil
}

type DiscordMessage struct {
	Username  string         `json:"username,omitempty"`
	AvatarUrl string         `json:"avatar_url,omitempty"`
	Content   string         `json:"content"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

type DiscordEmbed struct {
	Title       string `json:"title,omitempty"`
	Url         string `json:"url,omitempty"`
	Description string `json:"description"`
	Color       uint   `json:"color"`
}

func buildDiscordMessage(msg *alertMsg) *DiscordMessage {
	prefix := "🚨 ALERT: "
	if msg.resolved {
		prefix = "💜 Resolved: "
	}
	return &DiscordMessage{
		Username: "Tenderduty",
		Content:  prefix + msg.chain,
		Embeds: []DiscordEmbed{{
			Description: msg.message,
		}},
	}
}

func notifyTg(msg *alertMsg) (err error) {
	if !msg.tg {
		return nil
	}
	if !shouldNotify(msg, tg) {
		return nil
	}
	bot, err := tgbotapi.NewBotAPI(msg.tgKey)
	if err != nil {
		l("notify telegram:", err)
		return
	}

	prefix := "🚨 ALERT"
	if msg.resolved {
		prefix = "💜 Resolved"
	}

	mc := tgbotapi.NewMessageToChannel(msg.tgChannel, fmt.Sprintf("%s: %s: %s", msg.chain, prefix, msg.message))
	_, err = bot.Send(mc)
	if err != nil {
		l("telegram send:", err)
	}
	return err
}

func notifyPagerduty(msg *alertMsg) (err error) {
	if !msg.pd {
		return nil
	}
	if !shouldNotify(msg, pd) {
		return nil
	}
	// key from the example, don't spam their api
	if msg.key == "aaaaaaaaaaaabbbbbbbbbbbbbcccccccccccc" {
		l("invalid pagerduty key")
		return
	}
	action := "trigger"
	if msg.resolved {
		action = "resolve"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = pagerduty.ManageEventWithContext(ctx, pagerduty.V2Event{
		RoutingKey: msg.key,
		Action:     action,
		DedupKey:   msg.uniqueId,
		Payload: &pagerduty.V2Payload{
			Summary:  msg.message,
			Source:   msg.uniqueId,
			Severity: msg.severity,
		},
	})
	return
}

// WebhookPayload represents the payload sent to a generic webhook endpoint
// The structure is inspired by Grafana's webhook contact point format
type WebhookPayload struct {
	Status   string         `json:"status"`
	Alerts   []WebhookAlert `json:"alerts"`
	Version  string         `json:"version"`
	GroupKey string         `json:"groupKey"`
}

// WebhookAlert represents a single alert in the webhook payload
type WebhookAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	Fingerprint  string            `json:"fingerprint"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

func notifyWebhook(msg *alertMsg) (err error) {
	if !msg.wh {
		return nil
	}
	if !shouldNotify(msg, wh) {
		return nil
	}

	// Skip sending resolved messages if disabled in config
	if msg.resolved && boolVal(msg.alertConfig.Webhook.DisableResolveMessage) {
		return nil
	}

	status := "firing"
	if msg.resolved {
		status = "resolved"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	endsAt := "0001-01-01T00:00:00Z"
	if msg.resolved {
		endsAt = now
	}

	alert := WebhookAlert{
		Status: status,
		Labels: map[string]string{
			"alertname": msg.uniqueId,
			"chain":     msg.chain,
			"severity":  msg.severity,
			"source":    "tenderduty",
		},
		Annotations: map[string]string{
			"summary":     msg.message,
			"description": msg.message,
		},
		StartsAt:    now,
		EndsAt:      endsAt,
		Fingerprint: msg.uniqueId,
	}

	payload := WebhookPayload{
		Status:   status,
		Alerts:   []WebhookAlert{alert},
		Version:  "1",
		GroupKey: msg.chain,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		l("⚠️ Could not marshal webhook payload!", err)
		return err
	}

	req, err := http.NewRequest("POST", msg.whURL, bytes.NewBuffer(data))
	if err != nil {
		l("⚠️ Could not create webhook request!", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		l("⚠️ Could not send webhook!", err)
		return err
	}
	_ = resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		l("⚠️ Webhook returned non-success status:", resp.StatusCode)
		return fmt.Errorf("webhook returned status %d for %s", resp.StatusCode, msg.chain)
	}

	return nil
}

func getAlarms(chain string) string {
	alarms.notifyMux.RLock()
	defer alarms.notifyMux.RUnlock()
	// don't show this info if the logs are disabled on the dashboard, potentially sensitive info could be leaked.
	if td.HideLogs || alarms.AllAlarms[chain] == nil {
		return ""
	}
	result := ""
	for k := range alarms.AllAlarms[chain] {
		result += "🚨 " + alarms.AllAlarms[chain][k].Message + "\n"
	}
	return result
}

// alert creates a universal alert and pushes it to the alertChan to be delivered to appropriate services
func (c *Config) alert(chainName, message, severity string, resolved bool, id *string) {
	if id == nil {
		return
	}
	c.chainsMux.RLock()
	a := &alertMsg{
		pd:           boolVal(c.DefaultAlertConfig.Pagerduty.Enabled) && boolVal(c.Chains[chainName].Alerts.Pagerduty.Enabled),
		disc:         boolVal(c.DefaultAlertConfig.Discord.Enabled) && boolVal(c.Chains[chainName].Alerts.Discord.Enabled),
		tg:           boolVal(c.DefaultAlertConfig.Telegram.Enabled) && boolVal(c.Chains[chainName].Alerts.Telegram.Enabled),
		slk:          boolVal(c.DefaultAlertConfig.Slack.Enabled) && boolVal(c.Chains[chainName].Alerts.Slack.Enabled),
		wh:           boolVal(c.DefaultAlertConfig.Webhook.Enabled) && boolVal(c.Chains[chainName].Alerts.Webhook.Enabled),
		severity:     severity,
		resolved:     resolved,
		chain:        fmt.Sprintf("%s (%s)", chainName, c.Chains[chainName].ChainId),
		message:      message,
		uniqueId:     *id,
		key:          c.Chains[chainName].Alerts.Pagerduty.ApiKey,
		tgChannel:    c.Chains[chainName].Alerts.Telegram.Channel,
		tgKey:        c.Chains[chainName].Alerts.Telegram.ApiKey,
		tgMentions:   strings.Join(c.Chains[chainName].Alerts.Telegram.Mentions, " "),
		discHook:     c.Chains[chainName].Alerts.Discord.Webhook,
		discMentions: strings.Join(c.Chains[chainName].Alerts.Discord.Mentions, " "),
		slkHook:      c.Chains[chainName].Alerts.Slack.Webhook,
		whURL:        c.Chains[chainName].Alerts.Webhook.URL,
		alertConfig:  &c.Chains[chainName].Alerts,
	}
	c.alertChan <- a
	c.chainsMux.RUnlock()
	alarms.notifyMux.Lock()
	defer alarms.notifyMux.Unlock()
	if alarms.AllAlarms[chainName] == nil {
		alarms.AllAlarms[chainName] = make(map[string]alertMsgCache)
	}
	if resolved && !alarms.AllAlarms[chainName][*id].SentTime.IsZero() {
		delete(alarms.AllAlarms[chainName], *id)
		return
	} else if resolved {
		return
	}
	cache := alertMsgCache{
		Message:  message,
		SentTime: time.Now(),
	}
	alarms.AllAlarms[chainName][*id] = cache
}

func evaluateConsecutiveBlocksMissedAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	alertID := fmt.Sprintf("ConsecutiveBlocksMissed_%s", cc.ValAddress)
	if int(cc.statConsecutiveMiss) >= intVal(cc.Alerts.ConsecutiveMissed) {
		if !alarms.exist(cc.name, alertID) {
			// alert on missed block counter!
			td.alert(
				cc.name,
				fmt.Sprintf("%s has missed %d blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.ConsecutiveMissed), cc.ChainId),
				cc.Alerts.ConsecutivePriority,
				false,
				&alertID,
			)
			alert = true
		}
	} else {
		if alarms.exist(cc.name, alertID) {
			// clear the alert
			td.alert(
				cc.name,
				fmt.Sprintf("%s has missed %d blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.ConsecutiveMissed), cc.ChainId),
				cc.Alerts.ConsecutivePriority,
				true,
				&alertID,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluatePercentageBlocksMissedAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	alertID := fmt.Sprintf("PercentageBlocksMissed_%s", cc.ValAddress)
	if 100*float64(cc.valInfo.Missed)/float64(cc.valInfo.Window) >= float64(intVal(cc.Alerts.Window)) {
		if !alarms.exist(cc.name, alertID) {
			// alert on missed block counter!
			td.alert(
				cc.name,
				fmt.Sprintf("%s has missed > %d%% of the slashing window's blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.Window), cc.ChainId),
				cc.Alerts.PercentagePriority,
				false,
				&alertID,
			)
			alert = true
		}
	} else {
		if alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("%s has missed > %d%% of the slashing window's blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.Window), cc.ChainId),
				cc.Alerts.PercentagePriority,
				true,
				&alertID,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluateNoRPCEndpointsAlert(cc *ChainConfig, noNodesSec *int) (bool, bool) {
	alert, resolved := false, false

	alertID := fmt.Sprintf("NoRPCEndpoints_%s", cc.ValAddress)
	if cc.noNodes {
		*noNodesSec += 2
		if *noNodesSec <= 60*td.NodeDownMin {
			if *noNodesSec%20 == 0 {
				l(fmt.Sprintf("no nodes available on %s for %d seconds, deferring alarm", cc.ChainId, *noNodesSec))
			}
		} else {
			if !alarms.exist(cc.name, alertID) {
				td.alert(
					cc.name,
					fmt.Sprintf("no RPC endpoints are working for %s", cc.ChainId),
					"critical",
					false,
					&alertID,
				)
				alert = true
			}
		}
	} else {
		if alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("no RPC endpoints are working for %s", cc.ChainId),
				"critical",
				true,
				&alertID,
			)
			resolved = true
		}
		*noNodesSec = 0
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluateChainStalledAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	if !cc.lastBlockTime.IsZero() {
		alertID := fmt.Sprintf("ChainStalled_%s", cc.ValAddress)
		if !cc.lastBlockAlarm && cc.lastBlockTime.Before(time.Now().Add(time.Duration(-intVal(cc.Alerts.Stalled))*time.Minute)) {
			cc.lastBlockAlarm = true
			td.alert(
				cc.name,
				fmt.Sprintf("stalled: have not seen a new block on %s in %d minutes", cc.ChainId, intVal(cc.Alerts.Stalled)),
				"critical",
				false,
				&alertID,
			)
			alert = true
		} else if !cc.lastBlockTime.Before(time.Now().Add(time.Duration(-intVal(cc.Alerts.Stalled)) * time.Minute)) {
			alarms.clearNoBlocks(cc)
			cc.lastBlockAlarm = false
			resolved = true
		}
		cc.activeAlerts = alarms.getCount(cc.name)
	}

	return alert, resolved
}

func evaluateValidatorInactiveAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	if cc.lastValInfo != nil && cc.lastValInfo.Bonded != cc.valInfo.Bonded &&
		cc.lastValInfo.Moniker == cc.valInfo.Moniker {
		inactive := "jailed"
		alertID := fmt.Sprintf("ValidatorInactive_%s", cc.ValAddress)
		if !cc.valInfo.Bonded && cc.lastValInfo.Bonded {
			if cc.valInfo.Tombstoned {
				inactive = "☠️ tombstoned 🪦"
			}
			td.alert(
				cc.name,
				fmt.Sprintf("%s is no longer active: validator %s is %s for chainid %s", cc.valInfo.Moniker, cc.ValAddress, inactive, cc.ChainId),
				"critical",
				false,
				&alertID,
			)
			alert = true
		} else if cc.valInfo.Bonded && !cc.lastValInfo.Bonded {
			td.alert(
				cc.name,
				fmt.Sprintf("%s is no longer active: validator %s is %s for chainid %s", cc.valInfo.Moniker, cc.ValAddress, inactive, cc.ChainId),
				"critical",
				true,
				&alertID,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluateConsecutiveEmptyBlocksAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	alertID := fmt.Sprintf("ConsecutiveEmptyBlocks_%s", cc.ValAddress)
	if int(cc.statConsecutiveEmpty) >= intVal(cc.Alerts.ConsecutiveEmpty) {
		if !alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("%s has proposed %d consecutive empty blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.ConsecutiveEmpty), cc.ChainId),
				cc.Alerts.ConsecutiveEmptyPriority,
				false,
				&alertID,
			)
			alert = true
		}
	} else {
		if alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("%s has proposed %d consecutive empty blocks on %s", cc.valInfo.Moniker, intVal(cc.Alerts.ConsecutiveEmpty), cc.ChainId),
				cc.Alerts.ConsecutiveEmptyPriority,
				true,
				&alertID,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluatePercentageEmptyBlocksAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	var emptyBlocksPercent float64
	if cc.statTotalProps > 0 {
		emptyBlocksPercent = 100 * float64(cc.statTotalPropsEmpty) / float64(cc.statTotalProps)
	}

	alertID := fmt.Sprintf("PercentageEmptyBlocks_%s", cc.ValAddress)
	if emptyBlocksPercent >= float64(intVal(cc.Alerts.EmptyWindow)) {
		if !alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("%s has > %d%% empty blocks (%d of %d proposed blocks) on %s",
					cc.valInfo.Moniker,
					intVal(cc.Alerts.EmptyWindow),
					int(cc.statTotalPropsEmpty),
					int(cc.statTotalProps),
					cc.ChainId),
				cc.Alerts.EmptyPercentagePriority,
				false,
				&alertID,
			)
			alert = true
		}
	} else {
		if alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				fmt.Sprintf("%s has > %d%% empty blocks (%d of %d proposed blocks) on %s",
					cc.valInfo.Moniker,
					intVal(cc.Alerts.EmptyWindow),
					int(cc.statTotalPropsEmpty),
					int(cc.statTotalProps),
					cc.ChainId),
				cc.Alerts.EmptyPercentagePriority,
				true,
				&alertID,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluateRPCNodeDownAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	for _, node := range cc.Nodes {
		alertID := fmt.Sprintf("RPCNodeDown_%s_%s", cc.ValAddress, node.Url)
		if node.AlertIfDown && node.down && !node.wasDown && !node.downSince.IsZero() &&
			time.Since(node.downSince) > time.Duration(td.NodeDownMin)*time.Minute {
			if !alarms.exist(cc.name, alertID) {
				td.alert(
					cc.name,
					fmt.Sprintf("Severity: %s\nRPC node %s has been down for > %d minutes on %s", td.NodeDownSeverity, node.Url, td.NodeDownMin, cc.ChainId),
					td.NodeDownSeverity,
					false,
					&alertID,
				)
				alert = true
			}
		} else if node.AlertIfDown && !node.down && node.wasDown {
			node.wasDown = false
			if alarms.exist(cc.name, alertID) {
				td.alert(
					cc.name,
					fmt.Sprintf("Severity: %s\nRPC node %s has been down for > %d minutes on %s", td.NodeDownSeverity, node.Url, td.NodeDownMin, cc.ChainId),
					td.NodeDownSeverity,
					true,
					&alertID,
				)
				resolved = true
			}
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

func evaluateStakeChangeAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	if cc.valInfo != nil && cc.lastValInfo != nil && cc.lastValInfo.DelegatedTokens > 0 {
		stakeNow := cc.valInfo.DelegatedTokens
		stakeBefore := cc.lastValInfo.DelegatedTokens
		stakeChangePercent := (stakeNow - stakeBefore) / stakeBefore
		trend := "increased"
		threshold := floatVal(cc.Alerts.StakeChangeIncreaseThreshold)
		if stakeChangePercent < 0 {
			trend = "dropped"
			threshold = floatVal(cc.Alerts.StakeChangeDropThreshold)
		}
		alertID := fmt.Sprintf("StakeChange_%s", cc.ValAddress)
		severity := "warning"
		unit := "base"
		if cc.denomMetadata != nil && cc.Provider.Name != "namada" {
			var stakeNowConverted, stakeBeforeConverted float64
			var displayUnit string
			var err0, err1 error
			stakeNowConverted, _, err0 = utils.ConvertFloatInBaseUnitToDisplayUnit(stakeNow, *cc.denomMetadata)
			stakeBeforeConverted, displayUnit, err1 = utils.ConvertFloatInBaseUnitToDisplayUnit(stakeBefore, *cc.denomMetadata)
			if err0 == nil && err1 == nil {
				stakeNow = stakeNowConverted
				stakeBefore = stakeBeforeConverted
				unit = displayUnit
			}
		} else if cc.Provider.Name == "namada" {
			unit = "NAM"
		}
		message := fmt.Sprintf("%s's stake has %s by %.1f%% (%s %s now) compared to the previous check (%s %s)", cc.valInfo.Moniker, trend, math.Abs(stakeChangePercent)*100, utils.HumanSI(stakeNow), unit, utils.HumanSI(stakeBefore), unit)
		if math.Abs(stakeChangePercent) >= threshold {
			if !alarms.exist(cc.name, alertID) {
				td.alert(cc.name, message, severity, false, &alertID)
				alert = true
			}
		} else {
			if alarms.exist(cc.name, alertID) {
				td.alert(cc.name, message, severity, true, &alertID)
				resolved = true
			}
		}
		cc.activeAlerts = alarms.getCount(cc.name)
	}

	return alert, resolved
}

func evaluateUnclaimedRewardsAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	selfRewardsLen := 0
	commissionLen := 0
	if cc.valInfo.SelfDelegationRewards != nil {
		selfRewardsLen = len(*cc.valInfo.SelfDelegationRewards)
	}
	if cc.valInfo.Commission != nil {
		commissionLen = len(*cc.valInfo.Commission)
	}

	if selfRewardsLen > 0 || commissionLen > 0 {
		var targetDenoms []string
		addDenom := func(denom string) {
			if denom == "" {
				return
			}
			if !slices.Contains(targetDenoms, denom) {
				targetDenoms = append(targetDenoms, denom)
			}
		}

		targetDenom := ""
		// we take the chain's native denoms from cc.denomMetadata
		if cc.denomMetadata != nil {
			addDenom(cc.denomMetadata.Base)
			addDenom(cc.denomMetadata.Display)
			targetDenom = cc.denomMetadata.Display
			if targetDenom == "" {
				targetDenom = cc.denomMetadata.Base
			}
		}

		// when `cc.denomMetadata` is nil, we try to infer the denom from the rewards
		if len(targetDenoms) == 0 {
			if selfRewardsLen > 0 {
				addDenom((*cc.valInfo.SelfDelegationRewards)[0].Denom)
			} else if commissionLen > 0 {
				// some chains return commission coins with IBC denoms
				// we will filter out these non-native coins for now
				for _, coin := range *cc.valInfo.Commission {
					if !strings.HasPrefix(coin.Denom, "ibc/") {
						addDenom(coin.Denom)
					}
				}
			}
		}

		if targetDenom == "" && len(targetDenoms) > 0 {
			targetDenom = targetDenoms[0]
		}

		if len(targetDenoms) == 0 || targetDenom == "" {
			return alert, resolved
		}

		var nativeCoins []github_com_cosmos_cosmos_sdk_types.DecCoin
		if selfRewardsLen > 0 {
			for _, coin := range *cc.valInfo.SelfDelegationRewards {
				if slices.Contains(targetDenoms, coin.Denom) {
					nativeCoins = append(nativeCoins, coin)
				}
			}
		}
		if commissionLen > 0 {
			for _, coin := range *cc.valInfo.Commission {
				if slices.Contains(targetDenoms, coin.Denom) {
					nativeCoins = append(nativeCoins, coin)
				}
			}
		}

		if len(nativeCoins) == 0 {
			return alert, resolved
		}

		if cc.denomMetadata != nil {
			convertedCoins, err := utils.ConvertDecCoinToDisplayUnit(nativeCoins, *cc.denomMetadata)
			if err == nil {
				nativeCoins = *convertedCoins
			} else {
				l(fmt.Errorf("cannot convert rewards/commission to display unit for %s, err: %w", cc.name, err))
			}
		}

		totalAmount := github_com_cosmos_cosmos_sdk_types.ZeroDec()
		for _, coin := range nativeCoins {
			totalAmount = totalAmount.Add(coin.Amount)
		}

		if totalAmount.IsZero() {
			return alert, resolved
		}

		totalRewards := github_com_cosmos_cosmos_sdk_types.NewDecCoinFromDec(targetDenom, totalAmount)

		coinPrice, err := td.coinMarketCapClient.GetPrice(td.ctx, cc.Slug)
		if err == nil {
			totalRewardsConverted := totalRewards.Amount.MustFloat64() * coinPrice.Price
			threshold := floatVal(cc.Alerts.UnclaimedRewardsThreshold)

			alertID := fmt.Sprintf("UnclaimedRewards_%s", cc.ValAddress)
			const severity = "warning"
			if totalRewardsConverted > threshold {
				if !alarms.exist(cc.name, alertID) {
					message := fmt.Sprintf("%s has more than %.0f (%.0f currently) %s unclaimed rewards on %s",
						cc.valInfo.Moniker, threshold, totalRewardsConverted, td.PriceConversion.Currency, cc.name)
					td.alert(cc.name, message, severity, false, &alertID)
					alert = true
				}
			} else {
				if alarms.exist(cc.name, alertID) {
					message := fmt.Sprintf("%s has more than %.0f %s unclaimed rewards on %s",
						cc.valInfo.Moniker, threshold, td.PriceConversion.Currency, cc.name)
					td.alert(cc.name, message, severity, true, &alertID)
					resolved = true
				}
			}

			cc.activeAlerts = alarms.getCount(cc.name)
		}
	}

	return alert, resolved
}

func evaluateUnvotedGovernanceProposalAlert(cc *ChainConfig) (bool, bool) {
	alert, resolved := false, false

	idTemplate := "UnvotedGovernanceProposal_%s_%d"
	msgTemplate := "[WARNING] There is an open proposal (#%v) that the validator has not voted on %s%s"

	unvotedProposalMap := make(map[uint64]bool)
	for _, proposal := range cc.unvotedOpenGovProposals {
		unvotedProposalMap[proposal.ProposalId] = true
	}

	for _, proposal := range cc.unvotedOpenGovProposals {
		alertID := fmt.Sprintf(idTemplate, cc.ValAddress, proposal.ProposalId)
		deadline := fmt.Sprintf(", deadline: %s UTC", proposal.VotingEndTime.Format("2006-01-02 15:04"))
		if cc.Provider.Name == "namada" {
			deadline = ""
		}
		alertMsg := fmt.Sprintf(msgTemplate, proposal.ProposalId, cc.name, deadline)

		if !alarms.exist(cc.name, alertID) {
			td.alert(
				cc.name,
				alertMsg,
				"warning",
				false,
				&alertID,
			)
			alert = true
		}
	}

	messagesToBeResolved := make(map[uint64]string)

	alarms.notifyMux.RLock()

	if alarms.AllAlarms[cc.name] != nil {
		for alertID := range alarms.AllAlarms[cc.name] {
			if strings.HasPrefix(alertID, "UnvotedGovernanceProposal") {
				parts := strings.Split(alertID, "_")
				if proposalID, err := strconv.ParseUint(parts[len(parts)-1], 10, 64); err == nil {
					if !unvotedProposalMap[proposalID] {
						messagesToBeResolved[proposalID] = alertID
					}
				}
			}
		}
	}

	alarms.notifyMux.RUnlock()

	for _, alertID := range messagesToBeResolved {
		if alarms.exist(cc.name, alertID) {
			alertIDCopy := alertID // Create local copy to avoid implicit memory aliasing
			td.alert(
				cc.name,
				alarms.AllAlarms[cc.name][alertID].Message,
				"warning",
				true,
				&alertIDCopy,
			)
			resolved = true
		}
	}

	cc.activeAlerts = alarms.getCount(cc.name)
	return alert, resolved
}

// watch handles monitoring for missed blocks, stalled chain, node downtime
// and also updates a few prometheus stats
// FIXME: not watching for nodes that are lagging the head block!
func (cc *ChainConfig) watch() {
	// wait until we have a moniker:
	noNodesSec := 0
	for {
		if cc.valInfo == nil || cc.valInfo.Moniker == "not connected" {
			time.Sleep(time.Second)
			if boolVal(cc.Alerts.AlertIfNoServers) && cc.noNodes && noNodesSec >= 60*td.NodeDownMin {
				alertID := fmt.Sprintf("NoRPCEndpoints_%s", cc.ValAddress)
				if !alarms.exist(cc.name, alertID) {
					td.alert(
						cc.name,
						fmt.Sprintf("no RPC endpoints are working for %s", cc.ChainId),
						"critical",
						false,
						&alertID,
					)
				}
			}
			noNodesSec += 1
			continue
		}
		noNodesSec = 0
		break
	}

	// initial stat creation for nodes, we only update again if the node is positive
	if td.Prom {
		for _, node := range cc.Nodes {
			td.statsChan <- cc.mkUpdate(metricNodeDownSeconds, 0, node.Url)
		}
	}

	for {
		time.Sleep(2 * time.Second)

		// alert if we can't monitor
		if boolVal(cc.Alerts.AlertIfNoServers) {
			evaluateNoRPCEndpointsAlert(cc, &noNodesSec)
		}

		// stalled chain detection
		if boolVal(cc.Alerts.StalledAlerts) {
			evaluateChainStalledAlert(cc)
		}

		// jailed detection - only alert if it changes.
		if boolVal(cc.Alerts.AlertIfInactive) {
			evaluateValidatorInactiveAlert(cc)
		}

		// consecutive missed block alarms:
		if boolVal(cc.Alerts.ConsecutiveAlerts) {
			evaluateConsecutiveBlocksMissedAlert(cc)
		}

		// window percentage missed block alarms
		if boolVal(cc.Alerts.PercentageAlerts) {
			evaluatePercentageBlocksMissedAlert(cc)
		}

		// empty blocks alarm handling
		if boolVal(cc.Alerts.ConsecutiveEmptyAlerts) {
			evaluateConsecutiveEmptyBlocksAlert(cc)
		}

		// window percentage empty block alarms
		if boolVal(cc.Alerts.EmptyPercentageAlerts) {
			evaluatePercentageEmptyBlocksAlert(cc)
		}

		// node down alarms
		evaluateRPCNodeDownAlert(cc)

		// validator stake change alerts
		if boolVal(cc.Alerts.StakeChangeAlerts) {
			evaluateStakeChangeAlert(cc)
		}

		// validator unclaimed rewards alert
		if boolVal(cc.Alerts.UnclaimedRewardsAlerts) && td.PriceConversion.Enabled && cc.valInfo.SelfDelegationRewards != nil && cc.valInfo.Commission != nil {
			evaluateUnclaimedRewardsAlert(cc)
		}

		// there are open proposals that the validator has not voted on
		if boolVal(cc.Alerts.GovernanceAlerts) {
			evaluateUnvotedGovernanceProposalAlert(cc)
		}

		if td.Prom {
			// raw block timer, ignoring finalized state
			td.statsChan <- cc.mkUpdate(metricLastBlockSecondsNotFinal, time.Since(cc.lastBlockTime).Seconds(), "")
			// update node-down times for prometheus
			for _, node := range cc.Nodes {
				if node.down && !node.downSince.IsZero() {
					td.statsChan <- cc.mkUpdate(metricNodeDownSeconds, time.Since(node.downSince).Seconds(), node.Url)
				}
			}
		}
	}
}
