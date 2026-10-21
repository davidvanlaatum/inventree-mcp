//go:build !no_integration_tests

package tools

import (
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runNoteToolsIntegration(t *testing.T, shared *testenv.SharedInvenTree) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	f := newMilestoneToolFixture(t, shared)
	part := f.ensure(t, testenv.FixturePart)
	company := f.ensure(t, testenv.FixtureSupplier)
	deps := f.deps()
	deps.notePlanStore = mustBulkStore(batch.Options[notePlan]{IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal, SupersedeKey: func(p notePlan) string { return p.Action }})
	firstTitle, err := f.run.Name("part-note")
	r.NoError(err)
	secondTitle, err := f.run.Name("company-note")
	r.NoError(err)
	items := []NoteMutationItem{{ModelType: "part.part", ModelID: part.ID, Title: &firstTitle, Content: dvgoutils.Ptr("part content")}, {ModelType: "company.company", ModelID: company.ID, Title: &secondTitle, Content: dvgoutils.Ptr("company content")}}
	_, preview, err := bulkNotes(deps, BulkCreateNotesToolName, "create")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(preview.Items, 2)
	_, created, err := bulkNotes(deps, BulkCreateNotesToolName, "create")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: items, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	r.Len(created.Items, 2)
	for _, v := range created.Items {
		r.Equal("applied", v.Outcome)
		r.Positive(v.NoteID)
	}
	_, page, err := searchNotes(deps)(ctx, &mcp.CallToolRequest{}, SearchNotesInput{ModelType: "part.part", ModelID: part.ID})
	r.NoError(err)
	r.Len(page.Results, 1)
	_, exact, err := getNote(deps)(ctx, &mcp.CallToolRequest{}, IDInput{ID: created.Items[0].NoteID})
	r.NoError(err)
	a.Equal(firstTitle, exact.Record.Title)
	edits := []NoteMutationItem{{NoteID: created.Items[0].NoteID, Content: dvgoutils.Ptr("")}, {NoteID: created.Items[1].NoteID, Description: dvgoutils.Ptr("updated")}}
	_, preview, err = bulkNotes(deps, BulkUpdateNotesToolName, "update")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: edits, DryRun: true})
	r.NoError(err)
	_, updated, err := bulkNotes(deps, BulkUpdateNotesToolName, "update")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: edits, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	a.Empty(updated.Items[0].Record.Content)
	for _, v := range created.Items {
		input := NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: v.NoteID}, DryRun: true}
		_, preview, err = noteWrite(deps, DeleteNoteToolName, "delete")(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = preview.PlanHash
		_, out, err := noteWrite(deps, DeleteNoteToolName, "delete")(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal("applied", out.Items[0].Outcome)
	}
}
