package tools

import (
	"encoding/json"
	"testing"
)

func TestValidateConfigSetting(t *testing.T) {
	// Valid settings
	validSettings := []string{
		"theme", "editorMode", "verbose", "preferredNotifChannel",
		"autoCompactEnabled", "autoMemoryEnabled", "autoDreamEnabled",
		"fileCheckpointingEnabled", "showTurnDuration",
		"terminalProgressBarEnabled", "todoFeatureEnabled",
		"model", "alwaysThinkingEnabled", "permissions.defaultMode",
		"language", "teammateMode",
	}
	for _, s := range validSettings {
		spec, err := validateConfigSetting(s)
		if err != nil {
			t.Errorf("validateConfigSetting(%q) returned error: %v", s, err)
		}
		if spec == nil {
			t.Errorf("validateConfigSetting(%q) returned nil spec", s)
		}
	}

	// Invalid setting
	_, err := validateConfigSetting("nonexistent_setting")
	if err == nil {
		t.Error("validateConfigSetting(invalid) should return error")
	}
}

func TestNormalizeConfigValueBoolean(t *testing.T) {
	spec := &ConfigSettingSpec{Kind: "boolean"}

	val, err := normalizeConfigValue(spec, "true")
	if err != nil || val != true {
		t.Errorf("normalizeConfigValue(true) = %v, %v", val, err)
	}

	val, err = normalizeConfigValue(spec, "false")
	if err != nil || val != false {
		t.Errorf("normalizeConfigValue(false) = %v, %v", val, err)
	}

	_, err = normalizeConfigValue(spec, "invalid")
	if err == nil {
		t.Error("normalizeConfigValue(boolean, invalid) should error")
	}
}

func TestNormalizeConfigValueString(t *testing.T) {
	spec := &ConfigSettingSpec{Kind: "string"}
	val, err := normalizeConfigValue(spec, "dark")
	if err != nil || val != "dark" {
		t.Errorf("normalizeConfigValue(string, dark) = %v, %v", val, err)
	}
}

func TestNormalizeConfigValueConstrained(t *testing.T) {
	spec := &ConfigSettingSpec{
		Kind:    "string",
		Options: []string{"default", "vim", "emacs"},
	}

	val, err := normalizeConfigValue(spec, "vim")
	if err != nil || val != "vim" {
		t.Errorf("normalizeConfigValue(vim) = %v, %v", val, err)
	}

	_, err = normalizeConfigValue(spec, "invalid_option")
	if err == nil {
		t.Error("normalizeConfigValue(invalid option) should error")
	}
}

func TestParseTypedInput(t *testing.T) {
	input := map[string]interface{}{
		"command": "echo hello",
		"timeout": float64(5000),
	}
	var parsed BashInput
	err := parseTypedInput(input, &parsed)
	if err != nil {
		t.Fatalf("parseTypedInput failed: %v", err)
	}
	if parsed.Command != "echo hello" {
		t.Errorf("Command = %q", parsed.Command)
	}
	if parsed.Timeout != 5000 {
		t.Errorf("Timeout = %d", parsed.Timeout)
	}
}

func TestFormatTypedOutput(t *testing.T) {
	output := BashOutput{
		Stdout:   "hello\n",
		ExitCode: 0,
		Command:  "echo hello",
	}
	result, err := formatTypedOutput(output)
	if err != nil {
		t.Fatalf("formatTypedOutput failed: %v", err)
	}
	if !contains(result, "hello") {
		t.Errorf("formatTypedOutput result missing content: %s", result)
	}
	// Should be valid JSON
	var parsed BashOutput
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("result is not valid JSON: %v", err)
	}
}

func TestConfigFilePath(t *testing.T) {
	global := configFilePath("global")
	if global == "" {
		t.Error("configFilePath(global) returned empty")
	}
	settings := configFilePath("settings")
	if settings == "" {
		t.Error("configFilePath(settings) returned empty")
	}
}

func TestBashInputStruct(t *testing.T) {
	input := BashInput{
		Command:         "ls -la",
		Timeout:         10000,
		Description:     "List files",
		RunInBackground: true,
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal BashInput failed: %v", err)
	}
	var parsed BashInput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal BashInput failed: %v", err)
	}
	if parsed.Command != "ls -la" {
		t.Errorf("Command = %q", parsed.Command)
	}
	if parsed.Timeout != 10000 {
		t.Errorf("Timeout = %d", parsed.Timeout)
	}
	if !parsed.RunInBackground {
		t.Error("RunInBackground should be true")
	}
}

func TestReadFileInputStruct(t *testing.T) {
	input := ReadFileInput{
		Path:   "/tmp/test.go",
		Offset: 10,
		Limit:  50,
	}
	data, _ := json.Marshal(input)
	var parsed ReadFileInput
	json.Unmarshal(data, &parsed)
	if parsed.Path != "/tmp/test.go" {
		t.Errorf("Path = %q", parsed.Path)
	}
	if parsed.Offset != 10 {
		t.Errorf("Offset = %d", parsed.Offset)
	}
}

func TestEditFileInputStruct(t *testing.T) {
	input := EditFileInput{
		Path:       "main.go",
		OldString:  "old",
		NewString:  "new",
		ReplaceAll: true,
	}
	data, _ := json.Marshal(input)
	var parsed EditFileInput
	json.Unmarshal(data, &parsed)
	if !parsed.ReplaceAll {
		t.Error("ReplaceAll should be true")
	}
	if parsed.OldString != "old" {
		t.Errorf("OldString = %q", parsed.OldString)
	}
}

func TestGrepInputStruct(t *testing.T) {
	input := GrepInput{
		Pattern:         "TODO",
		Path:            ".",
		OutputMode:      "count",
		CaseInsensitive: true,
		HeadLimit:       100,
	}
	data, _ := json.Marshal(input)
	var parsed GrepInput
	json.Unmarshal(data, &parsed)
	if parsed.CaseInsensitive != true {
		t.Error("CaseInsensitive should be true")
	}
	if parsed.OutputMode != "count" {
		t.Errorf("OutputMode = %q", parsed.OutputMode)
	}
}

func TestAgentInputStruct(t *testing.T) {
	input := AgentInput{
		Description:  "Search codebase",
		Prompt:       "Find all TODO comments",
		SubagentType: "Explore",
	}
	data, _ := json.Marshal(input)
	var parsed AgentInput
	json.Unmarshal(data, &parsed)
	if parsed.SubagentType != "Explore" {
		t.Errorf("SubagentType = %q", parsed.SubagentType)
	}
}
