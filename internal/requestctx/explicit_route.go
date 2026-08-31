package requestctx

import "context"

// explicitRoute carries a caller-asserted (operation, family) pair for an
// outbound request whose destination URL alone cannot identify which
// registry entry it belongs to. This is needed for the media-download
// family: InvenTree returns opaque signed /media/... URLs for attachments,
// part images, company images, and data outputs, and those URLs carry no
// distinguishing marker of which Client method requested them. The issuing
// method sets this explicitly instead of relying on path-template matching.
type explicitRoute struct {
	Operation string
	Family    string
}

type explicitRouteKey struct{}

// WithExplicitRoute attaches the caller-known (operation, family) pair to
// ctx for the single outbound request this context is used for.
func WithExplicitRoute(ctx context.Context, operation, family string) context.Context {
	return context.WithValue(ctx, explicitRouteKey{}, explicitRoute{Operation: operation, Family: family})
}

// ExplicitRoute returns the (operation, family) pair set by
// WithExplicitRoute, if any.
func ExplicitRoute(ctx context.Context) (operation string, family string, ok bool) {
	route, ok := ctx.Value(explicitRouteKey{}).(explicitRoute)
	if !ok {
		return "", "", false
	}
	return route.Operation, route.Family, true
}
