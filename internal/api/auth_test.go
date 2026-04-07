package api

import (
	"os"
	"testing"
)

func TestEnvIsTruthy(t *testing.T) {
	tests := []struct{ input string; want bool }{
		{"true", true}, {"True", true}, {"TRUE", true},
		{"1", true}, {"yes", true}, {"YES", true},
		{"false", false}, {"0", false}, {"no", false},
		{"", false}, {"maybe", false},
	}
	for _, tt := range tests {
		if got := envIsTruthy(tt.input); got != tt.want {
			t.Errorf("envIsTruthy(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDetectProviderFromEnv(t *testing.T) {
	tests := []struct {
		key   string
		val   string
		want  AuthMethod
	}{
		{"CLAUDE_CODE_USE_BEDROCK", "true", AuthAWSSigV4},
		{"CLAUDE_CODE_USE_VERTEX", "1", AuthGoogleADC},
		{"CLAUDE_CODE_USE_FOUNDRY", "yes", AuthAzureToken},
	}
	for _, tt := range tests {
		// Clear all
		for _, k := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
			os.Unsetenv(k)
		}
		t.Setenv(tt.key, tt.val)
		if got := DetectProviderFromEnv(); got != tt.want {
			t.Errorf("DetectProviderFromEnv() with %s=%s = %v, want %v", tt.key, tt.val, got, tt.want)
		}
	}
	// Clean state returns APIKey
	for _, k := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		os.Unsetenv(k)
	}
	if got := DetectProviderFromEnv(); got != AuthAPIKey {
		t.Errorf("DetectProviderFromEnv() with no env = %v, want AuthAPIKey", got)
	}
}

func TestAWSRegion(t *testing.T) {
	os.Unsetenv("AWS_REGION")
	os.Unsetenv("AWS_DEFAULT_REGION")
	t.Setenv("AWS_REGION", "ap-northeast-1")
	if got := awsRegion(); got != "ap-northeast-1" {
		t.Errorf("awsRegion() = %q, want %q", got, "ap-northeast-1")
	}
	os.Unsetenv("AWS_REGION")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")
	if got := awsRegion(); got != "eu-west-1" {
		t.Errorf("awsRegion() = %q, want %q", got, "eu-west-1")
	}
	os.Unsetenv("AWS_DEFAULT_REGION")
	if got := awsRegion(); got != "us-east-1" {
		t.Errorf("awsRegion() default = %q, want %q", got, "us-east-1")
	}
}

func TestVertexRegion(t *testing.T) {
	os.Unsetenv("VERTEX_REGION_CLAUDE_3_5_HAIKU")
	os.Unsetenv("VERTEX_REGION_CLAUDE_HAIKU_4_5")
	os.Unsetenv("VERTEX_REGION_CLAUDE_3_5_SONNET")
	os.Unsetenv("VERTEX_REGION_CLAUDE_3_7_SONNET")
	os.Unsetenv("CLOUD_ML_REGION")

	t.Setenv("CLOUD_ML_REGION", "europe-west4")
	if got := vertexRegion("claude-3-opus"); got != "europe-west4" {
		t.Errorf("vertexRegion(opus) = %q, want %q", got, "europe-west4")
	}
	os.Unsetenv("CLOUD_ML_REGION")

	t.Setenv("VERTEX_REGION_CLAUDE_3_5_HAIKU", "asia-east1")
	if got := vertexRegion("claude-3-5-haiku"); got != "asia-east1" {
		t.Errorf("vertexRegion(haiku) = %q, want %q", got, "asia-east1")
	}
	os.Unsetenv("VERTEX_REGION_CLAUDE_3_5_HAIKU")

	if got := vertexRegion("unknown-model"); got != "us-east5" {
		t.Errorf("vertexRegion(default) = %q, want %q", got, "us-east5")
	}
}

func TestBaseURLForProvider(t *testing.T) {
	os.Unsetenv("ANTHROPIC_FOUNDRY_BASE_URL")
	os.Unsetenv("ANTHROPIC_FOUNDRY_RESOURCE")

	bedrockURL := BaseURLForProvider(AuthAWSSigV4, "claude-3")
	if !containsStr(bedrockURL, "bedrock-runtime") {
		t.Errorf("Bedrock URL should contain 'bedrock-runtime', got %q", bedrockURL)
	}

	vertexURL := BaseURLForProvider(AuthGoogleADC, "claude-3")
	if !containsStr(vertexURL, "aiplatform.googleapis.com") {
		t.Errorf("Vertex URL should contain 'aiplatform', got %q", vertexURL)
	}

	defaultURL := BaseURLForProvider(AuthAPIKey, "")
	// When no provider is detected, it returns BaseURL() which may be overridden by env
	if defaultURL == "" {
		t.Error("Default URL should not be empty")
	}
}

func TestHasCredentials(t *testing.T) {
	if (&AuthSource{}).HasCredentials() {
		t.Error("empty AuthSource should have no credentials")
	}
	if !(&AuthSource{APIKey: "key"}).HasCredentials() {
		t.Error("AuthSource with APIKey should have credentials")
	}
	if !(&AuthSource{Method: AuthAWSSigV4}).HasCredentials() {
		t.Error("AuthSource with SigV4 method should have credentials")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
