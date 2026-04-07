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
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VertexToken holds a resolved Google Cloud access token.
type VertexToken struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

// vertexProvider resolves Google Cloud credentials for Vertex AI.
type vertexProvider struct {
	mu        sync.Mutex
	cached    *VertexToken
	fetchedAt time.Time
}

var (
	vertexSingleton     *vertexProvider
	vertexSingletonOnce sync.Once
)

func vertexProviderInstance() *vertexProvider {
	vertexSingletonOnce.Do(func() {
		vertexSingleton = &vertexProvider{}
	})
	return vertexSingleton
}

const vertexTokenTTL = 60 * time.Minute
const vertexProbeTimeout = 5 * time.Second

// A vertexResolver produces a VertexToken or returns an error.
type vertexResolver func(ctx context.Context) (*VertexToken, error)

// token returns a cached token if still valid, otherwise resolves a new one.
func (vp *vertexProvider) token(ctx context.Context) (*VertexToken, error) {
	vp.mu.Lock()
	defer vp.mu.Unlock()

	if vp.cached != nil && time.Now().Before(vp.cached.ExpiresAt) {
		return vp.cached, nil
	}

	resolvers := []vertexResolver{
		vp.fromServiceAccountFile,
		vp.fromGcloudCLI,
		vp.fromADCFile,
		vp.fromMetadataServer,
	}

	for _, resolve := range resolvers {
		tok, err := resolve(ctx)
		if err == nil && tok != nil {
			vp.cached = tok
			vp.fetchedAt = time.Now()
			return tok, nil
		}
	}
	return nil, fmt.Errorf("no Google Cloud credentials found")
}

// fromServiceAccountFile loads a service account key from GOOGLE_APPLICATION_CREDENTIALS.
func (vp *vertexProvider) fromServiceAccountFile(_ context.Context) (*VertexToken, error) {
	path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if path == "" {
		return nil, fmt.Errorf("GOOGLE_APPLICATION_CREDENTIALS not set")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sa struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, err
	}
	if sa.Type != "service_account" {
		return nil, fmt.Errorf("not a service account key (type=%s)", sa.Type)
	}

	// Service account JWT signing requires golang.org/x/oauth2 or similar.
	// Direct users to gcloud CLI or ADC for now.
	return nil, fmt.Errorf("service account key detected; use 'gcloud auth application-default login' instead")
}

// fromGcloudCLI runs gcloud auth print-access-token.
func (vp *vertexProvider) fromGcloudCLI(ctx context.Context) (*VertexToken, error) {
	ctx, cancel := context.WithTimeout(ctx, vertexProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return nil, err
	}
	tokStr := strings.TrimSpace(string(out))
	if tokStr == "" {
		return nil, fmt.Errorf("gcloud returned empty token")
	}
	return &VertexToken{
		AccessToken: tokStr,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(vertexTokenTTL),
	}, nil
}

// fromADCFile reads Application Default Credentials from ~/.config/gcloud/.
func (vp *vertexProvider) fromADCFile(ctx context.Context) (*VertexToken, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	adcPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	data, err := os.ReadFile(adcPath)
	if err != nil {
		return nil, err
	}

	var adc struct {
		Type         string `json:"type"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &adc); err != nil {
		return nil, err
	}
	if adc.Type != "authorized_user" || adc.RefreshToken == "" {
		return nil, fmt.Errorf("ADC file is not an authorized_user credential")
	}

	// Exchange refresh token for access token
	form := url.Values{}
	form.Set("client_id", adc.ClientID)
	form.Set("client_secret", adc.ClientSecret)
	form.Set("refresh_token", adc.RefreshToken)
	form.Set("grant_type", "refresh_token")

	resp, err := http.Post("https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ADC refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ADC refresh returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	return &VertexToken{
		AccessToken: parsed.AccessToken,
		TokenType:   parsed.TokenType,
		ExpiresAt:   time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

// fromMetadataServer probes the GCP metadata server (works inside Compute/Cloud Run/GKE).
func (vp *vertexProvider) fromMetadataServer(ctx context.Context) (*VertexToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Quick connectivity check
	probeReq, _ := http.NewRequestWithContext(ctx, "GET",
		"http://metadata.google.internal/computeMetadata/v1/", nil)
	probeReq.Header.Set("Metadata-Flavor", "Google")
	probeResp, err := http.DefaultClient.Do(probeReq)
	if err != nil || probeResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("not running on GCP")
	}
	probeResp.Body.Close()

	// Fetch token
	tokenURL := "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	req, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	return &VertexToken{
		AccessToken: parsed.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

// resolveVertexAuth builds an AuthSource for Google Vertex AI.
func resolveVertexAuth(ctx context.Context) (*AuthSource, error) {
	vp := vertexProviderInstance()
	tok, err := vp.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("vertex: %w", err)
	}
	return &AuthSource{
		Method:      AuthGoogleADC,
		GoogleToken: tok.AccessToken,
	}, nil
}

// InvalidateVertexCache clears cached Vertex credentials.
func InvalidateVertexCache() {
	vp := vertexProviderInstance()
	vp.mu.Lock()
	vp.cached = nil
	vp.fetchedAt = time.Time{}
	vp.mu.Unlock()
}
