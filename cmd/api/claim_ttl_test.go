package main

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/config"
)

// mirrors attachStorefrontClaimAPI TTL wiring against SecurityEmailTokenTTL.
func claimEmailerTTLForConfig(publicBaseURL string) time.Duration {
	h := storefront.NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithAccountSecurity("test-secret", 10*time.Minute)
	if publicBaseURL != "" {
		h = h.WithAccountSecurityEmailLinks(publicBaseURL, storefront.OrderClaimEmailTokenTTL)
	}
	return h.SecurityEmailTokenTTL()
}

func TestClaimEmailTokenTTL_FromSecurityVerifier(t *testing.T) {
	if got := claimEmailerTTLForConfig(""); got != storefront.DefaultSecurityEmailTokenTTL {
		t.Fatalf("empty PublicBaseURL TTL = %v, want %v", got, storefront.DefaultSecurityEmailTokenTTL)
	}
	if got := claimEmailerTTLForConfig("https://shop.example"); got != storefront.OrderClaimEmailTokenTTL {
		t.Fatalf("PublicBaseURL TTL = %v, want %v", got, storefront.OrderClaimEmailTokenTTL)
	}

	cfgEmpty := &config.Config{}
	cfgPublic := &config.Config{}
	cfgPublic.Server.PublicBaseURL = "https://shop.example"
	if claimEmailerTTLForConfig(cfgEmpty.Server.PublicBaseURL) != storefront.DefaultSecurityEmailTokenTTL {
		t.Fatal("config empty base must use default security email TTL")
	}
	if claimEmailerTTLForConfig(cfgPublic.Server.PublicBaseURL) != storefront.OrderClaimEmailTokenTTL {
		t.Fatal("config public base must use order-claim email TTL")
	}

	if formatClaimTokenTTL(storefront.DefaultSecurityEmailTokenTTL) != "30 minutes" {
		t.Fatalf("format default = %q", formatClaimTokenTTL(storefront.DefaultSecurityEmailTokenTTL))
	}
	if formatClaimTokenTTL(storefront.OrderClaimEmailTokenTTL) != "45 minutes" {
		t.Fatalf("format claim = %q", formatClaimTokenTTL(storefront.OrderClaimEmailTokenTTL))
	}
	// Truncate, never round up past ExpiresAt.
	if formatClaimTokenTTL(90*time.Second) != "1 minute" {
		t.Fatalf("format 90s = %q, want 1 minute", formatClaimTokenTTL(90*time.Second))
	}
}
