package main

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
)

func TestStorefrontClaimPageURL(t *testing.T) {
	ssr := &config.Config{}
	ssr.Frontend.Enabled = true
	ssr.Frontend.Mode = "ssr"
	ssr.Server.PublicBaseURL = "https://api.example.com"
	if got := storefrontClaimPageURL(ssr); got != "https://api.example.com/account/orders/claim" {
		t.Fatalf("ssr = %q", got)
	}

	headless := &config.Config{}
	headless.Frontend.Enabled = false
	headless.Server.PublicBaseURL = "https://api.example.com"
	if got := storefrontClaimPageURL(headless); got != "" {
		t.Fatalf("headless without claim_url = %q, want empty", got)
	}

	headless.Frontend.ClaimURL = "https://www.example.com/orders/claim/"
	if got := storefrontClaimPageURL(headless); got != "https://www.example.com/orders/claim" {
		t.Fatalf("explicit claim_url = %q", got)
	}
}
