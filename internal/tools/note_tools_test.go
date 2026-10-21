package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNotes struct {
	mu                     sync.Mutex
	notes                  map[int]inventree.Note
	writes                 int
	createResponseLoss     bool
	readError              bool
	siblingErrorAfterWrite bool
	mutateError            bool
}

func (f *fakeNotes) SearchNotesPage(_ context.Context, q inventree.NoteQuery) (inventree.Page[inventree.Note], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readError || (f.siblingErrorAfterWrite && f.writes > 0) {
		return inventree.Page[inventree.Note]{}, errors.New("private-upstream-marker")
	}
	var result []inventree.Note
	for _, n := range f.notes {
		if n.ModelType == q.ModelType && (q.ModelID == 0 || n.ModelID == q.ModelID) {
			result = append(result, n)
		}
	}
	slices.SortFunc(result, func(a, b inventree.Note) int { return a.PK - b.PK })
	return inventree.Page[inventree.Note]{Count: len(result), Results: result}, nil
}
func (f *fakeNotes) GetNote(_ context.Context, id int) (inventree.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readError {
		return inventree.Note{}, assert.AnError
	}
	n, ok := f.notes[id]
	if !ok {
		return inventree.Note{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return n, nil
}
func (f *fakeNotes) CreateNote(_ context.Context, input inventree.NoteCreate) (inventree.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.mutateError {
		return inventree.Note{}, assert.AnError
	}
	id := len(f.notes) + 1
	for f.notes[id].PK != 0 {
		id++
	}
	n := inventree.Note{PK: id, ModelType: input.ModelType, ModelID: input.ModelID, Title: input.Title, Description: input.Description, Content: input.Content}
	if input.Primary != nil {
		n.Primary = *input.Primary
	}
	found := false
	for key, sibling := range f.notes {
		if sibling.ModelType == n.ModelType && sibling.ModelID == n.ModelID {
			found = true
			if n.Primary {
				sibling.Primary = false
				f.notes[key] = sibling
			}
		}
	}
	if !found {
		n.Primary = true
	}
	f.notes[id] = n
	if f.createResponseLoss {
		return inventree.Note{}, errors.New("simulated private response loss")
	}
	return n, nil
}
func (f *fakeNotes) UpdateNote(_ context.Context, id int, fields inventree.PatchFields) (inventree.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.mutateError {
		return inventree.Note{}, assert.AnError
	}
	n := f.notes[id]
	for key, value := range fields {
		switch key {
		case "title":
			n.Title = value.Value().(string)
		case "description":
			n.Description = value.Value().(string)
		case "content":
			n.Content = value.Value().(string)
		case "primary":
			n.Primary = value.Value().(bool)
		}
	}
	if n.Primary {
		for key, sibling := range f.notes {
			if sibling.ModelType == n.ModelType && sibling.ModelID == n.ModelID && key != id {
				sibling.Primary = false
				f.notes[key] = sibling
			}
		}
	}
	f.notes[id] = n
	return n, nil
}
func (f *fakeNotes) DeleteNote(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	delete(f.notes, id)
	return nil
}
func noteTestDeps(f *fakeNotes) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return f, nil }, EnableWriteTools: true, BulkMaxItems: 25, BulkConcurrency: 4, notePlanStore: mustBulkStore(batch.Options[notePlan]{IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal, SupersedeKey: func(p notePlan) string { return p.Action }})}
}

func TestNotesCreatePreviewConfirmReplayAndDuplicateProtection(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	f := &fakeNotes{notes: map[int]inventree.Note{}}
	deps := noteTestDeps(f)
	input := NoteWriteInput{NoteMutationItem: NoteMutationItem{ModelType: "part.part", ModelID: 1, Title: dvgoutils.Ptr("Assembly"), Content: dvgoutils.Ptr("**Instructions**")}, DryRun: true}
	_, preview, err := noteWrite(deps, CreateNoteToolName, "create")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Zero(f.writes)
	r.Len(preview.Items, 1)
	a.True(preview.Items[0].Record.Primary)
	r.NotEmpty(preview.PlanHash)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = preview.PlanHash
	_, out, err := noteWrite(deps, CreateNoteToolName, "create")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("applied", out.Items[0].Outcome)
	r.NotNil(out.Items[0].Record)
	a.Equal(1, out.Items[0].NoteID)
	a.Equal(1, f.writes)
	_, replay, err := noteWrite(deps, CreateNoteToolName, "create")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, replay.Status)
	a.Equal(1, f.writes)
	input.DryRun = true
	input.Confirm = false
	input.PlanHash = ""
	_, duplicate, err := noteWrite(deps, CreateNoteToolName, "create")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal("failed", duplicate.Items[0].Outcome)
}

func TestNotesEditPrimaryAndDeleteUseExactReviewedIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	f := &fakeNotes{notes: map[int]inventree.Note{1: {PK: 1, ModelType: "part.part", ModelID: 1, Title: "First", Content: "keep", Primary: true}, 2: {PK: 2, ModelType: "part.part", ModelID: 1, Title: "Second", Content: "clear"}}}
	deps := noteTestDeps(f)
	input := NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: 2, Content: dvgoutils.Ptr(""), Primary: dvgoutils.Ptr(true)}, DryRun: true}
	_, preview, err := noteWrite(deps, UpdateNoteToolName, "update")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = preview.PlanHash
	_, out, err := noteWrite(deps, UpdateNoteToolName, "update")(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal("applied", out.Items[0].Outcome)
	a.Empty(f.notes[2].Content)
	a.True(f.notes[2].Primary)
	a.False(f.notes[1].Primary)
	a.Equal("keep", f.notes[1].Content)
	del := NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: 2}, DryRun: true}
	_, preview, err = noteWrite(deps, DeleteNoteToolName, "delete")(ctx, &mcp.CallToolRequest{}, del)
	r.NoError(err)
	del.DryRun = false
	del.Confirm = true
	del.PlanHash = preview.PlanHash
	_, out, err = noteWrite(deps, DeleteNoteToolName, "delete")(ctx, &mcp.CallToolRequest{}, del)
	r.NoError(err)
	a.Equal("applied", out.Items[0].Outcome)
	a.NotContains(f.notes, 2)
	a.Contains(f.notes, 1)
}

func TestNotesPlansRejectChangedStateConflictingParentsAndInvalidFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		action string
		items  []NoteMutationItem
	}{
		{"reparent", "update", []NoteMutationItem{{NoteID: 1, ModelID: 2, Title: dvgoutils.Ptr("x")}}},
		{"empty edit", "update", []NoteMutationItem{{NoteID: 1}}},
		{"sole primary false", "update", []NoteMutationItem{{NoteID: 1, Primary: dvgoutils.Ptr(false)}}},
		{"sales domain", "create", []NoteMutationItem{{ModelType: "order.salesorder", ModelID: 1, Title: dvgoutils.Ptr("x")}}},
		{"missing title", "create", []NoteMutationItem{{ModelType: "part.part", ModelID: 2}}},
		{"delete with fields", "delete", []NoteMutationItem{{NoteID: 1, Content: dvgoutils.Ptr("x")}}},
		{"same parent", "create", []NoteMutationItem{{ModelType: "part.part", ModelID: 2, Title: dvgoutils.Ptr("x")}, {ModelType: "part.part", ModelID: 2, Title: dvgoutils.Ptr("y")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			f := &fakeNotes{notes: map[int]inventree.Note{1: {PK: 1, ModelType: "part.part", ModelID: 1, Title: "First", Primary: true}}}
			deps := noteTestDeps(f)
			_, out, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", tc.action, tc.items, true, false, "")
			r.NoError(err)
			for _, item := range out.Items {
				r.Equal("failed", item.Outcome)
			}
			r.Zero(f.writes)
		})
	}
	t.Run("drift", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		f := &fakeNotes{notes: map[int]inventree.Note{1: {PK: 1, ModelType: "part.part", ModelID: 1, Title: "First", Primary: true}}}
		deps := noteTestDeps(f)
		items := []NoteMutationItem{{NoteID: 1, Content: dvgoutils.Ptr("next")}}
		_, preview, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", "update", items, true, false, "")
		r.NoError(err)
		n := f.notes[1]
		n.Content = "concurrent"
		f.notes[1] = n
		_, out, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", "update", items, false, true, preview.PlanHash)
		r.NoError(err)
		r.Equal(StatusClarificationRequired, out.Status)
		r.Zero(f.writes)
	})
}

func TestNotesBatchReportsIndependentFailuresAndAmbiguousCreation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	f := &fakeNotes{notes: map[int]inventree.Note{}}
	deps := noteTestDeps(f)
	items := []NoteMutationItem{{ModelType: "part.part", ModelID: 1, Title: dvgoutils.Ptr("Good")}, {ModelType: "invalid", ModelID: 2, Title: dvgoutils.Ptr("Bad")}}
	_, preview, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, BulkCreateNotesToolName, "create", items, true, false, "")
	r.NoError(err)
	_, out, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, BulkCreateNotesToolName, "create", items, false, true, preview.PlanHash)
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.Equal("applied", out.Items[0].Outcome)
	a.Equal("failed", out.Items[1].Outcome)
	a.Equal(1, f.writes)
	items = []NoteMutationItem{{ModelType: "part.part", ModelID: 3, Title: dvgoutils.Ptr("Loss")}}
	_, preview, err = executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", "create", items, true, false, "")
	r.NoError(err)
	f.createResponseLoss = true
	_, out, err = executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", "create", items, false, true, preview.PlanHash)
	r.NoError(err)
	a.Equal("ambiguous", out.Items[0].Outcome)
	a.Contains(out.Items[0].RecoveryPlan, "Do not retry blindly")
	a.NotContains(fmt.Sprint(out), "private response")
	_, next, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, "test", "create", items, true, false, "")
	r.NoError(err)
	a.Equal("failed", next.Items[0].Outcome)
	a.Equal(2, f.writes)
}

func TestNotesSchemasAndReadHandlers(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	searchSchema, err := noteInputSchema[SearchNotesInput]()
	r.NoError(err)
	a.Equal([]any{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"}, searchSchema.Properties["model_type"].Enum)
	for _, schemaCall := range []func() ([]byte, error){func() ([]byte, error) {
		s, err := noteInputSchema[NoteWriteInput]()
		if err != nil {
			return nil, err
		}
		return json.Marshal(s)
	}, func() ([]byte, error) {
		s, err := noteInputSchema[BulkNotesInput]()
		if err != nil {
			return nil, err
		}
		return json.Marshal(s)
	}} {
		encoded, err := schemaCall()
		r.NoError(err)
		a.Contains(string(encoded), `"enum":["part.part","company.company"`)
		a.NotContains(string(encoded), "order.salesorder")
	}
	f := &fakeNotes{notes: map[int]inventree.Note{1: {PK: 1, ModelType: "part.part", ModelID: 1, Title: "First", Primary: true}}}
	deps := noteTestDeps(f)
	_, out, err := getNote(deps)(ctx, &mcp.CallToolRequest{}, IDInput{ID: 1})
	r.NoError(err)
	a.Equal(1, out.Record.PK)
	_, missing, err := getNote(deps)(ctx, &mcp.CallToolRequest{}, IDInput{ID: 2})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
	_, page, err := searchNotes(deps)(ctx, &mcp.CallToolRequest{}, SearchNotesInput{ModelType: "part.part", ModelID: 1})
	r.NoError(err)
	a.Len(page.Results, 1)
	f.readError = true
	_, _, err = searchNotes(deps)(ctx, &mcp.CallToolRequest{}, SearchNotesInput{ModelType: "part.part"})
	r.Error(err)
}

func TestNotesNoOpAndFailedVerificationDoNotClaimSuccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	n := inventree.Note{PK: 1, ModelType: "part.part", ModelID: 1, Title: "First", Content: "same", Primary: true}
	f := &fakeNotes{notes: map[int]inventree.Note{1: n}}
	deps := noteTestDeps(f)
	items := []NoteMutationItem{{NoteID: 1, Content: dvgoutils.Ptr("same")}}
	_, preview, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, UpdateNoteToolName, "update", items, true, false, "")
	r.NoError(err)
	_, out, err := executeNotes(ctx, &mcp.CallToolRequest{}, deps, f, UpdateNoteToolName, "update", items, false, true, preview.PlanHash)
	r.NoError(err)
	a.Equal("skipped", out.Items[0].Outcome)
	a.Zero(f.writes)
	r.NotNil(out.Items[0].Record)
	plan := buildNotePlan(ctx, f, "update", []NoteMutationItem{{NoteID: 1, Content: dvgoutils.Ptr("different")}})
	adapter := &noteAdapter{client: f, action: "update"}
	r.Error(adapter.Verify(ctx, plan.Items[0]), "no write record means verification cannot succeed")
	adapter.store(0, n)
	r.Error(adapter.Verify(ctx, plan.Items[0]), "a mismatched exact read must fail verification")
	f.readError = true
	_, reason, err := adapter.Preflight(ctx, plan.Items[0])
	r.Error(err)
	a.Equal(bulkReasonReadFailed, reason)
	r.Error((&noteAdapter{client: f, action: "delete"}).Verify(ctx, plan.Items[0]), "read errors are not deletion evidence")
}

func TestNotesBatchErrorsDoNotLogPrivateUpstreamDetails(t *testing.T) {
	for _, phase := range []string{"preflight", "mutation", "verification"} {
		t.Run(phase, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			var logs bytes.Buffer
			ctx = logging.WithLogger(ctx, slog.New(slog.NewTextHandler(&logs, nil)))
			f := &fakeNotes{notes: map[int]inventree.Note{}}
			item := notePlanItem{Index: 0, Target: inventree.Note{ModelType: "part.part", ModelID: 1, Title: "title", Primary: true}, Siblings: nil}
			expected := batch.OutcomeFailed
			switch phase {
			case "preflight":
				f.readError = true
			case "mutation":
				f.createResponseLoss = true
				expected = batch.OutcomeAmbiguous
			case "verification":
				f.siblingErrorAfterWrite = true
				expected = batch.OutcomeUnverified
			}
			adapter := &noteAdapter{client: f, action: "create"}
			results, _ := batch.Execute(ctx, []notePlanItem{item}, adapter, batch.ExecuteOptions{Concurrency: 1})
			r.Len(results, 1)
			r.Equal(expected, results[0].Outcome)
			r.NotEmpty(logs.String())
			r.NotContains(logs.String(), "private-upstream-marker")
			r.NotContains(logs.String(), "simulated private response loss")
		})
	}
}
