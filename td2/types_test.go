package tenderduty

import (
	"fmt"
	"reflect"
	"testing"
)

// alertConfigsEqual compares two AlertConfig structs, handling pointer comparisons properly
func alertConfigsEqual(a, b AlertConfig) bool {
	return intPtrEqual(a.Stalled, b.Stalled) &&
		boolPtrEqual(a.StalledAlerts, b.StalledAlerts) &&
		intPtrEqual(a.ConsecutiveMissed, b.ConsecutiveMissed) &&
		a.ConsecutivePriority == b.ConsecutivePriority &&
		boolPtrEqual(a.ConsecutiveAlerts, b.ConsecutiveAlerts) &&
		boolPtrEqual(a.ConsecutiveMissExcludePreactions, b.ConsecutiveMissExcludePreactions) &&
		intPtrEqual(a.Window, b.Window) &&
		a.PercentagePriority == b.PercentagePriority &&
		boolPtrEqual(a.PercentageAlerts, b.PercentageAlerts) &&
		intPtrEqual(a.ConsecutiveEmpty, b.ConsecutiveEmpty) &&
		a.ConsecutiveEmptyPriority == b.ConsecutiveEmptyPriority &&
		boolPtrEqual(a.ConsecutiveEmptyAlerts, b.ConsecutiveEmptyAlerts) &&
		intPtrEqual(a.EmptyWindow, b.EmptyWindow) &&
		a.EmptyPercentagePriority == b.EmptyPercentagePriority &&
		boolPtrEqual(a.EmptyPercentageAlerts, b.EmptyPercentageAlerts) &&
		boolPtrEqual(a.AlertIfInactive, b.AlertIfInactive) &&
		boolPtrEqual(a.AlertIfNoServers, b.AlertIfNoServers) &&
		boolPtrEqual(a.GovernanceAlerts, b.GovernanceAlerts) &&
		boolPtrEqual(a.StakeChangeAlerts, b.StakeChangeAlerts) &&
		floatPtrEqual(a.StakeChangeDropThreshold, b.StakeChangeDropThreshold) &&
		floatPtrEqual(a.StakeChangeIncreaseThreshold, b.StakeChangeIncreaseThreshold) &&
		boolPtrEqual(a.UnclaimedRewardsAlerts, b.UnclaimedRewardsAlerts) &&
		floatPtrEqual(a.UnclaimedRewardsThreshold, b.UnclaimedRewardsThreshold) &&
		pdConfigsEqual(a.Pagerduty, b.Pagerduty) &&
		discordConfigsEqual(a.Discord, b.Discord) &&
		teleConfigsEqual(a.Telegram, b.Telegram) &&
		slackConfigsEqual(a.Slack, b.Slack)
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func pdConfigsEqual(a, b PDConfig) bool {
	return boolPtrEqual(a.Enabled, b.Enabled) &&
		a.ApiKey == b.ApiKey &&
		a.DefaultSeverity == b.DefaultSeverity &&
		a.SeverityThreshold == b.SeverityThreshold
}

func discordConfigsEqual(a, b DiscordConfig) bool {
	return boolPtrEqual(a.Enabled, b.Enabled) &&
		a.Webhook == b.Webhook &&
		stringSlicesEqual(a.Mentions, b.Mentions) &&
		a.SeverityThreshold == b.SeverityThreshold
}

func teleConfigsEqual(a, b TeleConfig) bool {
	return boolPtrEqual(a.Enabled, b.Enabled) &&
		a.ApiKey == b.ApiKey &&
		a.Channel == b.Channel &&
		stringSlicesEqual(a.Mentions, b.Mentions) &&
		a.SeverityThreshold == b.SeverityThreshold
}

func slackConfigsEqual(a, b SlackConfig) bool {
	return boolPtrEqual(a.Enabled, b.Enabled) &&
		a.Webhook == b.Webhook &&
		stringSlicesEqual(a.Mentions, b.Mentions) &&
		a.SeverityThreshold == b.SeverityThreshold
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestApplyAlertDefaults(t *testing.T) {
	tests := []struct {
		name     string
		dst      AlertConfig
		src      AlertConfig
		expected AlertConfig
	}{
		{
			name: "apply all defaults to empty config",
			dst:  AlertConfig{},
			src: AlertConfig{
				Stalled:                      intPtr(10),
				StalledAlerts:                boolPtr(true),
				ConsecutiveMissed:            intPtr(5),
				ConsecutivePriority:          "high",
				ConsecutiveAlerts:            boolPtr(true),
				Window:                       intPtr(20),
				PercentagePriority:           "medium",
				PercentageAlerts:             boolPtr(false),
				ConsecutiveEmpty:             intPtr(15),
				ConsecutiveEmptyPriority:     "low",
				ConsecutiveEmptyAlerts:       boolPtr(true),
				EmptyWindow:                  intPtr(30),
				EmptyPercentagePriority:      "critical",
				EmptyPercentageAlerts:        boolPtr(false),
				AlertIfInactive:              boolPtr(true),
				AlertIfNoServers:             boolPtr(true),
				GovernanceAlerts:             boolPtr(true),
				StakeChangeAlerts:            boolPtr(false),
				StakeChangeDropThreshold:     floatPtr(5.0),
				StakeChangeIncreaseThreshold: floatPtr(10.0),
				UnclaimedRewardsAlerts:       boolPtr(true),
				UnclaimedRewardsThreshold:    floatPtr(100.0),
			},
			expected: AlertConfig{
				Stalled:                      intPtr(10),
				StalledAlerts:                boolPtr(true),
				ConsecutiveMissed:            intPtr(5),
				ConsecutivePriority:          "high",
				ConsecutiveAlerts:            boolPtr(true),
				Window:                       intPtr(20),
				PercentagePriority:           "medium",
				PercentageAlerts:             boolPtr(false),
				ConsecutiveEmpty:             intPtr(15),
				ConsecutiveEmptyPriority:     "low",
				ConsecutiveEmptyAlerts:       boolPtr(true),
				EmptyWindow:                  intPtr(30),
				EmptyPercentagePriority:      "critical",
				EmptyPercentageAlerts:        boolPtr(false),
				AlertIfInactive:              boolPtr(true),
				AlertIfNoServers:             boolPtr(true),
				GovernanceAlerts:             boolPtr(true),
				StakeChangeAlerts:            boolPtr(false),
				StakeChangeDropThreshold:     floatPtr(5.0),
				StakeChangeIncreaseThreshold: floatPtr(10.0),
				UnclaimedRewardsAlerts:       boolPtr(true),
				UnclaimedRewardsThreshold:    floatPtr(100.0),
			},
		},
		{
			name: "preserve existing values, only fill zeros",
			dst: AlertConfig{
				Stalled:                  intPtr(25),
				StalledAlerts:            boolPtr(false),
				ConsecutiveMissed:        intPtr(8),
				ConsecutivePriority:      "critical",
				Window:                   intPtr(50),
				PercentagePriority:       "high",
				StakeChangeDropThreshold: floatPtr(15.0),
			},
			src: AlertConfig{
				Stalled:                  intPtr(10),
				StalledAlerts:            boolPtr(true),
				ConsecutiveMissed:        intPtr(5),
				ConsecutivePriority:      "medium",
				ConsecutiveAlerts:        boolPtr(true),
				Window:                   intPtr(20),
				PercentagePriority:       "low",
				PercentageAlerts:         boolPtr(false),
				ConsecutiveEmpty:         intPtr(15),
				ConsecutiveEmptyPriority: "low",
				ConsecutiveEmptyAlerts:   boolPtr(true),
				AlertIfInactive:          boolPtr(true),
				StakeChangeDropThreshold: floatPtr(5.0),
			},
			expected: AlertConfig{
				Stalled:                  intPtr(25),     // preserved
				StalledAlerts:            boolPtr(false), // preserved
				ConsecutiveMissed:        intPtr(8),      // preserved
				ConsecutivePriority:      "critical",     // preserved
				ConsecutiveAlerts:        boolPtr(true),  // filled from src
				Window:                   intPtr(50),     // preserved
				PercentagePriority:       "high",         // preserved
				PercentageAlerts:         boolPtr(false), // filled from src
				ConsecutiveEmpty:         intPtr(15),     // filled from src
				ConsecutiveEmptyPriority: "low",          // filled from src
				ConsecutiveEmptyAlerts:   boolPtr(true),  // filled from src
				AlertIfInactive:          boolPtr(true),  // filled from src
				StakeChangeDropThreshold: floatPtr(15.0), // preserved
			},
		},
		{
			name: "nested struct PagerDuty config",
			dst: AlertConfig{
				Pagerduty: PDConfig{},
			},
			src: AlertConfig{
				Pagerduty: PDConfig{
					Enabled:           boolPtr(true),
					ApiKey:            "test-key",
					DefaultSeverity:   "critical",
					SeverityThreshold: "warning",
				},
			},
			expected: AlertConfig{
				Pagerduty: PDConfig{
					Enabled:           boolPtr(true),
					ApiKey:            "test-key",
					DefaultSeverity:   "critical",
					SeverityThreshold: "warning",
				},
			},
		},
		{
			name: "nested struct partial override",
			dst: AlertConfig{
				Pagerduty: PDConfig{
					Enabled: boolPtr(false),
					ApiKey:  "existing-key",
				},
			},
			src: AlertConfig{
				Pagerduty: PDConfig{
					Enabled:           boolPtr(true),
					ApiKey:            "default-key",
					DefaultSeverity:   "warning",
					SeverityThreshold: "info",
				},
			},
			expected: AlertConfig{
				Pagerduty: PDConfig{
					Enabled:           boolPtr(false), // preserved
					ApiKey:            "existing-key", // preserved
					DefaultSeverity:   "warning",      // filled from src
					SeverityThreshold: "info",         // filled from src
				},
			},
		},
		{
			name: "pointer field handling - nil vs non-nil",
			dst: AlertConfig{
				Stalled:       nil,           // nil pointer should be filled
				StalledAlerts: boolPtr(true), // non-nil should be preserved
			},
			src: AlertConfig{
				Stalled:       intPtr(10),
				StalledAlerts: boolPtr(false),
			},
			expected: AlertConfig{
				Stalled:       intPtr(10),    // should be filled from src (was nil)
				StalledAlerts: boolPtr(true), // should be preserved (was non-nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of dst to avoid modifying the test case
			dst := tt.dst
			applyAlertDefaults(&dst, &tt.src)

			if !alertConfigsEqual(dst, tt.expected) {
				t.Errorf("applyAlertDefaults() mismatch")
				t.Logf("Stalled - Got: %v, Expected: %v", ptrIntToString(dst.Stalled), ptrIntToString(tt.expected.Stalled))
				t.Logf("StalledAlerts - Got: %v, Expected: %v", ptrBoolToString(dst.StalledAlerts), ptrBoolToString(tt.expected.StalledAlerts))
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"zero int", 0, true},
		{"non-zero int", 5, false},
		{"zero string", "", true},
		{"non-zero string", "hello", false},
		{"zero bool", false, true},
		{"non-zero bool", true, false},
		{"zero float64", 0.0, true},
		{"non-zero float64", 3.14, false},
		{"nil pointer", (*int)(nil), true},
		{"non-nil pointer", intPtr(42), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := reflect.ValueOf(tt.value)
			result := isZero(v)
			if result != tt.expected {
				t.Errorf("isZero(%v) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestApplyAlertDefaultsWithComplexNesting(t *testing.T) {
	// Test with multiple nested structs
	dst := AlertConfig{
		Pagerduty: PDConfig{
			Enabled: boolPtr(true),
		},
		Discord: DiscordConfig{
			Webhook: "existing-webhook",
		},
		Telegram: TeleConfig{},
	}

	src := AlertConfig{
		Pagerduty: PDConfig{
			Enabled:           boolPtr(false),
			ApiKey:            "default-key",
			DefaultSeverity:   "warning",
			SeverityThreshold: "info",
		},
		Discord: DiscordConfig{
			Enabled:           boolPtr(true),
			Webhook:           "default-webhook",
			Mentions:          []string{"@here"},
			SeverityThreshold: "critical",
		},
		Telegram: TeleConfig{
			Enabled:           boolPtr(true),
			ApiKey:            "telegram-key",
			Channel:           "alerts",
			Mentions:          []string{"@admin"},
			SeverityThreshold: "warning",
		},
	}

	expected := AlertConfig{
		Pagerduty: PDConfig{
			Enabled:           boolPtr(true), // preserved from dst
			ApiKey:            "default-key", // filled from src
			DefaultSeverity:   "warning",     // filled from src
			SeverityThreshold: "info",        // filled from src
		},
		Discord: DiscordConfig{
			Enabled:           boolPtr(true),      // filled from src
			Webhook:           "existing-webhook", // preserved from dst
			Mentions:          []string{"@here"},  // filled from src
			SeverityThreshold: "critical",         // filled from src
		},
		Telegram: TeleConfig{
			Enabled:           boolPtr(true),      // filled from src
			ApiKey:            "telegram-key",     // filled from src
			Channel:           "alerts",           // filled from src
			Mentions:          []string{"@admin"}, // filled from src
			SeverityThreshold: "warning",          // filled from src
		},
	}

	applyAlertDefaults(&dst, &src)

	if !alertConfigsEqual(dst, expected) {
		t.Errorf("Complex nesting test failed\nGot:      %+v\nExpected: %+v", dst, expected)
	}
}

func TestApplyAlertDefaultsWithPointerFields(t *testing.T) {
	// Test pointer field handling specifically
	dst := AlertConfig{
		Stalled:           nil,            // nil pointer should be filled
		StalledAlerts:     boolPtr(false), // non-nil pointer should be preserved
		ConsecutiveMissed: intPtr(0),      // non-nil pointer should be preserved even if zero
	}

	src := AlertConfig{
		Stalled:           intPtr(30),
		StalledAlerts:     boolPtr(true),
		ConsecutiveMissed: intPtr(10),
	}

	expected := AlertConfig{
		Stalled:           intPtr(30),     // filled from src (was nil)
		StalledAlerts:     boolPtr(false), // preserved from dst (non-nil)
		ConsecutiveMissed: intPtr(0),      // preserved from dst (non-nil, even though zero)
	}

	applyAlertDefaults(&dst, &src)

	if !alertConfigsEqual(dst, expected) {
		t.Errorf("Pointer fields test failed\nGot:      %+v\nExpected: %+v", dst, expected)
	}
}

// Helper functions for creating pointers
func intPtr(i int) *int {
	return &i
}

// Debug helper functions
func ptrIntToString(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *p)
}

func ptrBoolToString(p *bool) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%t", *p)
}

func boolPtr(b bool) *bool {
	return &b
}

func floatPtr(f float64) *float64 {
	return &f
}

// Test utility functions
func TestBoolVal(t *testing.T) {
	tests := []struct {
		name     string
		input    *bool
		expected bool
	}{
		{"nil pointer", nil, false},
		{"false pointer", boolPtr(false), false},
		{"true pointer", boolPtr(true), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := boolVal(tt.input)
			if result != tt.expected {
				t.Errorf("boolVal(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIntVal(t *testing.T) {
	tests := []struct {
		name     string
		input    *int
		expected int
	}{
		{"nil pointer", nil, 0},
		{"zero pointer", intPtr(0), 0},
		{"positive pointer", intPtr(42), 42},
		{"negative pointer", intPtr(-5), -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := intVal(tt.input)
			if result != tt.expected {
				t.Errorf("intVal(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFloatVal(t *testing.T) {
	tests := []struct {
		name     string
		input    *float64
		expected float64
	}{
		{"nil pointer", nil, 0.0},
		{"zero pointer", floatPtr(0.0), 0.0},
		{"positive pointer", floatPtr(3.14), 3.14},
		{"negative pointer", floatPtr(-2.5), -2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := floatVal(tt.input)
			if result != tt.expected {
				t.Errorf("floatVal(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
