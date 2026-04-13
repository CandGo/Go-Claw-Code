package config

import (
	"os"
	"testing"
)

func TestFeatureFlags_RegisterAndIsEnabled(t *testing.T) {
	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "test_flag", DefaultValue: true})
	ff.Register(FeatureFlag{Name: "off_flag", DefaultValue: false})

	if !ff.IsEnabled("test_flag") {
		t.Error("test_flag should be enabled by default")
	}
	if ff.IsEnabled("off_flag") {
		t.Error("off_flag should be disabled by default")
	}
	if ff.IsEnabled("nonexistent") {
		t.Error("nonexistent flag should be disabled")
	}
}

func TestFeatureFlags_SetEnabled(t *testing.T) {
	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "test_flag", DefaultValue: false})

	err := ff.SetEnabled("test_flag", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ff.IsEnabled("test_flag") {
		t.Error("test_flag should be enabled after SetEnabled")
	}

	err = ff.SetEnabled("nonexistent", true)
	if err == nil {
		t.Error("should error on unknown flag")
	}
}

func TestFeatureFlags_LoadFromConfig(t *testing.T) {
	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "screenshot", DefaultValue: true})
	ff.Register(FeatureFlag{Name: "computer_use", DefaultValue: false})
	ff.Register(FeatureFlag{Name: "voice", DefaultValue: false})

	cfg := &Config{
		Settings: map[string]interface{}{
			"computer_use": true,
		},
	}
	ff.LoadFromConfig(cfg)

	if !ff.IsEnabled("screenshot") {
		t.Error("screenshot should remain true (default)")
	}
	if !ff.IsEnabled("computer_use") {
		t.Error("computer_use should be enabled from config")
	}
	if ff.IsEnabled("voice") {
		t.Error("voice should remain disabled (default)")
	}
}

func TestFeatureFlags_EnvOverride(t *testing.T) {
	os.Setenv("CLAW_FEATURE_TEST_VOICE", "true")
	defer os.Unsetenv("CLAW_FEATURE_TEST_VOICE")

	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "voice", DefaultValue: false, EnvOverride: "CLAW_FEATURE_TEST_VOICE"})

	cfg := &Config{Settings: map[string]interface{}{}}
	ff.LoadFromConfig(cfg)

	if !ff.IsEnabled("voice") {
		t.Error("voice should be enabled via env override")
	}
}

func TestFeatureFlags_EnvOverridesConfig(t *testing.T) {
	os.Setenv("CLAW_FEATURE_SCREENSHOT_TEST", "false")
	defer os.Unsetenv("CLAW_FEATURE_SCREENSHOT_TEST")

	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "screenshot", DefaultValue: true, EnvOverride: "CLAW_FEATURE_SCREENSHOT_TEST"})

	cfg := &Config{
		Settings: map[string]interface{}{
			"screenshot": true,
		},
	}
	ff.LoadFromConfig(cfg)

	if ff.IsEnabled("screenshot") {
		t.Error("env should override config to disable screenshot")
	}
}

func TestFeatureFlags_All(t *testing.T) {
	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "beta", DefaultValue: false})
	ff.Register(FeatureFlag{Name: "alpha", DefaultValue: true})

	states := ff.All()
	if len(states) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(states))
	}
	// Should be sorted by name
	if states[0].Name != "alpha" {
		t.Errorf("first flag should be alpha, got %s", states[0].Name)
	}
	if !states[0].Enabled {
		t.Error("alpha should be enabled")
	}
}

func TestFeatureFlags_FormatTable(t *testing.T) {
	ff := NewFeatureFlags()
	ff.Register(FeatureFlag{Name: "screenshot", DefaultValue: true})
	ff.Register(FeatureFlag{Name: "voice", DefaultValue: false})

	table := ff.FormatTable()
	if table == "" {
		t.Error("FormatTable should return non-empty string")
	}
	if !containsStr(table, "screenshot") || !containsStr(table, "voice") {
		t.Error("FormatTable should contain flag names")
	}
}
