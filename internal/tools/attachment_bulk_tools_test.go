package tools

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fake
// ---------------------------------------------------------------------------

type bulkFakeAttachments struct {
	mu             sync.Mutex
	attachments    map[int]inventree.Attachment
	updateErr      map[int]error
	applyBeforeErr map[int]bool
	updateCalls    int
}

func newBulkFakeAttachments() *bulkFakeAttachments {
	return &bulkFakeAttachments{
		attachments: map[int]inventree.Attachment{}, updateErr: map[int]error{}, applyBeforeErr: map[int]bool{},
	}
}

func (f *bulkFakeAttachments) GetPart(context.Context, int) (inventree.Part, error) {
	return inventree.Part{}, nil
}
func (f *bulkFakeAttachments) ListAttachments(context.Context, inventree.AttachmentQuery) ([]inventree.Attachment, error) {
	return nil, nil
}
func (f *bulkFakeAttachments) GetAttachmentMetadata(_ context.Context, id int) (inventree.Attachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.attachments[id]
	if !ok {
		return inventree.Attachment{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return record, nil
}
func (f *bulkFakeAttachments) DownloadAttachment(context.Context, int, inventree.AttachmentContentMode, int64) (inventree.DownloadedAttachment, error) {
	return inventree.DownloadedAttachment{}, nil
}
func (f *bulkFakeAttachments) UploadAttachment(context.Context, inventree.AttachmentCreate) (inventree.Attachment, error) {
	return inventree.Attachment{}, nil
}
func (f *bulkFakeAttachments) CreateLinkAttachment(context.Context, inventree.AttachmentCreate) (inventree.Attachment, error) {
	return inventree.Attachment{}, nil
}
func (f *bulkFakeAttachments) UpdateAttachmentMetadata(_ context.Context, id int, fields inventree.PatchFields) (inventree.Attachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls++
	record := f.attachments[id]
	if filename, ok := fields["filename"]; ok {
		record.Filename = filename.Value().(string)
	}
	if comment, ok := fields["comment"]; ok {
		record.Comment = comment.Value().(string)
	}
	if tags, ok := fields["tags"]; ok {
		record.Tags = tags.Value().([]string)
	}
	if f.applyBeforeErr[id] {
		f.attachments[id] = record
	}
	if err := f.updateErr[id]; err != nil {
		return inventree.Attachment{}, err
	}
	f.attachments[id] = record
	return record, nil
}
func (f *bulkFakeAttachments) DeleteAttachment(context.Context, int) error { return nil }
func (f *bulkFakeAttachments) SetPartPrimaryImage(context.Context, int, inventree.PartPrimaryImageCreate) (inventree.Part, error) {
	return inventree.Part{}, nil
}

func testAttachmentBulkDeps(client any) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.attachmentBulkPlanStore = mustBulkStore(batch.Options[attachmentBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p attachmentBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	return deps
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBulkUpdateAttachmentsDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Filename: "old.txt", Comment: "old"}
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("new")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("new", confirmOut.Items[0].Record.Comment)
	a.Equal(1, client.updateCalls)
}

func TestBulkUpdateAttachmentsSkipsAlreadyAtTargetState(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "already-set"}
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("already-set")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeSkipped), confirmOut.Items[0].Outcome)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdateAttachmentsRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Filename: "a.txt"}
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("x")}, {ID: 1, Comment: dvgoutils.Ptr("y")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(bulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Equal(bulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(bulkReasonDuplicateID, dryOut.Items[0].Message)
}

func TestBulkUpdateAttachmentsRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Filename: "old.txt"}
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Filename: dvgoutils.Ptr("new.txt")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)

	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Filename: "someone-else-changed-it.txt"}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdateAttachmentsMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "old"}
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("new")}, {ID: 999, Comment: dvgoutils.Ptr("missing")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.Equal(bulkOutcomeFailed, dryOut.Items[1].Outcome)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 2)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	a.Equal(string(batch.OutcomeFailed), confirmOut.Items[1].Outcome)
}

func TestBulkUpdateAttachmentsRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "old"}
	client.updateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.applyBeforeErr[1] = true
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	items := []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("new")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("new", confirmOut.Items[0].Record.Comment)
}

func TestBulkUpdateAttachmentsRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	deps := testAttachmentBulkDeps(client)
	handler := bulkUpdateAttachments(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	items := make([]BulkUpdateAttachmentItem, bulkUpdateMaxItems+1)
	for i := range items {
		items[i] = BulkUpdateAttachmentItem{ID: i + 1, Comment: dvgoutils.Ptr("x")}
	}
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: items})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkUpdateAttachmentsRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "A"}
	client.attachments[2] = inventree.Attachment{PK: 2, ModelType: "part", ModelID: 6, Comment: "B"}
	deps := testAttachmentBulkDeps(client)
	deps.attachmentBulkPlanStore = mustBulkStore(batch.Options[attachmentBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p attachmentBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkUpdateAttachments(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("A2")}}, DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateAttachmentsInput{Items: []BulkUpdateAttachmentItem{{ID: 2, Comment: dvgoutils.Ptr("B2")}}, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

func TestBuildAttachmentBulkPlanRejectsOutOfScopeModelType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "salesorder", ModelID: 5}

	plan := buildAttachmentBulkPlan(ctx, client, []BulkUpdateAttachmentItem{{ID: 1, Comment: dvgoutils.Ptr("x")}})
	r.Len(plan.Items, 1)
	a.NotEmpty(plan.Items[0].FailReason)
}

func TestAttachmentBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeAttachments()
	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "before"}
	item := buildAttachmentBulkPlanItem(ctx, client, BulkUpdateAttachmentItem{ID: 1, Comment: dvgoutils.Ptr("after")})
	r.Empty(item.FailReason)

	client.attachments[1] = inventree.Attachment{PK: 1, ModelType: "part", ModelID: 5, Comment: "drifted"}
	adapter := &attachmentBulkAdapter{client: client}
	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Equal(bulkReasonDrifted, reason)
	r.Error(err)
}
