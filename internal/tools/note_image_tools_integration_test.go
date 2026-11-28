//go:build !no_integration_tests

package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func noteImageMCPSession(t *testing.T, ctx context.Context, deps Dependencies) (*mcp.ClientSession, func()) {
	t.Helper()
	r := require.New(t)
	ctx, cancel := context.WithCancel(ctx)
	ct, st := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	deps.EnableWriteTools = true
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "note-image-test", Version: "test"}, nil)
		Register(server, deps)
		done <- server.Run(ctx, st)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "note-image-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	r.NoError(err)
	return session, func() {
		r.NoError(session.Close())
		cancel()
		if err := <-done; err != nil {
			r.ErrorIs(err, context.Canceled)
		}
	}
}
func callNoteImageMCP(t *testing.T, ctx context.Context, s *mcp.ClientSession, name string, in any) NoteImageOutput {
	t.Helper()
	r := require.New(t)
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
	r.NoError(err)
	r.False(result.IsError)
	var out NoteImageOutput
	r.NoError(structuredContentTo(result, &out))
	return out
}
func mutateNoteImageMCP(t *testing.T, ctx context.Context, s *mcp.ClientSession, name string, in NoteImageMutationInput) NoteImageOutput {
	t.Helper()
	r := require.New(t)
	in.DryRun = true
	preview := callNoteImageMCP(t, ctx, s, name, in)
	r.Equal(StatusOK, preview.Status, "%+v", preview)
	r.NotEmpty(preview.PlanHash)
	in.DryRun = false
	in.Confirm = true
	in.PlanHash = preview.PlanHash
	out := callNoteImageMCP(t, ctx, s, name, in)
	r.Equal(StatusOK, out.Status, "%+v", out)
	r.True(out.Verified)
	return out
}

func assertNoteImageFileRemoved(t *testing.T, ctx context.Context, c *inventree.Client, mediaURL string) {
	t.Helper()
	r := require.New(t)
	req, err := c.NewRequest(ctx, http.MethodGet, mediaURL, nil, nil)
	r.NoError(err)
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(http.StatusNotFound, resp.StatusCode, "fixture storage file must be removed after the note save commits")
}

func runNoteImageToolsIntegration(t *testing.T, shared *testenv.SharedInvenTree) {
	t.Helper()
	for _, short := range []string{"part", "company", "supplierpart", "manufacturerpart", "stockitem", "purchaseorder"} {
		t.Run(short, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			f := newMilestoneToolFixture(t, shared)
			target := f.attachmentTarget(t, short)
			model := map[string]string{"part": "part.part", "company": "company.company", "supplierpart": "company.supplierpart", "manufacturerpart": "company.manufacturerpart", "stockitem": "stock.stockitem", "purchaseorder": "order.purchaseorder"}[short]
			name, err := f.run.Name("note-image-tools")
			r.NoError(err)
			note, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: model, ModelID: target.modelID, Title: name, Content: "<p>preserve &amp; keep</p>"})
			r.NoError(err)
			session, closeSession := noteImageMCPSession(t, ctx, f.deps())
			defer closeSession()
			in := NoteImageMutationInput{NoteID: note.PK, Filename: name + ".png", ContentType: "image/png", InlineBase64: base64.StdEncoding.EncodeToString(tinyPNGBytes()), Alt: `A "quoted" & useful image`}
			added := mutateNoteImageMCP(t, ctx, session, UploadNoteImageToolName, in)
			r.Positive(added.ImageID)
			owned, err := f.client.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 100})
			r.NoError(err)
			r.Len(owned.Results, 1)
			encodedURL := strings.ReplaceAll(owned.Results[0].Image, "/", "&#47;")
			create := NoteWriteInput{NoteMutationItem: NoteMutationItem{ModelType: noteModelType(model), ModelID: target.modelID, Title: dvgoutils.Ptr(name + "-copy"), Content: dvgoutils.Ptr(`<img src="` + encodedURL + `">`)}, DryRun: true}
			copyPreviewResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: CreateNoteToolName, Arguments: create})
			r.NoError(err)
			var copyPreview NoteWriteOutput
			r.NoError(structuredContentTo(copyPreviewResult, &copyPreview))
			r.Equal(bulkOutcomeFailed, copyPreview.Items[0].Outcome)
			create.DryRun = false
			create.Confirm = true
			create.PlanHash = copyPreview.PlanHash
			copyResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: CreateNoteToolName, Arguments: create})
			r.NoError(err)
			var copyOut NoteWriteOutput
			r.NoError(structuredContentTo(copyResult, &copyOut))
			r.Equal(StatusPartialFailure, copyOut.Status)
			notes, err := f.client.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: model, ModelID: target.modelID, Limit: 100})
			r.NoError(err)
			r.Equal(1, notes.Count, "encoded native image copy must not create a note")
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ListNoteImagesToolName, Arguments: ListNoteImagesInput{NoteID: note.PK}})
			r.NoError(err)
			r.False(result.IsError)
			var page NoteImageListOutput
			r.NoError(structuredContentTo(result, &page))
			r.Len(page.Images, 1)
			r.Equal(added.ImageID, page.Images[0].ID)
			payload, err := json.Marshal(page)
			r.NoError(err)
			r.NotContains(string(payload), "/media/")
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: DownloadNoteImageToolName, Arguments: DownloadNoteImageInput{NoteID: note.PK, ImageID: added.ImageID}})
			r.NoError(err)
			r.False(result.IsError)
			r.NotEmpty(result.Content)
			// Ordinary note content edits cannot remove files through the old tool.
			result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: UpdateNoteToolName, Arguments: NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: note.PK, Content: dvgoutils.Ptr("")}, DryRun: true}})
			r.NoError(err)
			var refused NoteWriteOutput
			r.NoError(structuredContentTo(result, &refused))
			r.Equal(bulkOutcomeFailed, refused.Items[0].Outcome)
			in.ImageID = added.ImageID
			in.Alt = "replacement"
			replaced := mutateNoteImageMCP(t, ctx, session, ReplaceNoteImageToolName, in)
			r.NotEqual(added.ImageID, replaced.ImageID)
			r.Equal(added.ImageID, replaced.ReplacedImageID)
			assertNoteImageFileRemoved(t, ctx, f.client, owned.Results[0].Image)
			replacementPage, err := f.client.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 100})
			r.NoError(err)
			r.Len(replacementPage.Results, 1)
			removed := mutateNoteImageMCP(t, ctx, session, RemoveNoteImageToolName, NoteImageMutationInput{NoteID: note.PK, ImageID: replaced.ImageID})
			r.Equal("verified_removed", removed.Outcome)
			assertNoteImageFileRemoved(t, ctx, f.client, replacementPage.Results[0].Image)
			exact, err := f.client.GetNote(ctx, note.PK)
			r.NoError(err)
			r.Equal("<p>preserve &amp; keep</p><p></p>", exact.Content)
			if short == "part" {
				for _, replace := range []bool{false, true} {
					tool := RenderAndEmbedNoteImageToolName
					targetID := 0
					if replace {
						tool = RenderAndReplaceNoteImageToolName
						targetID = added.ImageID
					}
					input := RenderNoteImageInput{RenderComponentImageInput: RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}, Embed: NoteImageTargetInput{NoteID: note.PK, ImageID: targetID, DryRun: true}}
					preview := callNoteImageMCP(t, ctx, session, tool, input)
					r.Equal(StatusOK, preview.Status, "%+v", preview)
					input.Embed.DryRun = false
					input.Embed.Confirm = true
					input.Embed.PlanHash = preview.PlanHash
					rendered := callNoteImageMCP(t, ctx, session, tool, input)
					r.True(rendered.Verified, "%+v", rendered)
					added = rendered
				}
			}
			r.NoError(f.client.DeleteNote(ctx, note.PK))
		})
	}
	t.Run("primary_context", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		f := newMilestoneToolFixture(t, shared)
		part := f.ensure(t, testenv.FixturePart)
		name, err := f.run.Name("image-primary")
		r.NoError(err)
		first, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: name, Content: "first"})
		r.NoError(err)
		second, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: name + "-second", Content: "second", Primary: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.False(second.Primary)
		session, closeSession := noteImageMCPSession(t, ctx, f.deps())
		defer closeSession()
		in := NoteImageMutationInput{NoteID: second.PK, Filename: name + ".png", ContentType: "image/png", InlineBase64: base64.StdEncoding.EncodeToString(tinyPNGBytes())}
		added := mutateNoteImageMCP(t, ctx, session, UploadNoteImageToolName, in)
		r.True(added.Verified)
		exact, err := f.client.GetNote(ctx, second.PK)
		r.NoError(err)
		r.False(exact.Primary)
		r.NoError(f.client.DeleteNote(ctx, first.PK))
		exact, err = f.client.GetNote(ctx, second.PK)
		r.NoError(err)
		r.False(exact.Primary, "deleting the primary can leave a sole non-primary note")
		in.DryRun = true
		preview := callNoteImageMCP(t, ctx, session, UploadNoteImageToolName, in)
		r.Equal(StatusClarificationRequired, preview.Status)
		exact, err = f.client.GetNote(ctx, second.PK)
		r.NoError(err)
		r.False(exact.Primary)
		r.NoError(f.client.DeleteNote(ctx, second.PK))
	})
}
