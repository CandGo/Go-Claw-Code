package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// BedrockCreds holds AWS session credentials for Bedrock API calls.
type BedrockCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ExpiresAt       time.Time
}

// bedrockProvider resolves AWS credentials for Bedrock from multiple sources.
type bedrockProvider struct {
	mu       sync.Mutex
	cached   *BedrockCreds
	refreshed time.Time
}

var (
	bedrockSingleton     *bedrockProvider
	bedrockSingletonOnce sync.Once
)

// bedrockProviderInstance returns the shared Bedrock credential provider.
func bedrockProviderInstance() *bedrockProvider {
	bedrockSingletonOnce.Do(func() {
		bedrockSingleton = &bedrockProvider{}
	})
	return bedrockSingleton
}

// credentialTTL is the default time-to-live before forced re-resolution.
const credentialTTL = 60 * time.Minute

// A credSource attempts to produce BedrockCreds. Returns nil if this source has nothing to offer.
type credSource func(ctx context.Context) (*BedrockCreds, error)

// resolveChain tries each source in order, returning the first non-nil result.
func resolveChain(ctx context.Context, sources []credSource) (*BedrockCreds, error) {
	for _, src := range sources {
		creds, err := src(ctx)
		if err == nil && creds != nil {
			return creds, nil
		}
	}
	return nil, fmt.Errorf("no AWS credentials available from any source")
}

// fetchCreds returns cached credentials if still fresh, otherwise resolves anew.
func (p *bedrockProvider) fetchCreds(ctx context.Context) (*BedrockCreds, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil && time.Since(p.refreshed) < credentialTTL {
		return p.cached, nil
	}

	chain := []credSource{
		bearerTokenSource,
		envVarSource,
		iniFileSource,
		cliSource,
	}

	creds, err := resolveChain(ctx, chain)
	if err != nil {
		return nil, err
	}
	p.cached = creds
	p.refreshed = time.Now()
	return creds, nil
}

// bearerTokenSource checks for AWS_BEARER_TOKEN_BEDROCK.
func bearerTokenSource(_ context.Context) (*BedrockCreds, error) {
	if os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "" {
		return &BedrockCreds{}, nil
	}
	return nil, fmt.Errorf("no bearer token")
}

// envVarSource reads standard AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars.
func envVarSource(_ context.Context) (*BedrockCreds, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("env vars not set")
	}
	return &BedrockCreds{
		AccessKeyID:     ak,
		SecretAccessKey: sk,
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	}, nil
}

// iniFileSource reads ~/.aws/credentials in INI format.
func iniFileSource(_ context.Context) (*BedrockCreds, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(home + "/.aws/credentials")
	if err != nil {
		return nil, err
	}
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}
	return parseAWSINI(string(data), profile)
}

// cliSource invokes the AWS CLI to verify credentials are available.
func cliSource(ctx context.Context) (*BedrockCreds, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--output", "json").Output()
	if err != nil {
		return nil, err
	}
	var stub struct{}
	_ = json.Unmarshal(out, &stub)

	home, herr := os.UserHomeDir()
	if herr != nil {
		return nil, herr
	}
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}
	credData, rerr := os.ReadFile(home + "/.aws/credentials")
	if rerr != nil {
		return nil, rerr
	}
	return parseAWSINI(string(credData), profile)
}

// parseAWSINI reads an INI-style AWS credentials file and extracts the named profile.
func parseAWSINI(content, targetProfile string) (*BedrockCreds, error) {
	curProfile := ""
	creds := &BedrockCreds{}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			curProfile = strings.Trim(line, "[]")
			curProfile = strings.TrimPrefix(curProfile, "profile ")
			continue
		}
		if curProfile != targetProfile {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "aws_access_key_id":
			creds.AccessKeyID = val
		case "aws_secret_access_key":
			creds.SecretAccessKey = val
		case "aws_session_token":
			creds.SessionToken = val
		}
	}

	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return nil, fmt.Errorf("profile %q not found or incomplete", targetProfile)
	}
	return creds, nil
}

// resolveBedrockAuth builds an AuthSource for AWS Bedrock.
func resolveBedrockAuth(ctx context.Context, model string) (*AuthSource, error) {
	provider := bedrockProviderInstance()
	creds, err := provider.fetchCreds(ctx)
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	r := awsRegion()
	if model != "" {
		if alt := os.Getenv("ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION"); alt != "" {
			r = alt
		}
	}
	return &AuthSource{
		Method:    AuthAWSSigV4,
		AWSCreds:  creds,
		AWSRegion: r,
	}, nil
}

// InvalidateBedrockCache clears any cached Bedrock credentials.
func InvalidateBedrockCache() {
	p := bedrockProviderInstance()
	p.mu.Lock()
	p.cached = nil
	p.refreshed = time.Time{}
	p.mu.Unlock()
}
