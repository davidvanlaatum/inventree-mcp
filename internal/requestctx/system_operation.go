package requestctx

import "context"

type systemOperationKey struct{}

// WithSystemOperation marks ctx as belonging to a non-tool-invoked system
// operation (for example a startup connectivity check), rather than an MCP
// tool call. Outbound InvenTree request logs use this to attribute the call
// to a stable "system.<name>" caller instead of a tool name when no tool
// invocation is in progress.
func WithSystemOperation(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, systemOperationKey{}, name)
}

// SystemOperation returns the system operation name recorded for ctx, if
// any.
func SystemOperation(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(systemOperationKey{}).(string)
	return name, ok && name != ""
}
