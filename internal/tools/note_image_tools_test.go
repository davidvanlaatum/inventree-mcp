package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/render"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type fakeNoteImages struct {
	*fakeNotes
	imageMu sync.Mutex
	images  map[int]inventree.NoteImage
	data    map[int][]byte
	uploads int
	fault   string
}

func (f *fakeNoteImages) ListNoteImagesPage(_ context.Context, q inventree.NoteImageQuery) (inventree.Page[inventree.NoteImage], error) {
	f.imageMu.Lock()
	defer f.imageMu.Unlock()
	if f.fault == "list denied" || (f.fault == "list after upload" && f.uploads > 0) {
		return inventree.Page[inventree.NoteImage]{}, errors.New("private upstream list")
	}
	var ims []inventree.NoteImage
	for _, im := range f.images {
		if im.Note == q.NoteID {
			ims = append(ims, im)
		}
	}
	slices.SortFunc(ims, func(a, b inventree.NoteImage) int { return a.PK - b.PK })
	return inventree.Page[inventree.NoteImage]{Count: len(ims), Results: ims}, nil
}
func (f *fakeNoteImages) UploadNoteImage(_ context.Context, noteID int, in inventree.NoteImageUpload) (inventree.NoteImage, error) {
	f.imageMu.Lock()
	defer f.imageMu.Unlock()
	f.uploads++
	if f.fault == "upload denied" {
		return inventree.NoteImage{}, &inventree.APIError{StatusCode: 403}
	}
	im := inventree.NoteImage{PK: len(f.images) + 10, Note: noteID, Image: "/media/notes/new" + string(rune('a'+f.uploads)) + ".png"}
	f.images[im.PK] = im
	f.data[im.PK] = append([]byte{}, in.Content...)
	switch f.fault {
	case "lost upload":
		return inventree.NoteImage{}, errors.New("private upload response loss")
	case "vanished":
		delete(f.images, im.PK)
	case "drift after upload":
		f.mu.Lock()
		n := f.notes[noteID]
		n.Description = "external change"
		f.notes[noteID] = n
		f.mu.Unlock()
	case "other upload":
		f.images[100] = inventree.NoteImage{PK: 100, Note: noteID, Image: "/media/notes/other.png"}
	}
	return im, nil
}
func (f *fakeNoteImages) DownloadNoteImage(_ context.Context, noteID, imageID int, _ int64) (inventree.DownloadedNoteImage, error) {
	f.imageMu.Lock()
	defer f.imageMu.Unlock()
	im, ok := f.images[imageID]
	if !ok || im.Note != noteID {
		return inventree.DownloadedNoteImage{}, inventree.ErrNoteImageOutOfScope
	}
	if f.fault == "download failed" {
		return inventree.DownloadedNoteImage{}, errors.New("private download failure")
	}
	data := append([]byte{}, f.data[imageID]...)
	if f.fault == "corrupt bytes" {
		data = []byte("wrong bytes")
	}
	if f.fault == "drift on download" {
		f.images[100] = inventree.NoteImage{PK: 100, Note: noteID, Image: "/media/notes/late.png"}
	}
	return inventree.DownloadedNoteImage{Image: im, Content: data, ContentType: "image/png"}, nil
}
func (f *fakeNoteImages) UpdateNote(ctx context.Context, id int, fields inventree.PatchFields) (inventree.Note, error) {
	if f.fault == "patch denied" {
		return inventree.Note{}, &inventree.APIError{StatusCode: 400}
	}
	if f.fault == "patch lost without write" {
		return inventree.Note{}, errors.New("private connection failure")
	}
	n, err := f.fakeNotes.UpdateNote(ctx, id, fields)
	if err != nil {
		return n, err
	}
	f.imageMu.Lock()
	defer f.imageMu.Unlock()
	for key, im := range f.images {
		if im.Note == id && !strings.Contains(n.Content, im.Image) {
			delete(f.images, key)
			delete(f.data, key)
		}
	}
	if f.fault == "lost patch" {
		return inventree.Note{}, errors.New("private PATCH response loss")
	}
	if f.fault == "bad readback" {
		f.mu.Lock()
		n.Content += "unexpected"
		f.notes[id] = n
		f.mu.Unlock()
	}
	return n, nil
}

func noteImageTestSetup(t *testing.T) (context.Context, *fakeNoteImages, Dependencies, NoteImageMutationInput) {
	t.Helper()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	f := &fakeNoteImages{fakeNotes: &fakeNotes{notes: map[int]inventree.Note{1: {PK: 1, ModelType: "part.part", ModelID: 2, Title: "Note", Content: "<p>keep &amp; preserve</p>", Primary: true}}}, images: map[int]inventree.NoteImage{}, data: map[int][]byte{}}
	d := noteTestDeps(f.fakeNotes)
	d.ClientFromContext = func(context.Context) (any, error) { return f, nil }
	d.noteImagePlanStore = mustBulkStore(batch.Options[noteImagePlan]{IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal, SupersedeKey: func(p noteImagePlan) string { return p.Tool }})
	data := companyPNG(t, color.NRGBA{R: 255, A: 255})
	return ctx, f, d, NoteImageMutationInput{NoteID: 1, InlineBase64: base64.StdEncoding.EncodeToString(data), Filename: "test.png", ContentType: "image/png", Alt: `A "quoted" & useful image`}
}
func confirmNoteImage(t *testing.T, ctx context.Context, d Dependencies, name, action string, in NoteImageMutationInput) NoteImageOutput {
	t.Helper()
	r := require.New(t)
	in.DryRun = true
	_, preview, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
	r.NoError(err)
	r.Equal(StatusOK, preview.Status)
	r.NotEmpty(preview.PlanHash)
	in.DryRun = false
	in.Confirm = true
	in.PlanHash = preview.PlanHash
	_, out, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
	r.NoError(err)
	return out
}

func TestNoteImageAddReplaceRemoveAndRead(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, f, d, in := noteImageTestSetup(t)
	added := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
	r.True(added.Verified)
	r.Equal("verified_embedded", added.Outcome)
	r.Equal(1, f.uploads)
	r.Contains(f.notes[1].Content, "<p>keep &amp; preserve</p>")
	r.Contains(f.notes[1].Content, `alt="A &quot;quoted&quot; &amp; useful image"`)
	_, page, err := listNoteImages(d)(ctx, &mcp.CallToolRequest{}, ListNoteImagesInput{NoteID: 1})
	r.NoError(err)
	r.Len(page.Images, 1)
	encoded, err := json.Marshal(page)
	r.NoError(err)
	r.NotContains(string(encoded), "/media/")
	result, download, err := downloadNoteImage(d)(ctx, &mcp.CallToolRequest{}, DownloadNoteImageInput{NoteID: 1, ImageID: added.ImageID})
	r.NoError(err)
	r.True(download.Verified)
	r.Len(result.Content, 1)
	in.ImageID = added.ImageID
	in.Alt = "replacement"
	replaced := confirmNoteImage(t, ctx, d, ReplaceNoteImageToolName, "replace", in)
	r.True(replaced.Verified)
	r.Equal(added.ImageID, replaced.ReplacedImageID)
	r.NotContains(f.images, added.ImageID)
	r.Len(f.images, 1)
	r.Contains(f.notes[1].Content, `alt="replacement"`)
	removed := confirmNoteImage(t, ctx, d, RemoveNoteImageToolName, "remove", NoteImageMutationInput{NoteID: 1, ImageID: replaced.ImageID})
	r.True(removed.Verified)
	r.Equal("verified_removed", removed.Outcome)
	r.Empty(f.images)
	r.Equal("<p>keep &amp; preserve</p><p></p>", f.notes[1].Content)
}

func TestNoteImagePartialOutcomesNeverBlindlyRepeatOrPatchDrift(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fault, outcome string
		patches        int
	}{
		{"upload denied", "failed", 0}, {"lost upload", "outcome_unknown", 0}, {"vanished", "vanished_upload", 0}, {"drift after upload", "uploaded_but_not_embedded", 0}, {"other upload", "uploaded_but_not_embedded", 0}, {"list after upload", "uploaded_but_not_embedded", 0}, {"download failed", "failed_verification", 0}, {"corrupt bytes", "failed_verification", 0}, {"drift on download", "uploaded_but_not_embedded", 0}, {"patch denied", "uploaded_but_not_embedded", 0}, {"patch lost without write", "outcome_unknown", 0}, {"lost patch", "verified_embedded", 1}, {"bad readback", "failed_verification", 1},
	} {
		t.Run(tc.fault, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			f.fault = tc.fault
			out := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
			r.Equal(tc.outcome, out.Outcome)
			r.Equal(1, f.uploads)
			r.Equal(tc.patches, f.writes)
			encoded, err := json.Marshal(out)
			r.NoError(err)
			r.NotContains(string(encoded), "private")
			if out.Status == StatusPartialFailure {
				r.Contains(out.RecoveryPlan, "do not blindly repeat")
			}
			r.Equal(tc.fault == "lost patch", out.Verified)
		})
	}
}

func TestNoteImagePlansBindCurrentContentSourceImageSetAndAreSingleUse(t *testing.T) {
	t.Parallel()
	for _, drift := range []string{"note", "image", "source", "alt", "operation", "replay"} {
		t.Run(drift, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			if drift == "operation" || drift == "image" {
				im := inventree.NoteImage{PK: 5, Note: 1, Image: "/media/notes/existing.png"}
				f.images[5] = im
				n := f.notes[1]
				n.Content += `<img src="` + im.Image + `">`
				f.notes[1] = n
			}
			original := f.notes[1]
			in.DryRun = true
			_, preview, err := noteImageWrite(d, UploadNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, in)
			r.NoError(err)
			r.NotEmpty(preview.PlanHash)
			r.Zero(f.uploads)
			in.DryRun = false
			in.Confirm = true
			in.PlanHash = preview.PlanHash
			name, action := UploadNoteImageToolName, "add"
			switch drift {
			case "note":
				n := f.notes[1]
				n.Description = "external"
				f.notes[1] = n
			case "image":
				delete(f.images, 5)
			case "source":
				in.InlineBase64 = base64.StdEncoding.EncodeToString(companyPNG(t, color.NRGBA{B: 255, A: 255}))
			case "alt":
				in.Alt = "changed"
			case "operation":
				name = ReplaceNoteImageToolName
				action = "replace"
				in.ImageID = 5
			case "replay":
				_, out, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
				r.NoError(err)
				r.True(out.Verified)
				f.notes[1] = original
				f.images = map[int]inventree.NoteImage{}
				f.data = map[int][]byte{}
			}
			_, out, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
			r.NoError(err)
			r.Equal(StatusClarificationRequired, out.Status)
			if drift == "replay" {
				r.Equal(1, f.uploads)
			} else {
				r.Zero(f.uploads)
			}
		})
	}
}

func TestNoteImageValidationAndExistingNoteUpdateGuards(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing note", "template", "invalid parent", "missing source", "both sources", "URL source", "invalid base64", "bad image", "overlong alt", "missing image", "unowned image", "pending image", "too many images", "insufficient content", "list denied", "HTTP local", "render maximum"} {
		t.Run(mode, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			name, action := UploadNoteImageToolName, "add"
			in.DryRun = true
			switch mode {
			case "missing note":
				in.NoteID = 0
			case "template":
				n := f.notes[1]
				n.Template = true
				f.notes[1] = n
			case "invalid parent":
				n := f.notes[1]
				n.ModelType = "order.salesorder"
				f.notes[1] = n
			case "missing source":
				in.InlineBase64 = ""
			case "both sources":
				in.LocalPath = "/some/file.png"
			case "URL source":
				in.InlineBase64 = ""
				in.LocalPath = "https://evil.example.test/private.png"
			case "invalid base64":
				in.InlineBase64 = "%%%%"
			case "bad image":
				in.InlineBase64 = base64.StdEncoding.EncodeToString([]byte("not a PNG"))
			case "overlong alt":
				in.Alt = strings.Repeat("x", 501)
			case "missing image":
				name = ReplaceNoteImageToolName
				action = "replace"
			case "unowned image":
				name = ReplaceNoteImageToolName
				action = "replace"
				in.ImageID = 10
				f.images[10] = inventree.NoteImage{PK: 10, Note: 2, Image: "/media/notes/x.png"}
			case "pending image":
				f.images[10] = inventree.NoteImage{PK: 10, Note: 1, Image: "/media/notes/pending.png"}
			case "too many images":
				for id := 1; id <= 101; id++ {
					f.images[id] = inventree.NoteImage{PK: id, Note: 1, Image: "/media/notes/x.png"}
				}
			case "insufficient content":
				n := f.notes[1]
				n.Content = strings.Repeat("x", 49990)
				f.notes[1] = n
			case "list denied":
				f.fault = "list denied"
			case "HTTP local":
				in.InlineBase64 = ""
				in.LocalPath = "/allowed/file.png"
				d.UploadMode = upload.ModeHTTP
			case "render maximum":
				d.UploadMaxBytes = 1
				_, out, err := renderNoteImage(d, RenderAndEmbedNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, RenderNoteImageInput{RenderComponentImageInput: RenderComponentImageInput{Family: render.FamilyResistor, Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}, Embed: NoteImageTargetInput{NoteID: 1, DryRun: true}})
				r.NoError(err)
				r.Equal(StatusClarificationRequired, out.Status)
				r.Zero(f.uploads)
				return
			}
			_, out, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
			r.NoError(err)
			r.Equal(StatusClarificationRequired, out.Status)
			r.Zero(f.uploads)
			r.Zero(f.writes)
		})
	}
	t.Run("ordinary writes preserve image state", func(t *testing.T) {
		r := require.New(t)
		ctx, f, d, in := noteImageTestSetup(t)
		added := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
		r.True(added.Verified)
		for _, content := range []string{"", f.notes[1].Content + `<img src="/media/notes/another.png">`} {
			_, preview, err := noteWrite(d, UpdateNoteToolName, "update")(ctx, &mcp.CallToolRequest{}, NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: 1, Content: &content}, DryRun: true})
			r.NoError(err)
			r.Equal(bulkOutcomeFailed, preview.Items[0].Outcome)
		}
		inNote := NoteWriteInput{NoteMutationItem: NoteMutationItem{NoteID: 1, Description: dvgoutils.Ptr("safe metadata")}, DryRun: true}
		_, preview, err := noteWrite(d, UpdateNoteToolName, "update")(ctx, &mcp.CallToolRequest{}, inNote)
		r.NoError(err)
		r.Equal(bulkOutcomePlanned, preview.Items[0].Outcome)
		r.Equal([]int{added.ImageID}, preview.Items[0].ImageIDs)
		f.images[99] = inventree.NoteImage{PK: 99, Note: 1, Image: "/media/notes/pending.png"}
		inNote.DryRun = false
		inNote.Confirm = true
		inNote.PlanHash = preview.PlanHash
		_, out, err := noteWrite(d, UpdateNoteToolName, "update")(ctx, &mcp.CallToolRequest{}, inNote)
		r.NoError(err)
		r.Equal(StatusClarificationRequired, out.Status)
	})
}

func TestNoteImageLocalSourceAndServerRender(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, f, d, in := noteImageTestSetup(t)
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/allowed", 0700))
	data, err := base64.StdEncoding.DecodeString(in.InlineBase64)
	r.NoError(err)
	r.NoError(afero.WriteFile(fs, "/allowed/test.png", data, 0600))
	d.UploadFS = fs
	d.UploadMode = upload.ModeStdio
	d.UploadAllowRoots = []string{"/allowed"}
	in.InlineBase64 = ""
	in.LocalPath = "/allowed/test.png"
	added := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
	r.True(added.Verified)
	for _, replace := range []bool{false, true} {
		name, action := RenderAndEmbedNoteImageToolName, "add"
		imageID := 0
		if replace {
			name = RenderAndReplaceNoteImageToolName
			action = "replace"
			imageID = added.ImageID
		}
		input := RenderNoteImageInput{RenderComponentImageInput: RenderComponentImageInput{Family: render.FamilyResistor, Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}, Embed: NoteImageTargetInput{NoteID: 1, ImageID: imageID, DryRun: true}}
		_, preview, err := renderNoteImage(d, name, action)(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		r.NotEmpty(preview.PlanHash)
		input.Embed.DryRun = false
		input.Embed.Confirm = true
		input.Embed.PlanHash = preview.PlanHash
		_, out, err := renderNoteImage(d, name, action)(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		r.True(out.Verified)
		r.NotEmpty(f.data[out.ImageID])
		encoded, err := json.Marshal(out)
		r.NoError(err)
		r.NotContains(string(encoded), "base64")
	}
}

func TestNoteImageHTMLPreservationAndUnsupportedReferences(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	before := `<p>keep</p><!--comment--><img src="/media/notes/old.png" style="width: 80px" alt="old"><p>middle</p><img src='/media/notes/old.png'><img src="/media/notes/other.png">`
	got, err := editNoteImageHTML(before, "/media/notes/old.png", "/media/notes/new.png", "")
	r.NoError(err)
	r.Contains(got, `style="width: 80px" alt="old"`)
	r.Contains(got, `<!--comment-->`)
	r.Contains(got, `<p>middle</p>`)
	r.Contains(got, `<img src="/media/notes/other.png">`)
	r.Equal(2, strings.Count(got, "/media/notes/new.png"))
	for _, content := range []string{`![image](/media/notes/old.png)`, `<a href="/media/notes/old.png">link</a>`, before + "/media/notes/old.png", `<p>no image</p>`} {
		_, err = editNoteImageHTML(content, "/media/notes/old.png", "", "")
		r.Error(err)
	}
	for _, ref := range []string{`<a href="/media/notes/old%2Epng">keep</a>`, `<a href="&#47;media&#47;notes&#47;old.png">keep</a>`, `![old](%2Fmedia%2Fnotes%2Fold%2Epng)`, `100% prose ![old](/media/notes/%6Fld.png)`, `<a href="/me&#x09;dia/notes/old.png">keep</a>`, `<a href="\media\notes\old.png">keep</a>`} {
		for _, replacement := range []string{"", "/media/notes/new.png"} {
			_, err = editNoteImageHTML(`<img src="/media/notes/old.png">`+ref, "/media/notes/old.png", replacement, "")
			r.Error(err, "encoded leftover reference must prevent deletion: %s", ref)
		}
	}
}

func TestNoteImageCapacityIsCheckedBeforeAnyUpload(t *testing.T) {
	t.Parallel()
	t.Run("last available slot", func(t *testing.T) {
		r := require.New(t)
		ctx, f, d, in := noteImageTestSetup(t)
		var content strings.Builder
		for id := 1; id < 100; id++ {
			im := inventree.NoteImage{PK: id, Note: 1, Image: fmt.Sprintf("/media/notes/%d.png", id)}
			f.images[id] = im
			fmt.Fprintf(&content, `<img src="%s">`, im.Image)
		}
		n := f.notes[1]
		n.Content = content.String()
		f.notes[1] = n
		out := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
		r.True(out.Verified)
		r.Len(f.images, 100)
	})
	for _, action := range []string{"add", "replace", "remove"} {
		t.Run(action, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			var content strings.Builder
			for id := 1; id <= 100; id++ {
				im := inventree.NoteImage{PK: id, Note: 1, Image: fmt.Sprintf("/media/notes/%d.png", id)}
				f.images[id] = im
				fmt.Fprintf(&content, `<img src="%s">`, im.Image)
			}
			n := f.notes[1]
			n.Content = content.String()
			f.notes[1] = n
			name := UploadNoteImageToolName
			if action == "replace" {
				name = ReplaceNoteImageToolName
				in.ImageID = 1
			}
			if action == "remove" {
				name = RemoveNoteImageToolName
				in = NoteImageMutationInput{NoteID: 1, ImageID: 1}
			}
			in.DryRun = true
			_, out, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
			r.NoError(err)
			r.Zero(f.uploads)
			if action == "remove" {
				r.Equal(StatusOK, out.Status)
				in.DryRun = false
				in.Confirm = true
				in.PlanHash = out.PlanHash
				_, removed, err := noteImageWrite(d, name, action)(ctx, &mcp.CallToolRequest{}, in)
				r.NoError(err)
				r.True(removed.Verified)
				r.Len(f.images, 99)
			} else {
				r.Equal(StatusClarificationRequired, out.Status)
				r.Empty(out.PlanHash)
			}
		})
	}
}

func TestNoteImageMutationNeverImplicitlySelectsPrimary(t *testing.T) {
	t.Parallel()
	for _, withSibling := range []bool{false, true} {
		t.Run(fmt.Sprint(withSibling), func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			n := f.notes[1]
			n.Primary = false
			f.notes[1] = n
			if withSibling {
				f.notes[2] = inventree.Note{PK: 2, ModelType: n.ModelType, ModelID: n.ModelID, Title: "Primary", Content: "peer", Primary: true}
				out := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
				r.True(out.Verified)
				r.False(f.notes[1].Primary)
				r.True(f.notes[2].Primary)
			} else {
				in.DryRun = true
				_, out, err := noteImageWrite(d, UploadNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, in)
				r.NoError(err)
				r.Equal(StatusClarificationRequired, out.Status)
				r.Zero(f.uploads)
				// The ordinary metadata tool remains the explicit selection path.
				plan := buildNotePlan(ctx, f, "update", []NoteMutationItem{{NoteID: 1, Primary: dvgoutils.Ptr(true)}})
				r.Empty(plan.Items[0].FailReason)
			}
		})
	}
}

func TestGenericNotesRejectDecodedNativeImageReferences(t *testing.T) {
	t.Parallel()
	for _, content := range []string{`<img src="&#47;media&#47;notes&#47;other.png">`, `<img src="%2Fmedia%2Fnotes%2Fother.png">`, `<img src="/medi%61/notes/other.png">`, `100% text <img src="&#x2f;media&#x2f;notes&#x2f;other.png">`, `<img src=media/notes/other.png>`, `<img src="/me&#x09;dia/notes/other.png">`, `<img src="\media\notes\other.png">`} {
		t.Run(content, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, _ := noteImageTestSetup(t)
			create := buildNotePlan(ctx, f, "create", []NoteMutationItem{{ModelType: "part.part", ModelID: 2, Title: dvgoutils.Ptr("new"), Content: &content}})
			r.NotEmpty(create.Items[0].FailReason)
			update := NoteMutationItem{NoteID: 1, Content: &content}
			plan := buildNotePlan(ctx, f, "update", []NoteMutationItem{update})
			r.NotEmpty(plan.Items[0].FailReason)
			_, preview, err := bulkNotes(d, BulkUpdateNotesToolName, "update")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: []NoteMutationItem{update}, DryRun: true})
			r.NoError(err)
			r.Equal(bulkOutcomeFailed, preview.Items[0].Outcome)
			_, out, err := bulkNotes(d, BulkUpdateNotesToolName, "update")(ctx, &mcp.CallToolRequest{}, BulkNotesInput{Items: []NoteMutationItem{update}, Confirm: true, PlanHash: preview.PlanHash})
			r.NoError(err)
			r.Equal(StatusPartialFailure, out.Status)
			r.Zero(f.writes)
		})
	}
	r := require.New(t)
	state := noteImageState{Note: inventree.Note{Content: `<img src="/media/notes/old.png">`}, Images: []inventree.NoteImage{{Image: "/media/notes/old.png"}}}
	r.False(noteImageReferencesPreserved(state, `<img src="https://other.example.test/media/notes/old.png">`))
}

func TestNoteImageDestructiveFailureRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fault, outcome string
		verified       bool
	}{{"patch denied", "failed", false}, {"patch lost without write", "outcome_unknown", false}, {"lost patch", "verified_removed", true}, {"bad readback", "failed_verification", false}} {
		t.Run("remove "+tc.fault, func(t *testing.T) {
			r := require.New(t)
			ctx, f, d, in := noteImageTestSetup(t)
			added := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
			f.uploads = 0
			f.writes = 0
			f.fault = tc.fault
			out := confirmNoteImage(t, ctx, d, RemoveNoteImageToolName, "remove", NoteImageMutationInput{NoteID: 1, ImageID: added.ImageID})
			r.Equal(tc.outcome, out.Outcome)
			r.Equal(tc.verified, out.Verified)
			r.Zero(f.uploads)
			if tc.fault == "patch denied" || tc.fault == "patch lost without write" {
				r.Contains(f.images, added.ImageID)
				r.Contains(f.notes[1].Content, f.images[added.ImageID].Image)
				r.Zero(f.writes)
			}
		})
	}
	t.Run("replacement rejected after upload", func(t *testing.T) {
		r := require.New(t)
		ctx, f, d, in := noteImageTestSetup(t)
		added := confirmNoteImage(t, ctx, d, UploadNoteImageToolName, "add", in)
		oldURL := f.images[added.ImageID].Image
		before := f.notes[1]
		f.fault = "patch denied"
		in.ImageID = added.ImageID
		out := confirmNoteImage(t, ctx, d, ReplaceNoteImageToolName, "replace", in)
		r.Equal("uploaded_but_not_embedded", out.Outcome)
		r.Equal(added.ImageID, out.ReplacedImageID)
		r.NotEqual(added.ImageID, out.ImageID)
		r.Equal(before, f.notes[1])
		r.Contains(f.notes[1].Content, oldURL)
		r.NotContains(f.notes[1].Content, f.images[out.ImageID].Image)
		r.Len(f.images, 2)
	})
}

func TestNoteImagePlanCannotBeConsumedByAnotherOAuthPrincipal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, f, d, in := noteImageTestSetup(t)
	asPrincipal := func(principal string, fn func(context.Context)) {
		h := auth.RequireBearerToken(func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: principal}, nil
		}, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) { fn(req.Context()) }))
		req := httptest.NewRequest(http.MethodPost, "https://mcp.example.test/mcp", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer test")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		r.Equal(http.StatusOK, rec.Code)
	}
	var token string
	asPrincipal("operator-a", func(ctx context.Context) {
		in.DryRun = true
		_, out, err := noteImageWrite(d, UploadNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, in)
		r.NoError(err)
		token = out.PlanHash
		r.NotEmpty(token)
	})
	in.DryRun = false
	in.Confirm = true
	in.PlanHash = token
	asPrincipal("operator-b", func(ctx context.Context) {
		_, out, err := noteImageWrite(d, UploadNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, in)
		r.NoError(err)
		r.Equal(StatusClarificationRequired, out.Status)
		r.Zero(f.uploads)
	})
	asPrincipal("operator-a", func(ctx context.Context) {
		_, out, err := noteImageWrite(d, UploadNoteImageToolName, "add")(ctx, &mcp.CallToolRequest{}, in)
		r.NoError(err)
		r.True(out.Verified)
		r.Equal(1, f.uploads)
	})
}

func TestNoteImageOAuthGuardsBeforeReadingSourcesOrClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	for _, name := range []string{UploadNoteImageToolName, ReplaceNoteImageToolName, RemoveNoteImageToolName, RenderAndEmbedNoteImageToolName, RenderAndReplaceNoteImageToolName} {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		d := Dependencies{AuthorizationMode: AuthorizationModeOAuth, ClientFromContext: func(context.Context) (any, error) { t.Fatal("denied scope reached client"); return nil, nil }}
		result, _, err := GuardTool(d, name, noteImageWrite(d, name, "add"))(ctx, &mcp.CallToolRequest{}, NoteImageMutationInput{LocalPath: "/private/operator-data.png"})
		r.NoError(err)
		r.True(result.IsError)
	}
	ctx, _, _ := testhandler.SetupTestHandler(t)
	called := false
	handler := auth.RequireBearerToken(func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{Scopes: []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeUpload}}, nil
	}, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		called = true
		for _, name := range []string{ReplaceNoteImageToolName, RemoveNoteImageToolName, RenderAndReplaceNoteImageToolName} {
			result, denied := authorizeTool(req.Context(), "", name)
			r.True(denied)
			r.True(result.IsError)
		}
		_, denied := authorizeTool(req.Context(), "", UploadNoteImageToolName)
		r.False(denied)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://mcp.example.test/mcp", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	r.True(called)
}
