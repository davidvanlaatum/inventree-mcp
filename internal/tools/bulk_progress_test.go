package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// bulkUpdateMaxItems mirrors defaultBulkMaxItems (bulk_progress.go) that bulk
// tool tests exercise via testXBulkDeps' explicit BulkMaxItems, keeping every
// existing size-boundary test's literal batch sizes unchanged since F-S81
// removed the old fixed bulkUpdateMaxItems package constant of the same name.
const bulkUpdateMaxItems = defaultBulkMaxItems

// structuredContentTo re-marshals a real CallTool result's StructuredContent
// (delivered over the wire as map[string]any) back into a typed Go value.
func structuredContentTo(result *mcp.CallToolResult, out any) error {
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func TestNewBulkProgressReporterNilWhenNoSession(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"progressToken": "abc"}}}
	reporter := newBulkProgressReporter(req, "some_tool")

	r.Nil(reporter)
	// A nil reporter's report must be a safe no-op.
	reporter.report(context.Background(), 1, 1)
}

func TestNewBulkProgressReporterNilWhenNoProgressToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	req := &mcp.CallToolRequest{Session: &mcp.ServerSession{}, Params: &mcp.CallToolParamsRaw{}}
	reporter := newBulkProgressReporter(req, "some_tool")

	r.Nil(reporter)
}

func TestNewBulkProgressReporterNilWhenRequestIsNil(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	reporter := newBulkProgressReporter(nil, "some_tool")

	r.Nil(reporter)
}

// bulkProgressSession connects a real in-memory client/server session so
// tests can exercise bulkProgressReporter.report against a live
// *mcp.ServerSession rather than a bare struct.
func bulkProgressSession(t *testing.T, ctx context.Context, onProgress func(message string, progress, total float64)) (*mcp.ServerSession, *mcp.ClientSession, func()) {
	t.Helper()
	r := require.New(t)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{Name: "bulk-progress-test-server", Version: "v0.0.0"}, nil)
	serverSessionCh := make(chan *mcp.ServerSession, 1)
	serverDone := make(chan error, 1)
	serverCtx, cancel := context.WithCancel(ctx)
	go func() {
		session, err := server.Connect(serverCtx, serverTransport, nil)
		if err != nil {
			serverDone <- err
			return
		}
		serverSessionCh <- session
		serverDone <- session.Wait()
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "bulk-progress-test-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			if onProgress != nil {
				onProgress(req.Params.Message, req.Params.Progress, req.Params.Total)
			}
		},
	})
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)

	serverSession := <-serverSessionCh
	return serverSession, clientSession, func() {
		r.NoError(clientSession.Close())
		cancel()
		<-serverDone
	}
}

func TestBulkProgressReporterThrottlesInteriorButAlwaysSendsFinal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var mu sync.Mutex
	var received []float64
	serverSession, _, closeSession := bulkProgressSession(t, ctx, func(_ string, progress, _ float64) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, progress)
	})
	defer closeSession()

	reporter := &bulkProgressReporter{session: serverSession, token: "tok", toolName: "test_tool"}
	const total = 20
	for i := 1; i <= total; i++ {
		reporter.report(ctx, i, total)
	}

	// Give notifications (sent over the in-memory transport) a moment to
	// arrive before asserting.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) > 0 && received[len(received)-1] == float64(total)
	}, time.Second, 10*time.Millisecond, "final done==total notification must always be delivered")

	mu.Lock()
	defer mu.Unlock()
	r.Less(len(received), total, "interior notifications faster than the throttle interval must be dropped")
	r.Equal(float64(total), received[len(received)-1])
}

func TestBulkProgressReporterNotifyFailureIsNonFatal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	// A reporter bound to a session whose client has already disconnected:
	// NotifyProgress will fail, and report must swallow that error rather
	// than panicking or propagating it.
	serverSession, _, closeSession := bulkProgressSession(t, ctx, nil)
	closeSession()

	reporter := &bulkProgressReporter{session: serverSession, token: "tok", toolName: "test_tool"}
	r.NotPanics(func() {
		reporter.report(ctx, 1, 1)
	})
}

func TestBulkUpdatePartsEmitsProgressNotificationsOverRealMCPSession(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget A", Active: true}
	client.parts[2] = inventree.PartDetail{PK: 2, Name: "Widget B", Active: true}
	deps := testCatalogBulkDeps(client)
	deps.EnableWriteTools = true

	server := mcp.NewServer(&mcp.Implementation{Name: "bulk-progress-e2e-server", Version: "v0.0.0"}, nil)
	Register(server, deps)

	var mu sync.Mutex
	var progressCalls []float64
	var totalSeen float64
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverCtx, cancel := context.WithCancel(ctx)
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverCtx, serverTransport) }()

	caller := mcp.NewClient(&mcp.Implementation{Name: "bulk-progress-e2e-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			progressCalls = append(progressCalls, req.Params.Progress)
			totalSeen = req.Params.Total
		},
	})
	session, err := caller.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}()

	items := []BulkUpdatePartItem{
		{ID: 1, Active: dvgoutils.Ptr(false)},
		{ID: 2, Active: dvgoutils.Ptr(false)},
	}
	dryResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: BulkUpdatePartsToolName, Arguments: BulkUpdatePartsInput{Items: items, DryRun: true}})
	r.NoError(err)
	r.False(dryResult.IsError)
	var dryOut BulkUpdateOutput[PartDetailView]
	r.NoError(structuredContentTo(dryResult, &dryOut))

	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      BulkUpdatePartsToolName,
		Arguments: BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash},
		Meta:      mcp.Meta{"progressToken": "batch-1"},
	})
	r.NoError(err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(progressCalls) > 0 && progressCalls[len(progressCalls)-1] == totalSeen
	}, time.Second, 10*time.Millisecond, "a progress-token call must eventually report a final done==total notification")
}

func TestBulkUpdatePartsOmitsProgressNotificationsWithoutToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget A", Active: true}
	deps := testCatalogBulkDeps(client)
	deps.EnableWriteTools = true

	server := mcp.NewServer(&mcp.Implementation{Name: "bulk-progress-no-token-server", Version: "v0.0.0"}, nil)
	Register(server, deps)

	var mu sync.Mutex
	var progressCalls int
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverCtx, cancel := context.WithCancel(ctx)
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverCtx, serverTransport) }()

	caller := mcp.NewClient(&mcp.Implementation{Name: "bulk-progress-no-token-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, _ *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			progressCalls++
		},
	})
	session, err := caller.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}()

	items := []BulkUpdatePartItem{{ID: 1, Active: dvgoutils.Ptr(false)}}
	dryResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: BulkUpdatePartsToolName, Arguments: BulkUpdatePartsInput{Items: items, DryRun: true}})
	r.NoError(err)
	var dryOut BulkUpdateOutput[PartDetailView]
	r.NoError(structuredContentTo(dryResult, &dryOut))

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: BulkUpdatePartsToolName, Arguments: BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash}})
	r.NoError(err)

	// No progress token was attached, so no notification should ever arrive.
	// Sleep briefly rather than Eventually, since we're asserting an absence.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	r.Equal(0, progressCalls)
}
