package api

import (
	"context"
	"testing"
	"time"
)

func TestInvalidateVertexCache(t *testing.T) {
	InvalidateVertexCache()
}

func TestVertexProviderInstance(t *testing.T) {
	vp1 := vertexProviderInstance()
	vp2 := vertexProviderInstance()
	if vp1 != vp2 {
		t.Error("vertexProviderInstance should return the same singleton")
	}
}

func TestVertexToken_ExpiredCacheTriggersRefresh(t *testing.T) {
	vp := &vertexProvider{}
	vp.cached = &VertexToken{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	// All resolvers will fail (no env vars, no gcloud, no GCP metadata)
	_, err := vp.token(context.Background())
	if err == nil {
		t.Error("expected error when all resolvers fail after cache expiry")
	}
}

func TestVertexToken_ValidCacheIsReused(t *testing.T) {
	vp := &vertexProvider{}
	vp.cached = &VertexToken{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	tok, err := vp.token(context.Background())
	if err != nil {
		t.Fatalf("expected cached token, got error: %v", err)
	}
	if tok.AccessToken != "valid-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "valid-token")
	}
}
