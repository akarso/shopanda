package ssrf_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/ssrf"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.5.5",
		"192.168.1.1",
		"169.254.169.254",
		"::1",
		"fe80::1",
		"fc00::1",
		"0.0.0.0",
		"0.1.2.3",
		"100.64.1.1",
		"198.18.0.1",
		"192.0.2.1",
		"192.88.99.1",
		"198.51.100.1",
		"203.0.113.10",
		"240.0.0.1",
		"64:ff9b::10.0.0.1",
		"64:ff9b::8.8.8.8",
		"::ffff:10.0.0.1",
	}
	for _, s := range blocked {
		if !ssrf.IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	if ssrf.IsBlockedIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should be allowed")
	}
	if ssrf.IsBlockedIP(net.ParseIP("2001:4860:4860::8888")) {
		t.Error("public IPv6 should be allowed")
	}
}

func TestValidateURL_LiteralPrivate(t *testing.T) {
	err := ssrf.ValidateURL("https://127.0.0.1/hook")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want not allowed", err)
	}
	err = ssrf.ValidateURL("https://169.254.169.254/latest")
	if err == nil {
		t.Fatal("expected metadata IP rejection")
	}
	err = ssrf.ValidateURL("https://198.18.0.1/hook")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want benchmarking range rejection", err)
	}
	if err := ssrf.ValidateURL("https://example.com/hook"); err != nil {
		t.Fatalf("public hostname: %v", err)
	}
	if err := ssrf.ValidateURL("http://example.com/hook"); err == nil {
		t.Fatal("expected http rejection")
	}
}

func TestValidateURL_NonCanonicalIP(t *testing.T) {
	cases := []string{
		"https://127.1/hook",
		"https://2130706433/hook",
		"https://0x7f000001/hook",
		"https://0177.0.0.1/hook",
	}
	for _, raw := range cases {
		err := ssrf.ValidateURL(raw)
		if err == nil || !strings.Contains(err.Error(), "non-canonical") {
			t.Fatalf("%s: err=%v, want non-canonical", raw, err)
		}
	}
}

func TestFilterAllowedIPs_RejectsAnyBlocked(t *testing.T) {
	_, err := ssrf.FilterAllowedIPs([]net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("10.0.0.1"),
	})
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("err=%v, want disallowed for mixed set", err)
	}

	got, err := ssrf.FilterAllowedIPs([]net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")})
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestSafeDialContext_RejectsPrivateResolution(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	dial := ssrf.SafeDialContext(lookup)
	_, err := dial(context.Background(), "tcp", "evil.example:443")
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("err=%v, want disallowed", err)
	}
}

func TestSafeDialContext_RejectsRebindingMixedRecords(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("192.168.0.5")}, nil
	}
	dial := ssrf.SafeDialContext(lookup)
	_, err := dial(context.Background(), "tcp", "rebinding.example:443")
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("err=%v, want reject when any record is private", err)
	}
}

func TestNewHTTPClient_DisablesProxyAndDefaultDialTLS(t *testing.T) {
	c := ssrf.NewHTTPClient(time.Second, nil)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type %T", c.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("Proxy must be nil so HTTP(S)_PROXY cannot bypass destination IP checks")
	}
	if tr.DialTLS != nil || tr.DialTLSContext != nil {
		t.Fatal("DialTLS hooks must be unset so TLS uses DialContext (SafeDialContext)")
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext must be set")
	}
}
