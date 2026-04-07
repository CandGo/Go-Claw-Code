package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Credentials holds persisted API key credentials saved to ~/.go-claw/auth.json.
type Credentials struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// credentialsDir returns the directory for credential files (~/.go-claw/).
func credentialsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-claw"), nil
}

// CredentialsFilePath returns the absolute path to the credentials file.
func CredentialsFilePath() (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

// SaveCredentials writes credentials to ~/.go-claw/auth.json.
// Creates the directory if needed. File permissions are restricted to owner-only (0600).
func SaveCredentials(creds *Credentials) error {
	dir, err := credentialsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "auth.json")
	return os.WriteFile(path, data, 0600)
}

// LoadCredentials reads credentials from ~/.go-claw/auth.json.
// Returns (nil, nil) if the file does not exist — the caller should treat
// this as "no saved credentials" rather than an error.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// HasCredentialsFile returns true if the credentials file exists and is readable.
func HasCredentialsFile() bool {
	creds, err := LoadCredentials()
	return err == nil && creds != nil
}

// IsUsingClaudeCode returns true if the user chose to reuse Claude Code config.
func IsUsingClaudeCode() bool {
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		return false
	}
	return creds.APIKey == "(claude-code)"
}
