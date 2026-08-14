package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLocalUploadPolicyReportsCanonicalStdioPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/uploads/nested", 0o755))
	ctx, _, _ := testhandler.SetupTestHandler(t)
	deps := Dependencies{
		UploadMode:       upload.ModeStdio,
		UploadFS:         fs,
		UploadAllowRoots: []string{"/uploads/../uploads", "/uploads", "/uploads/nested"},
		UploadMaxBytes:   10 * 1024 * 1024,
	}

	result, output, err := getLocalUploadPolicy(deps)(ctx, &mcp.CallToolRequest{}, LocalUploadPolicyInput{})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.True(output.LocalPathEnabled)
	a.Equal([]string{"/uploads", "/uploads/nested"}, output.AllowedRoots)
	a.Equal(int64(10*1024*1024), output.AttachmentMaxBytes)
	a.Equal(upload.CompanyImageMaxBytes, output.CompanyImageMaxBytes)
	a.Contains(output.Requirements[1], "does not guarantee")
}

func TestGetLocalUploadPolicyReportsEmptyPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, output, err := getLocalUploadPolicy(Dependencies{UploadMode: upload.ModeStdio, UploadFS: afero.NewMemMapFs()})(ctx, &mcp.CallToolRequest{}, LocalUploadPolicyInput{})
	r.NoError(err)
	a.False(output.LocalPathEnabled)
	a.Empty(output.AllowedRoots)
	a.Equal(upload.DefaultMaxBytes, output.AttachmentMaxBytes)
	a.Equal(upload.CompanyImageMaxBytes, output.CompanyImageMaxBytes)
}

func TestLocalUploadPolicyToolRegistrationIsStdioOnly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	stdioTools := listedPolicyTools(t, ctx, Dependencies{UploadMode: upload.ModeStdio})
	r.Contains(stdioTools, GetLocalUploadPolicyToolName)
	a.True(stdioTools[GetLocalUploadPolicyToolName].Annotations.ReadOnlyHint)
	a.Empty(ToolAuthorizations[GetLocalUploadPolicyToolName].Scopes)
	a.Equal("stdio_only", httpRegistrationForTool(GetLocalUploadPolicyToolName, "read_only"))

	httpTools := listedPolicyTools(t, ctx, Dependencies{UploadMode: upload.ModeHTTP, UploadAllowRoots: []string{"/secret/root"}})
	a.NotContains(httpTools, GetLocalUploadPolicyToolName)
}

func TestGetLocalUploadPolicyThroughMCPBoundary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/uploads", 0o755))

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "local-upload-policy-boundary-test", Version: "v0.0.0"}, nil)
		Register(server, Dependencies{UploadMode: upload.ModeStdio, UploadFS: fs, UploadAllowRoots: []string{"/uploads"}, UploadMaxBytes: 10 * 1024 * 1024})
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: GetLocalUploadPolicyToolName})
	r.NoError(err)
	a.False(result.IsError)
	structured, ok := result.StructuredContent.(map[string]any)
	r.True(ok)
	a.Equal(StatusOK, structured["status"])
	a.Equal(true, structured["local_path_enabled"])
	a.Equal(float64(10*1024*1024), structured["attachment_max_bytes"])
	a.Equal(float64(upload.CompanyImageMaxBytes), structured["company_image_max_bytes"])
	a.Equal([]any{"/uploads"}, structured["allowed_roots"])
	a.NotContains(structured, "local_path")
	r.NoError(session.Close())
	cancel()
	serverErr := <-serverDone
	if serverErr != nil && !errors.Is(serverErr, context.Canceled) {
		r.NoError(serverErr)
	}
}

func TestLocalUploadRecoveryClassifiesConfiguredAndMissingAllowlists(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	outside, ok := localUploadRecovery(upload.ErrLocalUploadOutsideAllowlist)
	r.True(ok)
	a.Equal(LocalUploadReasonOutsideAllowlist, outside.Reason)
	a.Equal(GetLocalUploadPolicyToolName, outside.PolicyTool)
	a.Contains(outside.RecoveryPlan, "returned root")

	missing, ok := localUploadRecovery(upload.ErrLocalUploadAllowlistRequired)
	r.True(ok)
	a.Equal(LocalUploadReasonAllowlistRequired, missing.Reason)
	a.Contains(missing.RecoveryPlan, "Ask the operator")
	a.Contains(missing.RecoveryPlan, "inline content")
	a.NotContains(missing.RecoveryPlan, "returned root")

	_, ok = localUploadRecovery(errors.New("unrelated"))
	a.False(ok)
}

func listedPolicyTools(t *testing.T, ctx context.Context, deps Dependencies) map[string]*mcp.Tool {
	t.Helper()
	r := require.New(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "local-upload-policy-test", Version: "v0.0.0"}, nil)
		Register(server, deps)
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	result := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		result[tool.Name] = tool
	}
	r.NoError(session.Close())
	cancel()
	serverErr := <-serverDone
	if serverErr != nil && !errors.Is(serverErr, context.Canceled) {
		r.NoError(serverErr)
	}
	return result
}
