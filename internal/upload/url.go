package upload

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
)

const defaultMaxRedirects = 3

type DNSResolver func(context.Context, string) ([]netip.Addr, error)

type URLAllowlistEntry struct {
	Scheme string
	Host   string
	Port   string
}

type URLSource struct {
	URL      string
	Filename string
}

type URLFetcher struct {
	Resolver     DNSResolver
	MaxRedirects int
	Allowlist    []URLAllowlistEntry
}

func (f URLFetcher) Fetch(ctx context.Context, source URLSource, opts ReadOptions) (ResolvedSource, error) {
	target, err := parseFetchURL(source.URL)
	if err != nil {
		return ResolvedSource{}, err
	}
	resolver := f.Resolver
	if resolver == nil {
		resolver = lookupNetIP
	}
	if err := validateFetchURL(ctx, target, resolver, f.Allowlist); err != nil {
		return ResolvedSource{}, err
	}

	timeout := effectiveTimeout(opts.Timeout)
	fetchCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && timeout > 0 {
		fetchCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	client := &http.Client{
		Timeout: timeout,
		Transport: secureRoundTripper{
			resolver:  resolver,
			allowlist: f.Allowlist,
		},
	}
	maxRedirects := f.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = defaultMaxRedirects
	}
	client.CheckRedirect = func(req *http.Request, prior []*http.Request) error {
		if len(prior) >= maxRedirects {
			return errors.New("URL upload redirect limit exceeded")
		}
		if err := validateFetchURL(req.Context(), req.URL, resolver, f.Allowlist); err != nil {
			return err
		}
		return nil
	}

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ResolvedSource{}, err
	}
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return ResolvedSource{}, fmt.Errorf("fetch URL upload source %s failed", redactURLForError(source.URL))
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ResolvedSource{}, fmt.Errorf("fetch URL upload source returned status %d", resp.StatusCode)
	}
	content, err := readBounded(fetchCtx, resp.Body, opts)
	if err != nil {
		return ResolvedSource{}, err
	}
	filename := source.Filename
	if filename == "" {
		filename = filenameFromHTTP(resp, path.Base(target.EscapedPath()))
	}
	return ResolvedSource{
		Kind:        SourceURL,
		Filename:    cleanFilename(filename),
		ContentType: resp.Header.Get("Content-Type"),
		Size:        int64(len(content)),
		Content:     content,
	}, nil
}

func parseFetchURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("parse URL upload source failed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("URL upload source scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return nil, errors.New("URL upload source host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("URL upload source must not include userinfo")
	}
	return parsed, nil
}

func validateFetchURL(ctx context.Context, target *url.URL, resolver DNSResolver, allowlist []URLAllowlistEntry) error {
	addresses, err := resolver(ctx, target.Hostname())
	if err != nil {
		return errors.New("resolve URL upload source host failed")
	}
	if len(addresses) == 0 {
		return errors.New("resolve URL upload source host returned no addresses")
	}
	if allowlistMatches(target, allowlist) {
		return nil
	}
	for _, address := range addresses {
		if blockedAddress(address) {
			return errors.New("URL upload source resolved to a blocked address")
		}
	}
	return nil
}

type secureRoundTripper struct {
	resolver  DNSResolver
	allowlist []URLAllowlistEntry
}

func (s secureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return secureURLTransport(req.URL.Scheme, s.resolver, s.allowlist).RoundTrip(req)
}

func secureURLTransport(scheme string, resolver DNSResolver, allowlist []URLAllowlistEntry) *http.Transport {
	dialer := &net.Dialer{}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			target := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}
			addresses, err := resolver(ctx, host)
			if err != nil {
				return nil, errors.New("resolve URL upload source host failed")
			}
			if len(addresses) == 0 {
				return nil, errors.New("resolve URL upload source host returned no addresses")
			}
			allowlisted := allowlistMatches(target, allowlist)
			for _, address := range addresses {
				address = address.Unmap()
				if !allowlisted && blockedAddress(address) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
			}
			return nil, errors.New("URL upload source resolved to a blocked address")
		},
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   true,
		DisableCompression:  true,
		MaxIdleConns:        10,
		IdleConnTimeout:     DefaultTimeout,
		TLSHandshakeTimeout: DefaultTimeout,
	}
}

func allowlistMatches(target *url.URL, allowlist []URLAllowlistEntry) bool {
	for _, entry := range allowlist {
		if strings.EqualFold(entry.Scheme, target.Scheme) &&
			normalizedHost(entry.Host) == normalizedHost(target.Hostname()) &&
			entry.Port == effectivePort(target) {
			return true
		}
	}
	return false
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	switch target.Scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func normalizedHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func lookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip.IP); ok {
			out = append(out, addr.Unmap())
		}
	}
	return out, nil
}

func blockedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	if addr.Is4In6() {
		return true
	}
	if addr.Is4() {
		return blockedIPv4(addr)
	}
	return blockedIPv6(addr)
}

func blockedIPv4(addr netip.Addr) bool {
	blocked := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func blockedIPv6(addr netip.Addr) bool {
	blocked := []netip.Prefix{
		netip.MustParsePrefix("::/128"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("::ffff:0:0/96"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("fc00::/7"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("ff00::/8"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
