package tenderduty

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	gov "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// Helper function to create test config with minimal required fields
func createTestConfig() *Config {
	falseBool := false
	return &Config{
		chainsMux: sync.RWMutex{},
		Chains: map[string]*ChainConfig{
			"test-chain": {
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				Alerts: AlertConfig{
					Pagerduty: PDConfig{
						Enabled: &falseBool,
					},
					Discord: DiscordConfig{
						Enabled: &falseBool,
					},
					Telegram: TeleConfig{
						Enabled: &falseBool,
					},
					Slack: SlackConfig{
						Enabled: &falseBool,
					},
				},
			},
		},
		DefaultAlertConfig: AlertConfig{
			Pagerduty: PDConfig{
				Enabled: &falseBool,
			},
			Discord: DiscordConfig{
				Enabled: &falseBool,
			},
			Telegram: TeleConfig{
				Enabled: &falseBool,
			},
			Slack: SlackConfig{
				Enabled: &falseBool,
			},
		},
		alertChan: make(chan *alertMsg, 10),
	}
}

func TestSeverityThresholdToSeverities(t *testing.T) {
	tests := []struct {
		name      string
		threshold string
		expected  []string
	}{
		{
			name:      "critical threshold",
			threshold: "critical",
			expected:  []string{"critical"},
		},
		{
			name:      "warning threshold",
			threshold: "warning",
			expected:  []string{"critical", "warning"},
		},
		{
			name:      "info threshold",
			threshold: "info",
			expected:  []string{"critical", "warning", "info"},
		},
		{
			name:      "default threshold",
			threshold: "invalid",
			expected:  []string{"critical", "warning", "info"},
		},
		{
			name:      "case insensitive",
			threshold: "CRITICAL",
			expected:  []string{"critical"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SeverityThresholdToSeverities(tt.threshold)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("SeverityThresholdToSeverities(%s) = %v, want %v", tt.threshold, result, tt.expected)
			}
		})
	}
}

func TestAlarmCacheGetCount(t *testing.T) {
	cache := &alarmCache{
		AllAlarms: map[string]map[string]alertMsgCache{
			"chain1": {
				"alert1": {Message: "test1", SentTime: time.Now()},
				"alert2": {Message: "test2", SentTime: time.Now()},
			},
			"chain2": {
				"alert3": {Message: "test3", SentTime: time.Now()},
			},
		},
		notifyMux: sync.RWMutex{},
	}

	tests := []struct {
		name     string
		chain    string
		expected int
	}{
		{
			name:     "existing chain with alerts",
			chain:    "chain1",
			expected: 2,
		},
		{
			name:     "existing chain with one alert",
			chain:    "chain2",
			expected: 1,
		},
		{
			name:     "non-existing chain",
			chain:    "chain3",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cache.getCount(tt.chain)
			if result != tt.expected {
				t.Errorf("alarmCache.getCount(%s) = %v, want %v", tt.chain, result, tt.expected)
			}
		})
	}
}

func TestAlarmCacheClearAll(t *testing.T) {
	cache := &alarmCache{
		AllAlarms: map[string]map[string]alertMsgCache{
			"chain1": {
				"alert1": {Message: "test1", SentTime: time.Now()},
				"alert2": {Message: "test2", SentTime: time.Now()},
			},
		},
		notifyMux: sync.RWMutex{},
	}

	cache.clearAll("chain1")

	if len(cache.AllAlarms["chain1"]) != 0 {
		t.Errorf("Expected chain1 alerts to be cleared, but found %d alerts", len(cache.AllAlarms["chain1"]))
	}

	// Test clearing non-existing chain
	cache.clearAll("nonexistent")
	// Should not panic or cause errors
}

func TestShouldNotify(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		SentPdAlarms:   make(map[string]alertMsgCache),
		SentTgAlarms:   make(map[string]alertMsgCache),
		SentDiAlarms:   make(map[string]alertMsgCache),
		SentSlkAlarms:  make(map[string]alertMsgCache),
		AllAlarms:      make(map[string]map[string]alertMsgCache),
		flappingAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux:      sync.RWMutex{},
	}
	// Replace global alarms for testing
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	tests := []struct {
		name        string
		msg         *alertMsg
		dest        notifyDest
		setupAlarms func()
		expected    bool
		description string
	}{
		{
			name: "first alert should notify",
			msg: &alertMsg{
				uniqueId: "test_alert_1",
				severity: "critical",
				resolved: false,
				alertConfig: &AlertConfig{
					Pagerduty: PDConfig{SeverityThreshold: "critical"},
				},
			},
			dest:        pd,
			setupAlarms: func() {},
			expected:    true,
			description: "First time sending alert should return true",
		},
		{
			name: "duplicate alert should not notify",
			msg: &alertMsg{
				uniqueId: "test_alert_2",
				severity: "critical",
				resolved: false,
				alertConfig: &AlertConfig{
					Pagerduty: PDConfig{SeverityThreshold: "critical"},
				},
			},
			dest: pd,
			setupAlarms: func() {
				testAlarms.SentPdAlarms["test_alert_2"] = alertMsgCache{
					Message:  "Previous alert",
					SentTime: time.Now().Add(-1 * time.Hour),
				}
			},
			expected:    false,
			description: "Duplicate alert should not notify",
		},
		{
			name: "resolved alert should notify",
			msg: &alertMsg{
				uniqueId: "test_alert_3",
				severity: "critical",
				resolved: true,
				alertConfig: &AlertConfig{
					Pagerduty: PDConfig{SeverityThreshold: "critical"},
				},
			},
			dest: pd,
			setupAlarms: func() {
				testAlarms.SentPdAlarms["test_alert_3"] = alertMsgCache{
					Message:  "Previous alert",
					SentTime: time.Now().Add(-1 * time.Hour),
				}
			},
			expected:    true,
			description: "Resolved alert should notify to clear",
		},
		{
			name: "severity below threshold should not notify",
			msg: &alertMsg{
				uniqueId: "test_alert_4",
				severity: "info",
				resolved: false,
				alertConfig: &AlertConfig{
					Pagerduty: PDConfig{SeverityThreshold: "critical"},
				},
			},
			dest:        pd,
			setupAlarms: func() {},
			expected:    false,
			description: "Alert with severity below threshold should not notify",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.SentPdAlarms = make(map[string]alertMsgCache)
			testAlarms.SentTgAlarms = make(map[string]alertMsgCache)
			testAlarms.SentDiAlarms = make(map[string]alertMsgCache)
			testAlarms.SentSlkAlarms = make(map[string]alertMsgCache)
			testAlarms.flappingAlarms = make(map[string]map[string]alertMsgCache)

			tt.setupAlarms()

			result := shouldNotify(tt.msg, tt.dest)
			if result != tt.expected {
				t.Errorf("%s: shouldNotify() = %v, want %v", tt.description, result, tt.expected)
			}
		})
	}
}

func TestBuildSlackMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      *alertMsg
		expected *SlackMessage
	}{
		{
			name: "alert message",
			msg: &alertMsg{
				chain:       "test-chain",
				message:     "Test alert message",
				resolved:    false,
				slkMentions: "@here",
			},
			expected: &SlackMessage{
				Text: "Test alert message",
				Attachments: []Attachment{
					{
						Title: "TenderDuty 🚨 ALERT:  test-chain @here",
						Color: "danger",
					},
				},
			},
		},
		{
			name: "resolved message",
			msg: &alertMsg{
				chain:       "test-chain",
				message:     "Test resolved message",
				resolved:    true,
				slkMentions: "@here",
			},
			expected: &SlackMessage{
				Text: "OK: Test resolved message",
				Attachments: []Attachment{
					{
						Title: "TenderDuty 💜 Resolved:  test-chain @here",
						Color: "good",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSlackMessage(tt.msg)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("buildSlackMessage() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestBuildDiscordMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      *alertMsg
		expected *DiscordMessage
	}{
		{
			name: "alert message",
			msg: &alertMsg{
				chain:    "test-chain",
				message:  "Test alert message",
				resolved: false,
			},
			expected: &DiscordMessage{
				Username: "Tenderduty",
				Content:  "🚨 ALERT: test-chain",
				Embeds: []DiscordEmbed{
					{
						Description: "Test alert message",
					},
				},
			},
		},
		{
			name: "resolved message",
			msg: &alertMsg{
				chain:    "test-chain",
				message:  "Test resolved message",
				resolved: true,
			},
			expected: &DiscordMessage{
				Username: "Tenderduty",
				Content:  "💜 Resolved: test-chain",
				Embeds: []DiscordEmbed{
					{
						Description: "Test resolved message",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDiscordMessage(tt.msg)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("buildDiscordMessage() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestNotifySlack(t *testing.T) {
	tests := []struct {
		name           string
		msg            *alertMsg
		serverResponse int
		expectError    bool
	}{
		{
			name: "successful notification",
			msg: &alertMsg{
				slk:         true,
				chain:       "test-chain",
				message:     "test message",
				resolved:    false,
				slkMentions: "@here",
				slkHook:     "", // will be set to test server URL
			},
			serverResponse: 200,
			expectError:    false,
		},
		{
			name: "server error",
			msg: &alertMsg{
				slk:         true,
				chain:       "test-chain",
				message:     "test message",
				resolved:    false,
				slkMentions: "@here",
				slkHook:     "", // will be set to test server URL
			},
			serverResponse: 500,
			expectError:    true,
		},
		{
			name: "slack disabled",
			msg: &alertMsg{
				slk: false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.slk {
				// Create test server
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.serverResponse)
				}))
				defer server.Close()
				tt.msg.slkHook = server.URL
			}

			err := notifySlack(tt.msg)
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestConfigAlert(t *testing.T) {
	// Create test config
	config := &Config{
		alertChan: make(chan *alertMsg, 10),
		chainsMux: sync.RWMutex{},
		Chains: map[string]*ChainConfig{
			"test-chain": {
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				Alerts: AlertConfig{
					Pagerduty: PDConfig{
						Enabled: &[]bool{true}[0],
						ApiKey:  "test-key",
					},
					Discord: DiscordConfig{
						Enabled: &[]bool{false}[0],
					},
					Telegram: TeleConfig{
						Enabled: &[]bool{true}[0],
						ApiKey:  "tg-key",
						Channel: "test-channel",
					},
					Slack: SlackConfig{
						Enabled: &[]bool{false}[0],
					},
				},
			},
		},
		DefaultAlertConfig: AlertConfig{
			Pagerduty: PDConfig{
				Enabled: &[]bool{true}[0],
			},
			Discord: DiscordConfig{
				Enabled: &[]bool{true}[0],
			},
			Telegram: TeleConfig{
				Enabled: &[]bool{true}[0],
			},
			Slack: SlackConfig{
				Enabled: &[]bool{true}[0],
			},
		},
	}

	alertID := "test_alert_id"
	config.alert("test-chain", "test message", "critical", false, &alertID)

	// Check that alert was sent to channel
	select {
	case alertMsg := <-config.alertChan:
		if alertMsg.message != "test message" {
			t.Errorf("Expected message 'test message', got '%s'", alertMsg.message)
		}
		if alertMsg.severity != "critical" {
			t.Errorf("Expected severity 'critical', got '%s'", alertMsg.severity)
		}
		if alertMsg.uniqueId != "test_alert_id" {
			t.Errorf("Expected uniqueId 'test_alert_id', got '%s'", alertMsg.uniqueId)
		}
		if alertMsg.pd != true {
			t.Errorf("Expected pagerduty to be enabled")
		}
		if alertMsg.tg != true {
			t.Errorf("Expected telegram to be enabled")
		}
		if alertMsg.disc != false {
			t.Errorf("Expected discord to be disabled")
		}
		if alertMsg.slk != false {
			t.Errorf("Expected slack to be disabled")
		}
	case <-time.After(time.Second):
		t.Error("Alert was not sent to channel")
	}
}

func TestApplyAlertDefaultsCustom(t *testing.T) {
	// Create default config
	defaultConfig := &AlertConfig{
		Stalled:           &[]int{10}[0],
		StalledAlerts:     &[]bool{true}[0],
		ConsecutiveMissed: &[]int{5}[0],
		ConsecutiveAlerts: &[]bool{true}[0],
		Pagerduty: PDConfig{
			Enabled:         &[]bool{true}[0],
			DefaultSeverity: "critical",
		},
	}

	// Create chain config with some values set
	chainConfig := &AlertConfig{
		Stalled: &[]int{15}[0], // This should not be overridden
		// StalledAlerts not set, should get default
		// ConsecutiveMissed not set, should get default
		ConsecutiveAlerts: &[]bool{false}[0], // This should not be overridden
		Pagerduty: PDConfig{
			Enabled: &[]bool{false}[0], // This should not be overridden
			// DefaultSeverity not set, should get default
		},
	}

	applyAlertDefaults(chainConfig, defaultConfig)

	// Check that zero values were filled in
	if *chainConfig.Stalled != 15 {
		t.Errorf("Expected Stalled to remain 15, got %d", *chainConfig.Stalled)
	}
	if *chainConfig.StalledAlerts != true {
		t.Errorf("Expected StalledAlerts to be set to default true")
	}
	if *chainConfig.ConsecutiveMissed != 5 {
		t.Errorf("Expected ConsecutiveMissed to be set to default 5, got %d", *chainConfig.ConsecutiveMissed)
	}
	if *chainConfig.ConsecutiveAlerts != false {
		t.Errorf("Expected ConsecutiveAlerts to remain false")
	}
	if *chainConfig.Pagerduty.Enabled != false {
		t.Errorf("Expected Pagerduty.Enabled to remain false")
	}
	if chainConfig.Pagerduty.DefaultSeverity != "critical" {
		t.Errorf("Expected Pagerduty.DefaultSeverity to be set to default 'critical', got '%s'", chainConfig.Pagerduty.DefaultSeverity)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		expectFatal   bool
		expectWarning bool
		description   string
	}{
		{
			name: "valid config",
			config: &Config{
				EnableDash:  true,
				Listen:      "http://localhost:8080",
				NodeDownMin: 5,
				DefaultAlertConfig: AlertConfig{
					Pagerduty: PDConfig{
						Enabled: &[]bool{true}[0],
						ApiKey:  "ValidKeyWithoutSpecialChars",
					},
				},
				GovernanceAlertsReminderInterval: 6,
				Chains: map[string]*ChainConfig{
					"test": {
						ChainId: "test-1",
					},
				},
			},
			expectFatal:   false,
			expectWarning: false,
			description:   "Valid configuration should not produce errors or warnings",
		},
		{
			name: "invalid pagerduty key",
			config: &Config{
				DefaultAlertConfig: AlertConfig{
					Pagerduty: PDConfig{
						Enabled: &[]bool{true}[0],
						ApiKey:  "invalid+key-with_special",
					},
				},
				Chains: map[string]*ChainConfig{
					"test": {
						ChainId: "test-1",
					},
				},
			},
			expectFatal: true,
			description: "Invalid PagerDuty key should produce fatal error",
		},
		{
			name: "node down minutes too low",
			config: &Config{
				NodeDownMin: 2,
				Chains: map[string]*ChainConfig{
					"test": {
						ChainId: "test-1",
					},
				},
			},
			expectWarning: true,
			description:   "NodeDownMin < 3 should produce warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set default values that might be modified by validateConfig
			if tt.config.GovernanceAlertsReminderInterval == 0 {
				tt.config.GovernanceAlertsReminderInterval = 0 // Let validateConfig set it
			}

			fatal, problems := validateConfig(tt.config)

			if tt.expectFatal && !fatal {
				t.Errorf("%s: Expected fatal error but got none. Problems: %v", tt.description, problems)
			}
			if !tt.expectFatal && fatal {
				t.Errorf("%s: Expected no fatal error but got one. Problems: %v", tt.description, problems)
			}
			if tt.expectWarning && len(problems) == 0 {
				t.Errorf("%s: Expected warnings but got none", tt.description)
			}

			// Check that governance reminder interval is set to default when invalid
			if tt.config.GovernanceAlertsReminderInterval <= 0 {
				if tt.config.GovernanceAlertsReminderInterval != 6 {
					t.Errorf("Expected GovernanceAlertsReminderInterval to be set to 6, got %d", tt.config.GovernanceAlertsReminderInterval)
				}
			}
		})
	}
}

// TestChainConfigMkUpdate tests the mkUpdate method
func TestChainConfigMkUpdate(t *testing.T) {
	cc := &ChainConfig{
		name:    "test-chain",
		ChainId: "test-chain-1",
		valInfo: &ValInfo{
			Moniker: "test-validator",
		},
	}

	update := cc.mkUpdate(metricNodeDownSeconds, 123.45, "http://node1.example.com")

	if update.metric != metricNodeDownSeconds {
		t.Errorf("Expected metric to be metricNodeDownSeconds")
	}
	if update.counter != 123.45 {
		t.Errorf("Expected counter to be 123.45, got %f", update.counter)
	}
	if update.name != "test-chain" {
		t.Errorf("Expected name to be 'test-chain', got '%s'", update.name)
	}
	if update.chainId != "test-chain-1" {
		t.Errorf("Expected chainId to be 'test-chain-1', got '%s'", update.chainId)
	}
	if update.moniker != "test-validator" {
		t.Errorf("Expected moniker to be 'test-validator', got '%s'", update.moniker)
	}
	if update.endpoint != "http://node1.example.com" {
		t.Errorf("Expected endpoint to be 'http://node1.example.com', got '%s'", update.endpoint)
	}
}

func TestEvaluateConsecutiveBlocksMissedAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name                   string
		consecutiveMiss        float64
		consecutiveMissedAlert int
		existingAlert          bool
		expectedAlert          bool
		expectedResolved       bool
		description            string
	}{
		{
			name:                   "should trigger alert when consecutive misses exceed threshold",
			consecutiveMiss:        5,
			consecutiveMissedAlert: 3,
			existingAlert:          false,
			expectedAlert:          true,
			expectedResolved:       false,
			description:            "Alert should trigger when consecutive misses exceed threshold",
		},
		{
			name:                   "should not trigger duplicate alert",
			consecutiveMiss:        5,
			consecutiveMissedAlert: 3,
			existingAlert:          true,
			expectedAlert:          false,
			expectedResolved:       false,
			description:            "Should not trigger duplicate alert when already exists",
		},
		{
			name:                   "should resolve alert when consecutive misses drop below threshold",
			consecutiveMiss:        2,
			consecutiveMissedAlert: 3,
			existingAlert:          true,
			expectedAlert:          false,
			expectedResolved:       true,
			description:            "Should resolve alert when consecutive misses drop below threshold",
		},
		{
			name:                   "should not resolve non-existing alert",
			consecutiveMiss:        2,
			consecutiveMissedAlert: 3,
			existingAlert:          false,
			expectedAlert:          false,
			expectedResolved:       false,
			description:            "Should not resolve alert that doesn't exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:                "test-chain",
				ChainId:             "test-chain-1",
				ValAddress:          "testval123",
				statConsecutiveMiss: tt.consecutiveMiss,
				valInfo:             &ValInfo{Moniker: "test-validator"},
				Alerts: AlertConfig{
					ConsecutiveMissed:   &tt.consecutiveMissedAlert,
					ConsecutivePriority: "critical",
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["ConsecutiveBlocksMissed_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluateConsecutiveBlocksMissedAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluatePercentageBlocksMissedAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name             string
		missed           int64
		window           int64
		windowThreshold  int
		existingAlert    bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name:             "should trigger alert when percentage exceeds threshold",
			missed:           15,
			window:           100,
			windowThreshold:  10,
			existingAlert:    false,
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Alert should trigger when missed percentage exceeds threshold",
		},
		{
			name:             "should not trigger duplicate alert",
			missed:           15,
			window:           100,
			windowThreshold:  10,
			existingAlert:    true,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not trigger duplicate alert when already exists",
		},
		{
			name:             "should resolve alert when percentage drops below threshold",
			missed:           5,
			window:           100,
			windowThreshold:  10,
			existingAlert:    true,
			expectedAlert:    false,
			expectedResolved: true,
			description:      "Should resolve alert when percentage drops below threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				valInfo: &ValInfo{
					Moniker: "test-validator",
					Missed:  tt.missed,
					Window:  tt.window,
				},
				Alerts: AlertConfig{
					Window:             &tt.windowThreshold,
					PercentagePriority: "warning",
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["PercentageBlocksMissed_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluatePercentageBlocksMissedAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateChainStalledAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name             string
		lastBlockTime    time.Time
		stalledMinutes   int
		lastBlockAlarm   bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name:             "should trigger alert when chain is stalled",
			lastBlockTime:    time.Now().Add(-15 * time.Minute),
			stalledMinutes:   10,
			lastBlockAlarm:   false,
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should trigger alert when chain has been stalled longer than threshold",
		},
		{
			name:             "should not trigger duplicate alert",
			lastBlockTime:    time.Now().Add(-15 * time.Minute),
			stalledMinutes:   10,
			lastBlockAlarm:   true,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not trigger duplicate alert when already alarmed",
		},
		{
			name:             "should resolve alert when chain recovers",
			lastBlockTime:    time.Now().Add(-5 * time.Minute),
			stalledMinutes:   10,
			lastBlockAlarm:   true,
			expectedAlert:    false,
			expectedResolved: true,
			description:      "Should resolve alert when chain recovers from stall",
		},
		{
			name:             "should handle zero lastBlockTime",
			lastBlockTime:    time.Time{},
			stalledMinutes:   10,
			lastBlockAlarm:   false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should handle zero lastBlockTime gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:           "test-chain",
				ChainId:        "test-chain-1",
				ValAddress:     "testval123",
				lastBlockTime:  tt.lastBlockTime,
				lastBlockAlarm: tt.lastBlockAlarm,
				Alerts: AlertConfig{
					Stalled: &tt.stalledMinutes,
				},
			}

			alert, resolved := evaluateChainStalledAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateValidatorInactiveAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name             string
		currentBonded    bool
		previousBonded   bool
		tombstoned       bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name:             "should trigger alert when validator becomes inactive",
			currentBonded:    false,
			previousBonded:   true,
			tombstoned:       false,
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should alert when validator becomes jailed",
		},
		{
			name:             "should trigger alert when validator becomes tombstoned",
			currentBonded:    false,
			previousBonded:   true,
			tombstoned:       true,
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should alert when validator becomes tombstoned",
		},
		{
			name:             "should resolve alert when validator becomes active",
			currentBonded:    true,
			previousBonded:   false,
			tombstoned:       false,
			expectedAlert:    false,
			expectedResolved: true,
			description:      "Should resolve alert when validator becomes active again",
		},
		{
			name:             "should not alert if no state change",
			currentBonded:    true,
			previousBonded:   true,
			tombstoned:       false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert if validator state hasn't changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				valInfo: &ValInfo{
					Moniker:    "test-validator",
					Bonded:     tt.currentBonded,
					Tombstoned: tt.tombstoned,
				},
				lastValInfo: &ValInfo{
					Moniker: "test-validator",
					Bonded:  tt.previousBonded,
				},
			}

			alert, resolved := evaluateValidatorInactiveAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateConsecutiveEmptyBlocksAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name                      string
		consecutiveEmpty          float64
		consecutiveEmptyThreshold int
		existingAlert             bool
		expectedAlert             bool
		expectedResolved          bool
		description               string
	}{
		{
			name:                      "should trigger alert when consecutive empty blocks exceed threshold",
			consecutiveEmpty:          5,
			consecutiveEmptyThreshold: 3,
			existingAlert:             false,
			expectedAlert:             true,
			expectedResolved:          false,
			description:               "Should alert when consecutive empty blocks exceed threshold",
		},
		{
			name:                      "should not trigger duplicate alert",
			consecutiveEmpty:          5,
			consecutiveEmptyThreshold: 3,
			existingAlert:             true,
			expectedAlert:             false,
			expectedResolved:          false,
			description:               "Should not trigger duplicate alert",
		},
		{
			name:                      "should resolve alert when consecutive empty blocks drop below threshold",
			consecutiveEmpty:          2,
			consecutiveEmptyThreshold: 3,
			existingAlert:             true,
			expectedAlert:             false,
			expectedResolved:          true,
			description:               "Should resolve alert when consecutive empty blocks drop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:                 "test-chain",
				ChainId:              "test-chain-1",
				ValAddress:           "testval123",
				statConsecutiveEmpty: tt.consecutiveEmpty,
				valInfo:              &ValInfo{Moniker: "test-validator"},
				Alerts: AlertConfig{
					ConsecutiveEmpty:         &tt.consecutiveEmptyThreshold,
					ConsecutiveEmptyPriority: "warning",
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["ConsecutiveEmptyBlocks_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluateConsecutiveEmptyBlocksAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluatePercentageEmptyBlocksAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name                 string
		totalProps           float64
		totalPropsEmpty      float64
		emptyWindowThreshold int
		existingAlert        bool
		expectedAlert        bool
		expectedResolved     bool
		description          string
	}{
		{
			name:                 "should trigger alert when empty percentage exceeds threshold",
			totalProps:           100,
			totalPropsEmpty:      15,
			emptyWindowThreshold: 10,
			existingAlert:        false,
			expectedAlert:        true,
			expectedResolved:     false,
			description:          "Should alert when empty block percentage exceeds threshold",
		},
		{
			name:                 "should not trigger duplicate alert",
			totalProps:           100,
			totalPropsEmpty:      15,
			emptyWindowThreshold: 10,
			existingAlert:        true,
			expectedAlert:        false,
			expectedResolved:     false,
			description:          "Should not trigger duplicate alert",
		},
		{
			name:                 "should resolve alert when percentage drops below threshold",
			totalProps:           100,
			totalPropsEmpty:      5,
			emptyWindowThreshold: 10,
			existingAlert:        true,
			expectedAlert:        false,
			expectedResolved:     true,
			description:          "Should resolve alert when percentage drops below threshold",
		},
		{
			name:                 "should handle zero total props",
			totalProps:           0,
			totalPropsEmpty:      0,
			emptyWindowThreshold: 10,
			existingAlert:        false,
			expectedAlert:        false,
			expectedResolved:     false,
			description:          "Should handle zero total props gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:                "test-chain",
				ChainId:             "test-chain-1",
				ValAddress:          "testval123",
				statTotalProps:      tt.totalProps,
				statTotalPropsEmpty: tt.totalPropsEmpty,
				valInfo:             &ValInfo{Moniker: "test-validator"},
				Alerts: AlertConfig{
					EmptyWindow:             &tt.emptyWindowThreshold,
					EmptyPercentagePriority: "warning",
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["PercentageEmptyBlocks_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluatePercentageEmptyBlocksAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateNoRPCEndpointsAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	td.NodeDownMin = 1
	td.NodeDownSeverity = "warning"
	defer func() { td = originalTd }()

	tests := []struct {
		name               string
		noNodes            bool
		noNodesSec         int
		existingAlert      bool
		expectedAlert      bool
		expectedResolved   bool
		expectedNoNodesSec int
		description        string
	}{
		{
			name:               "should not trigger if node is down but not for enough waiting time",
			noNodes:            true,
			noNodesSec:         0,
			existingAlert:      false,
			expectedAlert:      false,
			expectedResolved:   false,
			expectedNoNodesSec: 2,
			description:        "Should not alert when nodes have been down shorter than threshold",
		},
		{
			name:               "should trigger when node is down for enough waiting time",
			noNodes:            true,
			noNodesSec:         60, // in this test we have set td.NodeDownMin = 1
			existingAlert:      false,
			expectedAlert:      true,
			expectedResolved:   false,
			expectedNoNodesSec: 62, // evaluation is done every 2 seconds
			description:        "Should alert when nodes have been down longer than threshold",
		},
		{
			name:               "should not trigger duplicate alert",
			noNodes:            true,
			noNodesSec:         120,
			existingAlert:      true,
			expectedAlert:      false,
			expectedResolved:   false,
			expectedNoNodesSec: 122,
			description:        "Should not trigger duplicate alert",
		},
		{
			name:               "should resolve alert when nodes recover",
			noNodes:            false,
			noNodesSec:         120,
			existingAlert:      true,
			expectedAlert:      false,
			expectedResolved:   true,
			expectedNoNodesSec: 0,
			description:        "Should resolve alert when nodes recover",
		},
		{
			name:               "should increment counter when nodes still down but below threshold",
			noNodes:            true,
			noNodesSec:         30,
			existingAlert:      false,
			expectedAlert:      false,
			expectedResolved:   false,
			expectedNoNodesSec: 32,
			description:        "Should increment counter but not alert when below threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				noNodes:    tt.noNodes,
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["NoRPCEndpoints_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			noNodesSec := tt.noNodesSec
			alert, resolved := evaluateNoRPCEndpointsAlert(cc, &noNodesSec)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
			if noNodesSec != tt.expectedNoNodesSec {
				t.Errorf("%s: expected noNodesSec %d, got %d", tt.description, tt.expectedNoNodesSec, noNodesSec)
			}
		})
	}
}

func TestEvaluateRPCNodeDownAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	td.NodeDownMin = 2
	td.NodeDownSeverity = "warning"
	defer func() { td = originalTd }()

	tests := []struct {
		name             string
		nodes            []*NodeConfig
		existingAlert    bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name: "should trigger alert when node is down longer than threshold",
			nodes: []*NodeConfig{
				{
					Url:         "http://node1.example.com",
					AlertIfDown: true,
					down:        true,
					wasDown:     false,
					downSince:   time.Now().Add(-5 * time.Minute),
				},
			},
			existingAlert:    false,
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should alert when node is down longer than threshold",
		},
		{
			name: "should not trigger duplicate alert",
			nodes: []*NodeConfig{
				{
					Url:         "http://node1.example.com",
					AlertIfDown: true,
					down:        true,
					wasDown:     false,
					downSince:   time.Now().Add(-5 * time.Minute),
				},
			},
			existingAlert:    true,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not trigger duplicate alert",
		},
		{
			name: "should resolve alert when node recovers",
			nodes: []*NodeConfig{
				{
					Url:         "http://node1.example.com",
					AlertIfDown: true,
					down:        false,
					wasDown:     true,
					downSince:   time.Now().Add(-5 * time.Minute),
				},
			},
			existingAlert:    true,
			expectedAlert:    false,
			expectedResolved: true,
			description:      "Should resolve alert when node recovers",
		},
		{
			name: "should not alert if AlertIfDown is false",
			nodes: []*NodeConfig{
				{
					Url:         "http://node1.example.com",
					AlertIfDown: false,
					down:        true,
					wasDown:     false,
					downSince:   time.Now().Add(-5 * time.Minute),
				},
			},
			existingAlert:    false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert if AlertIfDown is disabled",
		},
		{
			name: "should not alert if node just went down",
			nodes: []*NodeConfig{
				{
					Url:         "http://node1.example.com",
					AlertIfDown: true,
					down:        true,
					wasDown:     false,
					downSince:   time.Now().Add(-30 * time.Second),
				},
			},
			existingAlert:    false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert if node hasn't been down long enough",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				Nodes:      tt.nodes,
			}

			if tt.existingAlert && len(tt.nodes) > 0 {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				alertID := fmt.Sprintf("RPCNodeDown_%s_%s", cc.ValAddress, tt.nodes[0].Url)
				testAlarms.AllAlarms["test-chain"][alertID] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluateRPCNodeDownAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateStakeChangeAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name              string
		currentStake      float64
		previousStake     float64
		increaseThreshold float64
		dropThreshold     float64
		existingAlert     bool
		expectedAlert     bool
		expectedResolved  bool
		description       string
	}{
		{
			name:              "should trigger alert when stake increases above threshold",
			currentStake:      1200.0,
			previousStake:     1000.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     false,
			expectedAlert:     true,
			expectedResolved:  false,
			description:       "Should alert when stake increases by more than 15%",
		},
		{
			name:              "should trigger alert when stake drops below threshold",
			currentStake:      800.0,
			previousStake:     1000.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.15,
			existingAlert:     false,
			expectedAlert:     true,
			expectedResolved:  false,
			description:       "Should alert when stake drops by more than 15%",
		},
		{
			name:              "should not trigger duplicate alert",
			currentStake:      1200.0,
			previousStake:     1000.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     true,
			expectedAlert:     false,
			expectedResolved:  false,
			description:       "Should not trigger duplicate alert",
		},
		{
			name:              "should resolve alert when stake change is within threshold",
			currentStake:      1050.0,
			previousStake:     1000.0,
			increaseThreshold: 0.10,
			dropThreshold:     0.10,
			existingAlert:     true,
			expectedAlert:     false,
			expectedResolved:  true,
			description:       "Should resolve alert when stake change is within acceptable range",
		},
		{
			name:              "should not alert when change is within threshold",
			currentStake:      1050.0,
			previousStake:     1000.0,
			increaseThreshold: 0.10,
			dropThreshold:     0.10,
			existingAlert:     false,
			expectedAlert:     false,
			expectedResolved:  false,
			description:       "Should not alert when stake change is within threshold",
		},
		{
			name:              "should not trigger alert when previous stake is zero (avoid division by zero)",
			currentStake:      1000.0,
			previousStake:     0.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     false,
			expectedAlert:     false,
			expectedResolved:  false,
			description:       "Should not alert and avoid division by zero when previous stake is zero",
		},
		{
			name:              "should not process alert when previous stake becomes zero",
			currentStake:      1000.0,
			previousStake:     0.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     true,
			expectedAlert:     false,
			expectedResolved:  false,
			description:       "Should not process alert when previous stake becomes zero to avoid division by zero",
		},
		{
			name:              "should not trigger alert when both current and previous stakes are zero",
			currentStake:      0.0,
			previousStake:     0.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     false,
			expectedAlert:     false,
			expectedResolved:  false,
			description:       "Should not alert when both stakes are zero",
		},
		{
			name:              "should trigger alert when current stake drops to zero",
			currentStake:      0.0,
			previousStake:     1000.0,
			increaseThreshold: 0.15,
			dropThreshold:     0.10,
			existingAlert:     false,
			expectedAlert:     true,
			expectedResolved:  false,
			description:       "Should trigger alert when stake drops to zero (100% drop)",
		},
	}

	// Add test cases for nil value scenarios to ensure robustness
	nilTests := []struct {
		name             string
		valInfo          *ValInfo
		lastValInfo      *ValInfo
		existingAlert    bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name:             "should not trigger alert when valInfo is nil",
			valInfo:          nil,
			lastValInfo:      &ValInfo{DelegatedTokens: 1000.0},
			existingAlert:    false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert when valInfo is nil",
		},
		{
			name:             "should not trigger alert when lastValInfo is nil",
			valInfo:          &ValInfo{DelegatedTokens: 1000.0},
			lastValInfo:      nil,
			existingAlert:    false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert when lastValInfo is nil",
		},
		{
			name:             "should not trigger alert when both valInfo and lastValInfo are nil",
			valInfo:          nil,
			lastValInfo:      nil,
			existingAlert:    false,
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not alert when both valInfo and lastValInfo are nil",
		},
	}

	// Run the nil value tests
	for _, tt := range nilTests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				valInfo:    tt.valInfo,
				lastValInfo: tt.lastValInfo,
				Alerts: AlertConfig{
					StakeChangeIncreaseThreshold: &[]float64{0.15}[0],
					StakeChangeDropThreshold:     &[]float64{0.10}[0],
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["StakeChange_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluateStakeChangeAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			cc := &ChainConfig{
				name:       "test-chain",
				ChainId:    "test-chain-1",
				ValAddress: "testval123",
				valInfo: &ValInfo{
					Moniker:         "test-validator",
					DelegatedTokens: tt.currentStake,
				},
				lastValInfo: &ValInfo{
					DelegatedTokens: tt.previousStake,
				},
				Alerts: AlertConfig{
					StakeChangeIncreaseThreshold: &tt.increaseThreshold,
					StakeChangeDropThreshold:     &tt.dropThreshold,
				},
			}

			if tt.existingAlert {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				testAlarms.AllAlarms["test-chain"]["StakeChange_testval123"] = alertMsgCache{
					Message:  "test alert",
					SentTime: time.Now(),
				}
			}

			alert, resolved := evaluateStakeChangeAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}

func TestEvaluateUnvotedGovernanceProposalAlert(t *testing.T) {
	// Setup test alarm cache
	testAlarms := &alarmCache{
		AllAlarms: make(map[string]map[string]alertMsgCache),
		notifyMux: sync.RWMutex{},
	}
	originalAlarms := alarms
	alarms = testAlarms
	defer func() { alarms = originalAlarms }()

	// Setup test td
	originalTd := td
	td = createTestConfig()
	defer func() { td = originalTd }()

	tests := []struct {
		name             string
		unvotedProposals []gov.Proposal
		existingAlerts   map[string]bool
		expectedAlert    bool
		expectedResolved bool
		description      string
	}{
		{
			name: "should trigger alert for new unvoted proposal",
			unvotedProposals: []gov.Proposal{
				{
					ProposalId:    1,
					VotingEndTime: time.Now().Add(24 * time.Hour),
				},
			},
			existingAlerts:   map[string]bool{},
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should alert for new unvoted governance proposal",
		},
		{
			name: "should not trigger duplicate alert",
			unvotedProposals: []gov.Proposal{
				{
					ProposalId:    1,
					VotingEndTime: time.Now().Add(24 * time.Hour),
				},
			},
			existingAlerts: map[string]bool{
				"UnvotedGovernanceProposal_testval123_1": true,
			},
			expectedAlert:    false,
			expectedResolved: false,
			description:      "Should not trigger duplicate alert for same proposal",
		},
		{
			name:             "should resolve alert when proposal is voted on",
			unvotedProposals: []gov.Proposal{},
			existingAlerts: map[string]bool{
				"UnvotedGovernanceProposal_testval123_1": true,
			},
			expectedAlert:    false,
			expectedResolved: true,
			description:      "Should resolve alert when proposal is no longer unvoted",
		},
		{
			name: "should handle multiple proposals",
			unvotedProposals: []gov.Proposal{
				{
					ProposalId:    1,
					VotingEndTime: time.Now().Add(24 * time.Hour),
				},
				{
					ProposalId:    2,
					VotingEndTime: time.Now().Add(48 * time.Hour),
				},
			},
			existingAlerts:   map[string]bool{},
			expectedAlert:    true,
			expectedResolved: false,
			description:      "Should handle multiple unvoted proposals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset alarms for each test
			testAlarms.AllAlarms = make(map[string]map[string]alertMsgCache)

			if len(tt.existingAlerts) > 0 {
				testAlarms.AllAlarms["test-chain"] = make(map[string]alertMsgCache)
				for alertID := range tt.existingAlerts {
					testAlarms.AllAlarms["test-chain"][alertID] = alertMsgCache{
						Message:  "test governance alert",
						SentTime: time.Now(),
					}
				}
			}

			cc := &ChainConfig{
				name:                    "test-chain",
				ChainId:                 "test-chain-1",
				ValAddress:              "testval123",
				unvotedOpenGovProposals: tt.unvotedProposals,
				Provider:                ProviderConfig{Name: "cosmos"},
			}

			alert, resolved := evaluateUnvotedGovernanceProposalAlert(cc)

			if alert != tt.expectedAlert {
				t.Errorf("%s: expected alert %v, got %v", tt.description, tt.expectedAlert, alert)
			}
			if resolved != tt.expectedResolved {
				t.Errorf("%s: expected resolved %v, got %v", tt.description, tt.expectedResolved, resolved)
			}
		})
	}
}
