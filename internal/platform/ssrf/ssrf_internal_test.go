package ssrf

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSafeDialContext_DialsAllowedResolvedIP(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		if host != "hooks.example" {
			t.Fatalf("lookup host = %q", host)
		}
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	var dialed string
	dial := safeDialContext(lookup, func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		c1, c2 := net.Pipe()
		_ = c2.Close()
		return c1, nil
	})
	conn, err := dial(context.Background(), "tcp", "hooks.example:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if dialed != "8.8.8.8:443" {
		t.Fatalf("dialed %q, want 8.8.8.8:443", dialed)
	}
	_, _ = io.Copy(io.Discard, conn)
}

func TestSafeDialContext_RejectsNonCanonicalHost(t *testing.T) {
	dial := safeDialContext(nil, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dial should not be called")
		return nil, nil
	})
	_, err := dial(context.Background(), "tcp", "127.1:443")
	if err == nil || !strings.Contains(err.Error(), "non-canonical") {
		t.Fatalf("err=%v, want non-canonical", err)
	}
}
