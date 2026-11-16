package storefrontsession_test

import (
	"net/http"
	"testing"

	"github.com/akarso/shopanda/pkg/storefrontsession"
)

func TestSessionTokenFromRequest(t *testing.T) {
	if got := storefrontsession.SessionTokenFromRequest(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}

	req, _ := http.NewRequest("GET", "/", nil)
	if got := storefrontsession.SessionTokenFromRequest(req); got != "" {
		t.Fatalf("no cookie = %q", got)
	}

	req.AddCookie(&http.Cookie{Name: "other", Value: "x"})
	if got := storefrontsession.SessionTokenFromRequest(req); got != "" {
		t.Fatalf("wrong name = %q", got)
	}

	req2, _ := http.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: storefrontsession.SessionCookieName, Value: "  "})
	if got := storefrontsession.SessionTokenFromRequest(req2); got != "" {
		t.Fatalf("whitespace = %q", got)
	}

	req3, _ := http.NewRequest("GET", "/", nil)
	req3.AddCookie(&http.Cookie{Name: storefrontsession.SessionCookieName, Value: " tok "})
	if got := storefrontsession.SessionTokenFromRequest(req3); got != "tok" {
		t.Fatalf("got %q, want tok", got)
	}
}
