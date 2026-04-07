package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// FoundryToken holds a resolved Azure AD access token.
type FoundryToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

// foundryProvider resolves Azure credentials for Foundry endpoints.
type foundryProvider struct {
	mu        sync.Mutex
	cached    *FoundryToken
	fetchedAt time.Time
}

var (
	foundrySingleton     *foundryProvider
	foundrySingletonOnce sync.Once
)

func foundryProviderInstance() *foundryProvider {
	foundrySingletonOnce.Do(func() {
		foundrySingleton = &foundryProvider{}
	})
	return foundrySingleton
}

const foundryTokenTTL = 60 * time.Minute
const foundryCognitiveResource = "https://cognitiveservices.azure.com/.default"

// A foundryResolver produces a FoundryToken or returns an error.
type foundryResolver func(ctx context.Context) (*FoundryToken, error)

// accessToken returns a cached token if still valid, otherwise resolves a new one.
func (fp *foundryProvider) accessToken(ctx context.Context) (*FoundryToken, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	if fp.cached != nil && time.Now().Before(fp.cached.ExpiresAt) {
		return fp.cached, nil
	}

	resolvers := []foundryResolver{
		fp.fromAPIKey,
		fp.fromServicePrincipal,
		fp.fromAzureCLI,
		fp.fromManagedIdentity,
	}

	for _, resolve := range resolvers {
		tok, err := resolve(ctx)
		if err == nil && tok != nil {
			fp.cached = tok
			fp.fetchedAt = time.Now()
			return tok, nil
		}
	}
	return nil, fmt.Errorf("no Azure credentials found")
}

// fromAPIKey checks for ANTHROPIC_FOUNDRY_API_KEY (static key, no token needed).
func (fp *foundryProvider) fromAPIKey(_ context.Context) (*FoundryToken, error) {
	if os.Getenv("ANTHROPIC_FOUNDRY_API_KEY") != "" {
		return &FoundryToken{ExpiresAt: time.Now().Add(365 * 24 * time.Hour)}, nil
	}
	return nil, fmt.Errorf("no API key")
}

// fromServicePrincipal uses AZURE_TENANT_ID + AZURE_CLIENT_ID + AZURE_CLIENT_SECRET.
func (fp *foundryProvider) fromServicePrincipal(ctx context.Context) (*FoundryToken, error) {
	tenant := os.Getenv("AZURE_TENANT_ID")
	cid := os.Getenv("AZURE_CLIENT_ID")
	secret := os.Getenv("AZURE_CLIENT_SECRET")
	if tenant == "" || cid == "" || secret == "" {
		return nil, fmt.Errorf("service principal env vars incomplete")
	}

	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)
	form := url.Values{}
	form.Set("client_id", cid)
	form.Set("client_secret", secret)
	form.Set("scope", foundryCognitiveResource)
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SP token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SP token returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	return &FoundryToken{
		AccessToken: parsed.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

// fromAzureCLI runs `az account get-access-token`.
func (fp *foundryProvider) fromAzureCLI(ctx context.Context) (*FoundryToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "az", "account", "get-access-token",
		"--resource", "https://cognitiveservices.azure.com", "--output", "json").Output()
	if err != nil {
		return nil, err
	}

	var parsed struct {
		AccessToken string `json:"accessToken"`
		ExpiresOn   string `json:"expiresOn"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}

	expiry, _ := time.Parse(time.RFC3339, parsed.ExpiresOn)
	if expiry.IsZero() {
		expiry = time.Now().Add(foundryTokenTTL)
	}

	return &FoundryToken{
		AccessToken: parsed.AccessToken,
		ExpiresAt:   expiry,
	}, nil
}

// fromManagedIdentity queries the Azure Instance Metadata Service (IMDS).
func (fp *foundryProvider) fromManagedIdentity(ctx context.Context) (*FoundryToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	imdsURL := "http://169.254.169.254/metadata/identity/oauth2/token"
	params := url.Values{}
	params.Set("api-version", "2018-02-01")
	params.Set("resource", "https://cognitiveservices.azure.com")

	req, err := http.NewRequestWithContext(ctx, "GET", imdsURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IMDS unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IMDS returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	dur, _ := time.ParseDuration(parsed.ExpiresIn + "s")
	if dur == 0 {
		dur = foundryTokenTTL
	}

	return &FoundryToken{
		AccessToken: parsed.AccessToken,
		ExpiresAt:   time.Now().Add(dur),
	}, nil
}

// resolveFoundryAuth builds an AuthSource for Azure Foundry.
func resolveFoundryAuth(ctx context.Context) (*AuthSource, error) {
	fp := foundryProviderInstance()
	tok, err := fp.accessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("foundry: %w", err)
	}
	auth := &AuthSource{
		Method: AuthAzureToken,
	}
	// If an API key was configured, use it directly as the key header.
	if key := os.Getenv("ANTHROPIC_FOUNDRY_API_KEY"); key != "" {
		auth.APIKey = key
	} else {
		auth.AzureToken = tok.AccessToken
	}
	return auth, nil
}

// InvalidateFoundryCache clears cached Foundry credentials.
func InvalidateFoundryCache() {
	fp := foundryProviderInstance()
	fp.mu.Lock()
	fp.cached = nil
	fp.fetchedAt = time.Time{}
	fp.mu.Unlock()
}
