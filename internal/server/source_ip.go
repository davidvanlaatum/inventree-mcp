package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
)

type sourceIPResolver struct {
	trusted []netip.Prefix
}

type resolvedSourceIP struct {
	address   netip.Addr
	forwarded bool
}

func newSourceIPResolver(rawCIDRs []string) (sourceIPResolver, error) {
	resolver := sourceIPResolver{trusted: make([]netip.Prefix, 0, len(rawCIDRs))}
	for _, raw := range rawCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return sourceIPResolver{}, fmt.Errorf("parse trusted proxy CIDR %q: %w", raw, err)
		}
		if prefix.Bits() == 0 {
			return sourceIPResolver{}, fmt.Errorf("trusted proxy CIDR %q must not trust every address", raw)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (r sourceIPResolver) resolve(req *http.Request) resolvedSourceIP {
	peer, ok := parseRemoteIP(req.RemoteAddr)
	if !ok {
		return resolvedSourceIP{}
	}
	result := resolvedSourceIP{address: peer}
	if !r.isTrusted(peer) {
		return result
	}

	values := req.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return result
	}
	for valueIndex := len(values) - 1; valueIndex >= 0; valueIndex-- {
		remaining := values[valueIndex]
		for {
			separator := strings.LastIndexByte(remaining, ',')
			rawCandidate := remaining
			if separator >= 0 {
				rawCandidate = remaining[separator+1:]
			}
			candidate, err := netip.ParseAddr(strings.TrimSpace(rawCandidate))
			if err != nil {
				return resolvedSourceIP{address: peer}
			}
			candidate = candidate.Unmap()
			result = resolvedSourceIP{address: candidate, forwarded: true}
			if !r.isTrusted(candidate) {
				return result
			}
			if separator < 0 {
				break
			}
			remaining = remaining[:separator]
		}
	}
	return result
}

func (r sourceIPResolver) isTrusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func sourceIPMiddleware(baseLogger *slog.Logger, resolver sourceIPResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		resolved := resolver.resolve(req)
		requestCtx := req.Context()
		logger := baseLogger
		if resolved.address.IsValid() {
			requestCtx = requestctx.WithSourceIP(requestCtx, resolved.address)
			logger = logger.With(
				slog.String("source_ip", resolved.address.String()),
				slog.Bool("source_ip_forwarded", resolved.forwarded),
			)
		}
		requestCtx = logging.WithLogger(requestCtx, logger)
		next.ServeHTTP(w, req.WithContext(requestCtx))
	})
}

func parseRemoteIP(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}
