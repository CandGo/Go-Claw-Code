package api

import (
	"testing"
)

func TestParseAWSINI_DefaultProfile(t *testing.T) {
	ini := `
[default]
aws_access_key_id = AKID123
aws_secret_access_key = SECRET456
aws_session_token = TOK789

[production]
aws_access_key_id = PROD_AKID
aws_secret_access_key = PROD_SECRET
`
	creds, err := parseAWSINI(ini, "default")
	if err != nil {
		t.Fatalf("parseAWSINI: %v", err)
	}
	if creds.AccessKeyID != "AKID123" {
		t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, "AKID123")
	}
	if creds.SecretAccessKey != "SECRET456" {
		t.Errorf("SecretAccessKey = %q, want %q", creds.SecretAccessKey, "SECRET456")
	}
	if creds.SessionToken != "TOK789" {
		t.Errorf("SessionToken = %q, want %q", creds.SessionToken, "TOK789")
	}
}

func TestParseAWSINI_NamedProfile(t *testing.T) {
	ini := `[profile staging]
aws_access_key_id = STAGE_KEY
aws_secret_access_key = STAGE_SECRET
`
	creds, err := parseAWSINI(ini, "staging")
	if err != nil {
		t.Fatalf("parseAWSINI: %v", err)
	}
	if creds.AccessKeyID != "STAGE_KEY" {
		t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, "STAGE_KEY")
	}
}

func TestParseAWSINI_MissingProfile(t *testing.T) {
	ini := `[default]
aws_access_key_id = KEY
aws_secret_access_key = SECRET
`
	_, err := parseAWSINI(ini, "nonexistent")
	if err == nil {
		t.Error("expected error for missing profile")
	}
}

func TestParseAWSINI_CommentsAndBlanks(t *testing.T) {
	ini := `# comment line
; another comment

[default]
aws_access_key_id = KEY

aws_secret_access_key = SECRET
`
	creds, err := parseAWSINI(ini, "default")
	if err != nil {
		t.Fatalf("parseAWSINI: %v", err)
	}
	if creds.AccessKeyID != "KEY" {
		t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, "KEY")
	}
}

func TestInvalidateBedrockCache(t *testing.T) {
	// Just ensure it doesn't panic
	InvalidateBedrockCache()
}
