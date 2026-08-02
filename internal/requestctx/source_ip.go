package requestctx

import (
	"context"
	"net/netip"
)

type sourceIPKey struct{}

// WithSourceIP records the normalized client source IP resolved at the trusted
// reverse-proxy boundary.
func WithSourceIP(ctx context.Context, sourceIP netip.Addr) context.Context {
	if !sourceIP.IsValid() {
		return ctx
	}
	return context.WithValue(ctx, sourceIPKey{}, sourceIP.Unmap())
}

// SourceIP returns the normalized client source IP recorded for the request.
func SourceIP(ctx context.Context) (netip.Addr, bool) {
	sourceIP, ok := ctx.Value(sourceIPKey{}).(netip.Addr)
	return sourceIP, ok && sourceIP.IsValid()
}
