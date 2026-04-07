package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSigV4Signer_SkipsEmptyCredentials(t *testing.T) {
	signer := NewSigV4Signer(nil, "us-east-1")
	req := mustRequest("GET", "https://bedrock-runtime.us-east-1.amazonaws.com/model/claude")
	signer.Sign(req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("expected no Authorization header with nil creds, got %q", got)
	}
}

func TestSigV4Signer_SkipsIncompleteCredentials(t *testing.T) {
	signer := NewSigV4Signer(&BedrockCreds{AccessKeyID: "", SecretAccessKey: ""}, "us-east-1")
	req := mustRequest("GET", "https://bedrock-runtime.us-east-1.amazonaws.com/model/claude")
	signer.Sign(req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("expected no Authorization header with empty creds, got %q", got)
	}
}

func TestSigV4Signer_SetsAuthorizationHeader(t *testing.T) {
	creds := &BedrockCreds{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	signer := NewSigV4Signer(creds, "us-east-1")
	req := mustRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/claude-3/invoke")
	req.Header.Set("Content-Type", "application/json")
	signer.Sign(req)

	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatal("expected Authorization header to be set")
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Errorf("Authorization should start with AWS4-HMAC-SHA256, got %q", auth[:30])
	}
	if !strings.Contains(auth, "Credential=AKIAIOSFODNN7EXAMPLE/") {
		t.Errorf("Credential should contain access key, got %q", auth)
	}
	if !strings.Contains(auth, "us-east-1/bedrock/aws4_request") {
		t.Errorf("Credential scope should contain region/service, got %q", auth)
	}
}

func TestSigV4Signer_SetsAmzDateHeader(t *testing.T) {
	creds := &BedrockCreds{AccessKeyID: "AKID", SecretAccessKey: "secret"}
	signer := NewSigV4Signer(creds, "us-west-2")
	req := mustRequest("GET", "https://bedrock-runtime.us-west-2.amazonaws.com/")
	signer.Sign(req)

	amzDate := req.Header.Get("X-Amz-Date")
	if amzDate == "" {
		t.Fatal("X-Amz-Date header not set")
	}
	if len(amzDate) != 16 {
		t.Errorf("X-Amz-Date should be 16 chars (yyyyMMddTHHmmssZ), got %q", amzDate)
	}
}

func TestSigV4Signer_SessionTokenHeader(t *testing.T) {
	creds := &BedrockCreds{
		AccessKeyID:     "AKID",
		SecretAccessKey: "secret",
		SessionToken:    "tok123",
	}
	signer := NewSigV4Signer(creds, "us-east-1")
	req := mustRequest("GET", "https://example.com/")
	signer.Sign(req)

	if got := req.Header.Get("X-Amz-Security-Token"); got != "tok123" {
		t.Errorf("expected security token header, got %q", got)
	}
}

func TestHashHex(t *testing.T) {
	// SHA-256 of empty string is well-known
	got := hashHex("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("hashHex('') = %s, want %s", got, want)
	}
}

func TestEncodeCanonicalQuery(t *testing.T) {
	q := url.Values{}
	q.Set("b", "2")
	q.Set("a", "1")
	got := encodeCanonicalQuery(q)
	want := "a=1&b=2"
	if got != want {
		t.Errorf("encodeCanonicalQuery = %q, want %q", got, want)
	}
}

func TestEncodeCanonicalQueryEmpty(t *testing.T) {
	got := encodeCanonicalQuery(url.Values{})
	if got != "" {
		t.Errorf("expected empty string for empty query, got %q", got)
	}
}

func TestCollectSignedHeaderNames(t *testing.T) {
	req := mustRequest("GET", "https://example.com/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom", "value")
	// Authorization and User-Agent should be excluded
	req.Header.Set("Authorization", "should-be-ignored")
	req.Header.Set("User-Agent", "test")

	names := collectSignedHeaderNames(req)
	if strings.Contains(names, "authorization") {
		t.Error("signed headers should not contain 'authorization'")
	}
	if strings.Contains(names, "user-agent") {
		t.Error("signed headers should not contain 'user-agent'")
	}
	if !strings.Contains(names, "content-type") {
		t.Error("signed headers should contain 'content-type'")
	}
}

func mustRequest(method, rawURL string) *http.Request {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		panic(err)
	}
	return req
}
