package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// FeatureFlag defines a toggleable feature.
type FeatureFlag struct {
	Name         string // e.g. "screenshot", "computer_use", "voice"
	Description  string
	DefaultValue bool
	EnvOverride  string // e.g. "CLAW_FEATURE_SCREENSHOT"
}

// FlagState is a flag with its current enabled state.
type FlagState struct {
	Name    string
	Enabled bool
	Default bool
	Source  string // "default", "config", "env", "runtime"
}

// FeatureFlags holds the runtime state of all feature flags.
type FeatureFlags struct {
	flags  map[string]*FeatureFlag
	states map[string]bool
	source map[string]string // where each flag's value came from
	mu     sync.RWMutex
}

// GlobalFlags is the singleton instance, set during initialization.
var GlobalFlags *FeatureFlags

// NewFeatureFlags creates a new feature flag registry.
func NewFeatureFlags() *FeatureFlags {
	return &FeatureFlags{
		flags:  make(map[string]*FeatureFlag),
		states: make(map[string]bool),
		source: make(map[string]string),
	}
}

// Register adds a feature flag definition.
func (ff *FeatureFlags) Register(flag FeatureFlag) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	ff.flags[flag.Name] = &flag
	if _, exists := ff.states[flag.Name]; !exists {
		ff.states[flag.Name] = flag.DefaultValue
		ff.source[flag.Name] = "default"
	}
}

// IsEnabled checks if a feature flag is enabled.
func (ff *FeatureFlags) IsEnabled(name string) bool {
	ff.mu.RLock()
	defer ff.mu.RUnlock()
	enabled, ok := ff.states[name]
	if !ok {
		return false
	}
	return enabled
}

// SetEnabled toggles a feature flag at runtime.
func (ff *FeatureFlags) SetEnabled(name string, enabled bool) error {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if _, ok := ff.flags[name]; !ok {
		return fmt.Errorf("unknown feature flag: %s", name)
	}
	ff.states[name] = enabled
	ff.source[name] = "runtime"
	return nil
}

// All returns all flags with their current states, sorted by name.
func (ff *FeatureFlags) All() []FlagState {
	ff.mu.RLock()
	defer ff.mu.RUnlock()

	var result []FlagState
	for name, flag := range ff.flags {
		result = append(result, FlagState{
			Name:    name,
			Enabled: ff.states[name],
			Default: flag.DefaultValue,
			Source:  ff.source[name],
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// LoadFromConfig reads flag states from the config and environment variables.
// Environment variables take highest precedence.
func (ff *FeatureFlags) LoadFromConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	ff.mu.Lock()
	defer ff.mu.Unlock()

	// Load from config settings
	if cfg.Settings != nil {
		for name := range ff.flags {
			if val, ok := cfg.Settings[name]; ok {
				if b, ok := val.(bool); ok {
					ff.states[name] = b
					ff.source[name] = "config"
				}
			}
		}
	}

	// Environment variables override config
	for name, flag := range ff.flags {
		if flag.EnvOverride == "" {
			continue
		}
		if envVal := os.Getenv(flag.EnvOverride); envVal != "" {
			switch strings.ToLower(envVal) {
			case "true", "1", "yes":
				ff.states[name] = true
				ff.source[name] = "env"
			case "false", "0", "no":
				ff.states[name] = false
				ff.source[name] = "env"
			}
		}
	}
}

// FormatTable returns a human-readable table of all flags.
func (ff *FeatureFlags) FormatTable() string {
	states := ff.All()
	var buf strings.Builder
	buf.WriteString("Feature flags:\n")
	for _, s := range states {
		status := "disabled"
		if s.Enabled {
			status = "enabled"
		}
		defaultMarker := ""
		if s.Enabled != s.Default {
			defaultMarker = fmt.Sprintf(" (default: %v)", s.Default)
		}
		buf.WriteString(fmt.Sprintf("  %-15s %-9s [%s]%s\n", s.Name, status, s.Source, defaultMarker))
	}
	return buf.String()
}
