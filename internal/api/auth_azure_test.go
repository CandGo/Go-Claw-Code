package api

import (
	"context"
	"testing"
	"time"
)

func TestInvalidateFoundryCache(t *testing.T) {
	InvalidateFoundryCache()
}

func TestFoundryProviderInstance(t *testing.T) {
	fp1 := foundryProviderInstance()
	fp2 := foundryProviderInstance()
	if fp1 != fp2 {
		t.Error("foundryProviderInstance should return the same singleton")
	}
}

func TestFoundryToken_ExpiredCacheTriggersRefresh(t *testing.T) {
	fp := &foundryProvider{}
	fp.cached = &FoundryToken{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	_, err := fp.accessToken(context.Background())
	if err == nil {
		t.Error("expected error when all resolvers fail after cache expiry")
	}
}

func TestFoundryToken_ValidCacheIsReused(t *testing.T) {
	fp := &foundryProvider{}
	fp.cached = &FoundryToken{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	tok, err := fp.accessToken(context.Background())
	if err != nil {
		t.Fatalf("expected cached token, got error: %v", err)
	}
	if tok.AccessToken != "valid-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "valid-token")
	}
}
