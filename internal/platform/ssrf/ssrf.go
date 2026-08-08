package ssrf

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Well-known NAT64 prefix (RFC 6052). Last 32 bits embed an IPv4 address.
var nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")

// IANA special-purpose IPv4 ranges not covered by net.IP IsPrivate/IsLoopback/
// IsLinkLocal* (CGNAT, documentation/TEST-NET, benchmarking, reserved).
// See https://www.iana.org/assignments/iana-ipv4-special-registry/
var specialPurposeIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT / shared address space (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking (RFC 2544)
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved / class E
}

// LookupIPFunc resolves a hostname to IP addresses (injectable for tests).
type LookupIPFunc func(ctx context.Context, host string) ([]net.IP, error)

// dialContextFunc dials a network address (injectable for allow-path tests).
type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// DefaultLookupIP uses the system resolver.
func DefaultLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// IsBlockedIP reports whether ip is unsuitable for outbound webhook destinations
// (loopback, RFC1918, link-local, ULA, unspecified, multicast, IANA special-purpose
// IPv4 including CGNAT/TEST-NET/benchmarking/reserved, NAT64).
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4-mapped IPv6 (::ffff:x.x.x.x) → check the embedded IPv4.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		addr, ok := netip.AddrFromSlice(ip4)
		if !ok {
			return true
		}
		for _, p := range specialPurposeIPv4 {
			if p.Contains(addr) {
				return true
			}
		}
		return false
	}

	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	// Well-known NAT64: block the prefix (translator can reach internal IPv4).
	if nat64WellKnown.Contains(addr) {
		return true
	}
	return false
}

// ValidateURL checks scheme/host and rejects literal / non-canonical blocked IPs.
// Hostnames are not resolved here — callers that dial must use SafeDialContext.
func ValidateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("ssrf: url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("ssrf: invalid url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("ssrf: url must use https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("ssrf: url host is required")
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return fmt.Errorf("ssrf: destination address %s is not allowed", ip.String())
		}
		return nil
	}
	// Resolvers often accept inet_aton forms (127.1, 2130706433, 0x7f000001)
	// that net.ParseIP rejects — refuse them at create/update time.
	if looksLikeNonCanonicalIP(host) {
		return fmt.Errorf("ssrf: non-canonical IP host is not allowed")
	}
	return nil
}

// looksLikeNonCanonicalIP reports hosts that are IP-shaped but not net.ParseIP.
func looksLikeNonCanonicalIP(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || strings.Contains(h, ":") {
		// IPv6 literals are handled by ParseIP; anything else with ':' is not this class.
		return false
	}
	// Decimal or hex integer forms of IPv4 (e.g. 2130706433, 0x7f000001).
	if strings.HasPrefix(h, "0x") {
		if _, err := strconv.ParseUint(h[2:], 16, 32); err == nil {
			return true
		}
	}
	if _, err := strconv.ParseUint(h, 10, 32); err == nil {
		return true
	}
	// Dotted inet_aton forms with 1–4 numeric parts (e.g. 127.1, 0177.0.0.1).
	parts := strings.Split(h, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if !isInetAtonPart(p) {
			return false
		}
	}
	return true
}

func isInetAtonPart(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "0x") {
		_, err := strconv.ParseUint(p[2:], 16, 32)
		return err == nil
	}
	// Decimal or octal (leading zero) — reject any all-digit / octal-looking part.
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	return true
}

// FilterAllowedIPs returns only public IPs. If any resolved address is blocked,
// or none remain, it returns an error (strict: mixed A/AAAA sets are rejected).
func FilterAllowedIPs(ips []net.IP) ([]net.IP, error) {
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf: host resolved to no addresses")
	}
	allowed := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if IsBlockedIP(ip) {
			return nil, fmt.Errorf("ssrf: destination resolves to disallowed address %s", ip.String())
		}
		allowed = append(allowed, ip)
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("ssrf: host resolved to no allowed addresses")
	}
	return allowed, nil
}

// SafeDialContext returns a DialContext that resolves via lookup, rejects any
// blocked address, and connects only to an allowed IP (DNS-rebinding safe).
func SafeDialContext(lookup LookupIPFunc) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return safeDialContext(lookup, dialer.DialContext)
}

func safeDialContext(lookup LookupIPFunc, dial dialContextFunc) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if lookup == nil {
		lookup = DefaultLookupIP
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("ssrf: split addr: %w", err)
		}
		if net.ParseIP(host) == nil && looksLikeNonCanonicalIP(host) {
			return nil, fmt.Errorf("ssrf: non-canonical IP host is not allowed")
		}
		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			ips, err = lookup(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("ssrf: lookup %s: %w", host, err)
			}
		}
		allowed, err := FilterAllowedIPs(ips)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range allowed {
			target := net.JoinHostPort(ip.String(), port)
			conn, err := dial(ctx, network, target)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("ssrf: no dialable address")
		}
		return nil, lastErr
	}
}

// NewHTTPClient builds an http.Client that dials only SSRF-safe addresses.
// Redirects are disabled. Environment HTTP(S)_PROXY is ignored (egress proxy
// is out of scope — honoring it would skip destination IP checks).
func NewHTTPClient(timeout time.Duration, lookup LookupIPFunc) *http.Client {
	base, _ := http.DefaultTransport.(*http.Transport)
	var transport *http.Transport
	if base != nil {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	transport.DialContext = SafeDialContext(lookup)
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
