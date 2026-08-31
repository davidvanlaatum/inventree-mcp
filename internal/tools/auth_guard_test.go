package tools

import (
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizeToolRecordsInternalFailureForUnknownTool(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx = WithOutcomeRecorder(ctx)

	result, denied := authorizeTool(ctx, "https://mcp.example.test/.well-known/oauth-protected-resource", "not_a_registered_tool")

	r.True(denied)
	r.NotNil(result)
	a.True(result.IsError)
	outcome, ok := OutcomeFromContext(ctx)
	a.True(ok)
	a.Equal(OutcomeInternalFailure, outcome)
}

func TestAuthorizeToolRecordsAuthorizationFailureWhenTokenInfoIsMissing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx = WithOutcomeRecorder(ctx)

	authz, ok := ToolAuthorizations[GetPartToolName]
	r.True(ok)
	r.NotEmpty(authz.Scopes, "GetPartToolName must require at least one scope for this test to be meaningful")

	result, denied := authorizeTool(ctx, "https://mcp.example.test/.well-known/oauth-protected-resource", GetPartToolName)

	r.True(denied)
	r.NotNil(result)
	a.True(result.IsError)
	outcome, ok := OutcomeFromContext(ctx)
	a.True(ok)
	a.Equal(OutcomeAuthorizationFailure, outcome)
}

func TestAuthorizeToolAllowsScopelessToolWithoutRecordingAnOutcome(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx = WithOutcomeRecorder(ctx)

	authz, ok := ToolAuthorizations[HealthVersionToolName]
	r.True(ok)
	r.Empty(authz.Scopes, "HealthVersionToolName must require no scopes for this test to be meaningful")

	result, denied := authorizeTool(ctx, "https://mcp.example.test/.well-known/oauth-protected-resource", HealthVersionToolName)

	r.False(denied)
	a.Nil(result)
	_, ok = OutcomeFromContext(ctx)
	a.False(ok, "a scopeless tool must not record any outcome; authorizeTool did not run a classification branch")
}
