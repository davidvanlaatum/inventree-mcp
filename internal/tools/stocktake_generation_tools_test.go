package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStocktakeGenerationClient struct {
	queued      []inventree.PartStocktakeGenerate
	outputs     []inventree.DataOutput
	outputCall  int
	generateErr error
}

func (f *fakeStocktakeGenerationClient) DownloadDataOutput(_ context.Context, outputURL string, _ int64) (inventree.DownloadedDataOutput, error) {
	return inventree.DownloadedDataOutput{Content: []byte("report"), ContentType: "text/plain", SourceURL: outputURL}, nil
}

func (f *fakeStocktakeGenerationClient) GeneratePartStocktake(_ context.Context, request inventree.PartStocktakeGenerate) (inventree.PartStocktakeGenerate, error) {
	f.queued = append(f.queued, request)
	if f.generateErr != nil {
		return inventree.PartStocktakeGenerate{}, f.generateErr
	}
	return inventree.PartStocktakeGenerate{Output: &inventree.DataOutput{PK: 90, Complete: false}}, nil
}

func (f *fakeStocktakeGenerationClient) GetDataOutput(_ context.Context, id int) (inventree.DataOutput, error) {
	if len(f.outputs) == 0 {
		return inventree.DataOutput{PK: id, Complete: true, Progress: 1, Total: 1}, nil
	}
	index := f.outputCall
	if index >= len(f.outputs) {
		index = len(f.outputs) - 1
	}
	f.outputCall++
	return f.outputs[index], nil
}

func stocktakeGenerationDeps(fake *fakeStocktakeGenerationClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) { return fake, nil },
		EnableWriteTools:  true,
		stocktakePlanStore: newStocktakePlanStore(time.Now, func() (string, error) {
			return "stocktake-token", nil
		}),
		stocktakeTaskStore: newStocktakeTaskStore(time.Now),
	}
}

func TestGenerateStocktakeRequiresExactlyOneSelectorAndOutput(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{}
	handler := generateStocktake(stocktakeGenerationDeps(client))

	_, invalid, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 1, CategoryID: 2, GenerateEntry: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalid.Status)

	_, invalid, err = handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalid.Status)
	a.Equal("generate_entry", invalid.Clarification.Field)
}

func TestGenerateStocktakeBindsPlanAndReturnsTaskHandle(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{
		{PK: 90, Complete: false, Progress: 0, Total: 2},
		{PK: 90, Complete: true, Progress: 2, Total: 2, Output: dvgoutils.Ptr("https://inventory.example.test/report.pdf")},
	}}
	deps := stocktakeGenerationDeps(client)
	handler := generateStocktake(deps)

	_, preview, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, LocationID: 7, GenerateEntry: true, GenerateReport: true})
	r.NoError(err)
	a.Equal(StatusOK, preview.Status)
	a.Equal("location", preview.Plan.Selector)
	a.Equal("stocktake-token", preview.PlanHash)

	_, queued, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, LocationID: 7, GenerateEntry: true, GenerateReport: true})
	r.NoError(err)
	a.Equal(StatusPending, queued.Status)
	r.NotNil(queued.Task)
	a.False(queued.Task.Complete)
	r.Len(client.queued, 1)
	a.Equal(7, *client.queued[0].Location)
	a.True(client.queued[0].GenerateEntry)
	a.True(client.queued[0].GenerateReport)

	_, completed, err := pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90})
	r.NoError(err)
	a.Equal(StatusOK, completed.Status)
	r.NotNil(completed.Task)
	a.True(completed.Task.Complete)

	_, reused, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, LocationID: 7, GenerateEntry: true, GenerateReport: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)

}

func TestGenerateStocktakeFailsClosedOnTaskErrorAndMismatchedIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client := &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{{PK: 90, Complete: true, Errors: map[string]any{"detail": "report failed"}}}}
	deps := stocktakeGenerationDeps(client)
	handler := generateStocktake(deps)
	_, preview, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 4, GenerateReport: true})
	r.NoError(err)
	_, queued, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, PartID: 4, GenerateReport: true})
	r.NoError(err)
	a.Equal(StatusPending, queued.Status)
	_, failed, err := pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90})
	r.NoError(err)
	a.Equal(StatusPartialFailure, failed.Status)
	a.True(failed.Task.HasErrors)

	client = &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{{PK: 91, Complete: true}}}
	deps = stocktakeGenerationDeps(client)
	handler = generateStocktake(deps)
	_, preview, err = handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 4, GenerateEntry: true})
	r.NoError(err)
	_, queued, err = handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, PartID: 4, GenerateEntry: true})
	r.NoError(err)
	a.Equal(StatusPending, queued.Status)
	_, failed, err = pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90})
	r.NoError(err)
	a.Equal(StatusPartialFailure, failed.Status)
	a.Contains(failed.RecoveryPlan, "same task_id")
}

func TestGenerateStocktakeDownloadsCompletedReportThroughClientPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{{PK: 90, Complete: true, Output: dvgoutils.Ptr("https://inventory.example.test/report.pdf")}}}
	deps := stocktakeGenerationDeps(client)
	handler := generateStocktake(deps)
	_, preview, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 4, GenerateReport: true})
	r.NoError(err)
	_, queued, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, PartID: 4, GenerateReport: true})
	r.NoError(err)
	a.Equal(StatusPending, queued.Status)
	_, output, err := pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.NotNil(output.Report)
	a.Equal("text/plain", output.Report.ContentType)
}

func TestPollStocktakeGenerationReturnsPendingWithoutEnqueueing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{{PK: 90, Progress: 3, Total: 10, Complete: false}}}
	deps := stocktakeGenerationDeps(client)
	r.NoError(deps.stocktakeTaskStore.bind(ctx, 90, false))

	_, output, err := pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90, WaitSeconds: 1})
	r.NoError(err)
	a.Equal(StatusPending, output.Status)
	r.NotNil(output.Task)
	a.Equal(3, output.Task.Progress)
	a.Equal(10, output.Task.Total)
	a.Equal(1, output.RetryAfterSeconds)
	a.Empty(client.queued)
}

func TestStocktakeTaskStoreBindsPrincipalAndExpires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 22, 0, 0, 0, 0, time.UTC)
	clock := now
	principal := "operator-a"
	store := newStocktakeTaskStore(func() time.Time { return clock })
	store.principal = func(context.Context) string { return principal }
	ctx, _, _ := testhandler.SetupTestHandler(t)
	require.NoError(t, store.bind(ctx, 90, true))
	principal = "operator-b"
	otherCtx, _, _ := testhandler.SetupTestHandler(t)
	assert.False(t, func() bool { _, ok := store.lookup(otherCtx, 90); return ok }())
	principal = "operator-a"
	clock = now.Add(stocktakeTaskLifetime)
	assert.False(t, func() bool { _, ok := store.lookup(ctx, 90); return ok }())
}

func TestStocktakeTaskStoreBoundsReservations(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	store := newStocktakeTaskStore(time.Now)
	store.maxEntries = 1
	store.maxEntriesPerPrincipal = 1
	require.NoError(t, store.reserve(ctx))
	require.Error(t, store.reserve(ctx))
	store.release(ctx)
	require.NoError(t, store.reserve(ctx))
}

func TestStocktakePollingIsAvailableWithoutWriteTools(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	listed := listedPolicyTools(t, ctx, Dependencies{})
	assert.Contains(t, listed, PollStocktakeGenerationToolName)
	assert.NotContains(t, listed, GenerateStocktakeToolName)
}

func TestPollStocktakeGenerationRequiresRequestedReportArtifact(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{outputs: []inventree.DataOutput{{PK: 90, Complete: true}}}
	deps := stocktakeGenerationDeps(client)
	require.NoError(t, deps.stocktakeTaskStore.bind(ctx, 90, true))
	_, output, err := pollStocktakeGeneration(deps)(ctx, &mcp.CallToolRequest{}, PollStocktakeGenerationInput{TaskID: 90})
	r.NoError(err)
	r.Equal(StatusPartialFailure, output.Status)
	r.Contains(output.RecoveryPlan, "report artifact")
}

func TestGenerateStocktakeReturnsAmbiguousRecoveryAfterEnqueueError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := &fakeStocktakeGenerationClient{generateErr: errors.New("transport failed")}
	deps := stocktakeGenerationDeps(client)
	handler := generateStocktake(deps)
	_, preview, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{DryRun: true, PartID: 4, GenerateEntry: true})
	r.NoError(err)
	_, output, err := handler(ctx, &mcp.CallToolRequest{}, GenerateStocktakeInput{Confirm: true, PlanHash: preview.PlanHash, PartID: 4, GenerateEntry: true})
	r.NoError(err)
	r.Equal(StatusPartialFailure, output.Status)
	r.Contains(output.RecoveryPlan, "ambiguous")
}
