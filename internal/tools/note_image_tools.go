package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"
)

const (
	ListNoteImagesToolName            = "list_note_images"
	DownloadNoteImageToolName         = "download_note_image"
	UploadNoteImageToolName           = "upload_note_image"
	ReplaceNoteImageToolName          = "replace_note_image"
	RemoveNoteImageToolName           = "remove_note_image"
	RenderAndEmbedNoteImageToolName   = "render_and_embed_note_image"
	RenderAndReplaceNoteImageToolName = "render_and_replace_note_image"
	maxNoteWorkflowImages             = 100
)

type NoteImageReadClient interface {
	GetNote(context.Context, int) (inventree.Note, error)
	SearchNotesPage(context.Context, inventree.NoteQuery) (inventree.Page[inventree.Note], error)
	ListNoteImagesPage(context.Context, inventree.NoteImageQuery) (inventree.Page[inventree.NoteImage], error)
	DownloadNoteImage(context.Context, int, int, int64) (inventree.DownloadedNoteImage, error)
}
type NoteImageWriteClient interface {
	NoteImageReadClient
	UploadNoteImage(context.Context, int, inventree.NoteImageUpload) (inventree.NoteImage, error)
	UpdateNote(context.Context, int, inventree.PatchFields) (inventree.Note, error)
}

type ListNoteImagesInput struct {
	NoteID int `json:"note_id" jsonschema:"Stable readable note ID on a supported parent."`
	Limit  int `json:"limit,omitempty" jsonschema:"Page size, default 20, maximum 100."`
	Offset int `json:"offset,omitempty" jsonschema:"Offset 0-5000; pages are not a snapshot."`
}
type DownloadNoteImageInput struct {
	NoteID   int   `json:"note_id"`
	ImageID  int   `json:"image_id" jsonschema:"Image ID returned by list_note_images for this note; no URL input."`
	MaxBytes int64 `json:"max_bytes,omitempty" jsonschema:"Maximum bytes, default and ceiling 5242880."`
}
type NoteImageSummary struct {
	ID     int    `json:"image_id"`
	NoteID int    `json:"note_id"`
	Date   string `json:"date,omitempty"`
}
type NoteImageListOutput struct {
	Status  string             `json:"status"`
	Count   int                `json:"count"`
	Images  []NoteImageSummary `json:"images"`
	HasMore bool               `json:"has_more"`
}
type NoteImageMutationInput struct {
	NoteID       int    `json:"note_id" jsonschema:"Existing supported note ID; this workflow never creates or promotes a note."`
	ImageID      int    `json:"image_id,omitempty" jsonschema:"Required owned image ID for replacement/removal; omit for addition."`
	InlineBase64 string `json:"inline_base64,omitempty" jsonschema:"PNG/JPEG/WebP bytes, mutually exclusive with local_path; never a URL."`
	LocalPath    string `json:"local_path,omitempty" jsonschema:"STDIO-only allowlisted local path; never allowed in HTTP mode."`
	Filename     string `json:"filename,omitempty" jsonschema:"Required for inline bytes; extension must match decoded content."`
	ContentType  string `json:"content_type,omitempty" jsonschema:"image/png, image/jpeg or image/webp, matching decoded bytes and extension."`
	Alt          string `json:"alt,omitempty" jsonschema:"Optional image alt text, at most 500 characters. Omission preserves replacement alt text; new images default to empty alt text."`
	DryRun       bool   `json:"dry_run,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
	PlanHash     string `json:"plan_hash,omitempty" jsonschema:"Principal-bound, single-use token issued by a matching dry run."`
}
type RenderNoteImageInput struct {
	RenderComponentImageInput
	Embed NoteImageTargetInput `json:"embed"`
}
type NoteImageTargetInput struct {
	NoteID   int    `json:"note_id"`
	ImageID  int    `json:"image_id,omitempty" jsonschema:"Required only by render_and_replace_note_image."`
	Alt      string `json:"alt,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
	Confirm  bool   `json:"confirm,omitempty"`
	PlanHash string `json:"plan_hash,omitempty"`
}
type NoteImageOutput struct {
	Status              string                 `json:"status"`
	Outcome             string                 `json:"outcome"`
	NoteID              int                    `json:"note_id"`
	ImageID             int                    `json:"image_id,omitempty"`
	ReplacedImageID     int                    `json:"replaced_image_id,omitempty"`
	PlanHash            string                 `json:"plan_hash,omitempty"`
	SHA256              string                 `json:"sha256,omitempty"`
	Verified            bool                   `json:"verified"`
	RecoveryPlan        string                 `json:"recovery_plan,omitempty"`
	Clarification       *ClarificationResponse `json:"clarification,omitempty"`
	LocalUploadRecovery *LocalUploadRecovery   `json:"local_upload_recovery,omitempty"`
}

type noteImageState struct {
	Note            inventree.Note
	Images          []inventree.NoteImage
	SiblingNoteIDs  []int
	ParentNoteCount int
}
type noteImagePlan struct {
	Tool, Action                       string
	State                              noteImageState
	ImageID                            int
	Alt, Filename, ContentType, SHA256 string
	Size                               int
}

func (p noteImagePlan) Digest() (string, error) { return batch.JSONDigest(p) }

func registerNoteImageReadTools(s *mcp.Server, d Dependencies) {
	addNoteTool(s, d, ListNoteImagesToolName, "List note images", "Lists one bounded page of image IDs for a supported readable note. Does not expose raw media URLs, expanded users, or an unscoped list.", listNoteImages(d))
	addNoteTool(s, d, DownloadNoteImageToolName, "Download note image", "Returns bounded raster image content selected by note_id and image_id after checking note and parent permissions; a known media URL alone never authorizes a download.", downloadNoteImage(d))
}
func registerNoteImageWriteTools(s *mcp.Server, d Dependencies) {
	if d.noteImagePlanStore == nil {
		d.noteImagePlanStore = mustBulkStore(batch.Options[noteImagePlan]{IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal, SupersedeKey: func(p noteImagePlan) string { return fmt.Sprintf("%s:%d", p.Tool, p.State.Note.PK) }})
	}
	for _, op := range []struct{ name, action string }{{UploadNoteImageToolName, "add"}, {ReplaceNoteImageToolName, "replace"}, {RemoveNoteImageToolName, "remove"}} {
		addNoteTool(s, d, op.name, op.name, "Previews or confirms one note-image operation. Repeat the matching dry run with confirm:true and plan_hash. Uses separate upload and HTML-content writes with explicit partial outcomes; never blindly retry creation. Replacement/removal can delete files even when another note references them. Only owned HTML img references are editable; unrelated note content is preserved. Single-writer use is required.", noteImageWrite(d, op.name, op.action))
	}
	for _, op := range []struct{ name, action string }{{RenderAndEmbedNoteImageToolName, "add"}, {RenderAndReplaceNoteImageToolName, "replace"}} {
		schema, err := jsonschema.For[RenderNoteImageInput](&jsonschema.ForOptions{TypeSchemas: componentRenderTypeSchemas()})
		if err != nil {
			panic(err)
		}
		tool := ToolDescriptor(op.name, op.name, componentRenderToolDescription+" Renders PNG bytes directly into the confirmed note-image workflow; does not relay base64 to the model. Use embed.dry_run first, then embed.confirm and embed.plan_hash. Replacement requires destructive permission and an owned embed.image_id. Single-writer use and explicit partial-failure recovery apply.")
		tool.InputSchema = schema
		mcp.AddTool(s, tool, GuardTool(d, op.name, renderNoteImage(d, op.name, op.action)))
	}
}

func listNoteImages(d Dependencies) mcp.ToolHandlerFor[ListNoteImagesInput, NoteImageListOutput] {
	return LookupHandler[NoteImageReadClient, ListNoteImagesInput, NoteImageListOutput](d, ListNoteImagesToolName, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteImageReadClient, in ListNoteImagesInput) (*mcp.CallToolResult, NoteImageListOutput, error) {
		if in.Limit == 0 {
			in.Limit = 20
		}
		if in.NoteID <= 0 || in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || in.Offset > 5000 {
			return nil, NoteImageListOutput{}, errors.New("note image listing requires bounded positive inputs")
		}
		p, err := c.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: in.NoteID, Limit: in.Limit, Offset: in.Offset})
		if err != nil {
			return nil, NoteImageListOutput{}, errors.New("note images could not be safely listed")
		}
		out := NoteImageListOutput{Status: StatusOK, Count: p.Count, Images: []NoteImageSummary{}, HasMore: in.Offset+len(p.Results) < p.Count}
		for _, im := range p.Results {
			if im.Note != in.NoteID {
				return nil, NoteImageListOutput{}, errors.New("note image scope mismatch")
			}
			out.Images = append(out.Images, NoteImageSummary{ID: im.PK, NoteID: im.Note, Date: im.Date})
		}
		return TextResult(StatusOK), out, nil
	})
}
func downloadNoteImage(d Dependencies) mcp.ToolHandlerFor[DownloadNoteImageInput, NoteImageOutput] {
	return LookupHandler[NoteImageReadClient, DownloadNoteImageInput, NoteImageOutput](d, DownloadNoteImageToolName, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteImageReadClient, in DownloadNoteImageInput) (*mcp.CallToolResult, NoteImageOutput, error) {
		if in.MaxBytes == 0 {
			in.MaxBytes = upload.CompanyImageMaxBytes
		}
		v, err := c.DownloadNoteImage(ctx, in.NoteID, in.ImageID, in.MaxBytes)
		if err != nil {
			return nil, NoteImageOutput{}, errors.New("note image download could not be safely completed")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: v.Content, MIMEType: v.ContentType}}}, NoteImageOutput{Status: StatusOK, Outcome: "downloaded", NoteID: in.NoteID, ImageID: in.ImageID, Verified: true}, nil
	})
}

// Complete workflow snapshots are deliberately bounded. Read-only listing can
// page further; mutation refuses a note with more than 100 owned images.
func readNoteImageState(ctx context.Context, c interface {
	GetNote(context.Context, int) (inventree.Note, error)
	ListNoteImagesPage(context.Context, inventree.NoteImageQuery) (inventree.Page[inventree.NoteImage], error)
}, id int) (noteImageState, error) {
	n, err := c.GetNote(ctx, id)
	if err != nil || n.PK != id || n.Template || !inventree.ValidNoteModelType(n.ModelType) || n.ModelID <= 0 {
		return noteImageState{}, errors.New("selected note or supported parent is not readable")
	}
	p, err := c.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: id, Limit: maxNoteWorkflowImages})
	if err != nil || p.Count != len(p.Results) || p.Count > maxNoteWorkflowImages {
		return noteImageState{}, errors.New("complete bounded note image state could not be inspected")
	}
	ims := append([]inventree.NoteImage{}, p.Results...)
	slices.SortFunc(ims, func(a, b inventree.NoteImage) int { return a.PK - b.PK })
	urls := map[string]bool{}
	for i, im := range ims {
		if im.Note != id || im.PK <= 0 || im.Image == "" || (i > 0 && ims[i-1].PK == im.PK) {
			return noteImageState{}, errors.New("note image scope mismatch")
		}
		if urls[im.Image] {
			return noteImageState{}, errors.New("duplicate note image storage reference")
		}
		urls[im.Image] = true
	}
	return noteImageState{Note: n, Images: ims}, nil
}

// Saving a sole non-primary note implicitly promotes it upstream. Inspect and
// bind that parent context rather than select a primary as an image side effect.
func readNoteImageMutationState(ctx context.Context, c NoteImageWriteClient, id int) (noteImageState, error) {
	state, err := readNoteImageState(ctx, c, id)
	if err != nil || state.Note.Primary {
		return state, err
	}
	p, err := c.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: state.Note.ModelType, ModelID: state.Note.ModelID, Limit: 2})
	if err != nil || p.Count < 2 || len(p.Results) != 2 {
		return noteImageState{}, errors.New("select a primary note explicitly before editing images on a sole non-primary note")
	}
	state.ParentNoteCount = p.Count
	for _, n := range p.Results {
		if n.PK <= 0 || n.Template || n.ModelType != state.Note.ModelType || n.ModelID != state.Note.ModelID {
			return noteImageState{}, errors.New("note primary context could not be inspected")
		}
		state.SiblingNoteIDs = append(state.SiblingNoteIDs, n.PK)
	}
	slices.Sort(state.SiblingNoteIDs)
	return state, nil
}
func noteImagesReferenced(state noteImageState, except int) bool {
	for _, im := range state.Images {
		if im.PK != except && !strings.Contains(state.Note.Content, im.Image) {
			return false
		}
	}
	return true
}

var noteMediaReferencePattern = regexp.MustCompile(`[^\s<>"'()]+`)

func parseNoteMediaReference(value string) (*url.URL, error) {
	value = strings.NewReplacer("\t", "", "\r", "", "\n", "", "\\", "/").Replace(value)
	value = strings.TrimFunc(value, func(r rune) bool { return r <= 32 })
	return url.Parse(value)
}

func canonicalNoteMediaPath(value string) string {
	u, err := parseNoteMediaReference(value)
	if err != nil {
		return ""
	}
	p := path.Clean("/" + strings.TrimLeft(u.Path, "/"))
	index := strings.Index(p, "/media/notes/")
	if index < 0 {
		return ""
	}
	return p[index:]
}

func noteMediaReferences(content string) []string {
	var refs []string
	// Decode each URL-like token separately so a literal percent elsewhere
	// in prose cannot hide an encoded native URL. Parse performs the URL
	// path unescaping; HTML entities are decoded once, as by the sanitizer.
	for _, candidate := range noteMediaReferencePattern.FindAllString(html.UnescapeString(content), -1) {
		if p := canonicalNoteMediaPath(candidate); p != "" {
			refs = append(refs, "path:"+p)
		}
	}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		for _, a := range tok.Attr {
			p := canonicalNoteMediaPath(a.Val)
			if p == "" {
				continue
			}
			refs = append(refs, "path:"+p)
			// Keep the full address for URL attributes as well, so changing
			// origin, mount, query or fragment is not a generic-note edit.
			switch a.Key {
			case "src", "href", "poster", "background", "data", "srcset":
				u, err := parseNoteMediaReference(a.Val)
				if err == nil {
					userinfo := ""
					if u.User != nil {
						userinfo = u.User.String() + "@"
					}
					refs = append(refs, "attribute:"+a.Key+":"+u.Scheme+"//"+userinfo+u.Host+path.Clean("/"+strings.TrimLeft(u.Path, "/"))+"?"+u.RawQuery+"#"+u.Fragment)
				}
			}
		}
	}
	slices.Sort(refs)
	return slices.Compact(refs)
}

func noteImageStillReferenced(content, imageURL string) bool {
	if strings.Contains(content, imageURL) {
		return true
	}
	p := canonicalNoteMediaPath(imageURL)
	return p != "" && slices.Contains(noteMediaReferences(content), "path:"+p)
}

func noteImageReferencesPreserved(state noteImageState, target string) bool {
	if !reflect.DeepEqual(noteMediaReferences(state.Note.Content), noteMediaReferences(target)) {
		return false
	}
	for _, im := range state.Images {
		if !strings.Contains(target, im.Image) {
			return false
		}
	}
	return true
}
func findNoteImage(state noteImageState, id int) (inventree.NoteImage, bool) {
	for _, im := range state.Images {
		if im.PK == id {
			return im, true
		}
	}
	return inventree.NoteImage{}, false
}

func noteImageWrite(d Dependencies, name, action string) mcp.ToolHandlerFor[NoteImageMutationInput, NoteImageOutput] {
	return LookupHandler[NoteImageWriteClient, NoteImageMutationInput, NoteImageOutput](d, name, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteImageWriteClient, in NoteImageMutationInput) (*mcp.CallToolResult, NoteImageOutput, error) {
		var source upload.ResolvedSource
		if action != "remove" {
			if (strings.TrimSpace(in.InlineBase64) == "") == (strings.TrimSpace(in.LocalPath) == "") || isHTTPURLString(in.LocalPath) {
				return noteImageClarify(in.NoteID, "Supply exactly one inline_base64 or allowlisted STDIO local_path. Notes-image URL ingestion is not supported.")
			}
			var result *mcp.CallToolResult
			var old CompanyImageOutput
			var ok bool
			var err error
			source, result, old, ok, err = resolveCompanyImageSource(ctx, d, SetCompanyImageInput{InlineBase64: in.InlineBase64, LocalPath: in.LocalPath, Filename: in.Filename, ContentType: in.ContentType})
			if err != nil {
				return noteImageClarify(in.NoteID, "Image source is invalid or inaccessible; use bounded inline bytes or an allowlisted STDIO path.")
			}
			if !ok {
				if old.LocalUploadRecovery != nil {
					return result, NoteImageOutput{Status: StatusClarificationRequired, NoteID: in.NoteID, LocalUploadRecovery: old.LocalUploadRecovery}, nil
				}
				return noteImageClarify(in.NoteID, "The image source requires valid inline bytes or a permitted STDIO path.")
			}
		} else if in.InlineBase64 != "" || in.LocalPath != "" || in.Filename != "" || in.ContentType != "" || in.Alt != "" {
			return noteImageClarify(in.NoteID, "Removal takes only note_id, image_id and confirmation fields.")
		}
		return executeNoteImage(ctx, c, d, name, action, in, source)
	})
}
func renderNoteImage(d Dependencies, name, action string) mcp.ToolHandlerFor[RenderNoteImageInput, NoteImageOutput] {
	return LookupHandler[NoteImageWriteClient, RenderNoteImageInput, NoteImageOutput](d, name, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteImageWriteClient, in RenderNoteImageInput) (*mcp.CallToolResult, NoteImageOutput, error) {
		v, err := renderFromComponentInput(in.RenderComponentImageInput)
		if err != nil {
			return noteImageClarify(in.Embed.NoteID, "The render parameters are invalid; use a supported component family and its documented fields.")
		}
		return executeNoteImage(ctx, c, d, name, action, NoteImageMutationInput{NoteID: in.Embed.NoteID, ImageID: in.Embed.ImageID, Alt: in.Embed.Alt, DryRun: in.Embed.DryRun, Confirm: in.Embed.Confirm, PlanHash: in.Embed.PlanHash}, upload.ResolvedSource{Filename: "render-" + v.SHA256 + ".png", ContentType: "image/png", Content: v.PNG})
	})
}

func noteImageClarify(id int, reason string) (*mcp.CallToolResult, NoteImageOutput, error) {
	v := NewClarification("Which valid, current note-image plan should be used?", "note_image_plan", reason, "dry_run", true, nil, map[string]any{"note_id": id})
	return TextResult(StatusClarificationRequired), NoteImageOutput{Status: StatusClarificationRequired, NoteID: id, Clarification: &v}, nil
}
func noteImagePartial(out NoteImageOutput, outcome, reason string) (*mcp.CallToolResult, NoteImageOutput, error) {
	out.Status = StatusPartialFailure
	out.Outcome = outcome
	out.RecoveryPlan = reason + " Read the exact note and list_note_images before another write; do not blindly repeat upload or use a metadata save as rollback."
	return TextResult(out.Status), out, nil
}

func executeNoteImage(ctx context.Context, c NoteImageWriteClient, d Dependencies, name, action string, in NoteImageMutationInput, source upload.ResolvedSource) (*mcp.CallToolResult, NoteImageOutput, error) {
	if in.NoteID <= 0 || utf8.RuneCountInString(in.Alt) > 500 || (action == "add" && in.ImageID != 0) || (action != "add" && in.ImageID <= 0) {
		return noteImageClarify(in.NoteID, "Use a positive note_id, an owned image_id only for replacement/removal, and alt text of at most 500 characters.")
	}
	state, err := readNoteImageMutationState(ctx, c, in.NoteID)
	if err != nil {
		return noteImageClarify(in.NoteID, "A complete supported readable note with at most 100 images is required. A sole non-primary note needs an explicit primary selection through update_note first.")
	}
	if !noteImagesReferenced(state, 0) {
		return noteImageClarify(in.NoteID, "An unreferenced upload is pending on this note. Reconcile it before any note save, which would delete its file.")
	}
	if action != "remove" && len(state.Images) >= maxNoteWorkflowImages {
		return noteImageClarify(in.NoteID, "Upload-based operations need a free temporary image slot. At 100 images, remove an image first or use the web editor.")
	}
	plan := noteImagePlan{Tool: name, Action: action, State: state, ImageID: in.ImageID, Alt: in.Alt}
	var selected inventree.NoteImage
	if action != "add" {
		var ok bool
		selected, ok = findNoteImage(state, in.ImageID)
		if !ok {
			return noteImageClarify(in.NoteID, "image_id must belong to this note.")
		}
		if _, err = editNoteImageHTML(state.Note.Content, selected.Image, "", in.Alt); err != nil {
			return noteImageClarify(in.NoteID, "The selected image must be referenced only by editable HTML img elements. Other URL references must be reconciled in the web editor.")
		}
	}
	if action != "remove" {
		if _, err = upload.ValidateRasterImage(source); err != nil || int64(len(source.Content)) > companyImageMaxBytes(&d) {
			return noteImageClarify(in.NoteID, "The image must satisfy the bounded PNG/JPEG/WebP raster policy.")
		}
		sum := sha256.Sum256(source.Content)
		plan.SHA256 = hex.EncodeToString(sum[:])
		plan.Filename = source.Filename
		plan.ContentType = source.ContentType
		plan.Size = len(source.Content)
		// The storage name is unknown until POST. Reserve space for the
		// client's maximum URL length before writing, rather than leave an
		// upload behind when the note has insufficient content capacity.
		preview := state.Note.Content + `<p><img src="` + strings.Repeat("x", 2048) + `" alt="` + escapeNoteImageAttribute(in.Alt) + `"></p>`
		if action == "replace" {
			preview, err = editNoteImageHTML(state.Note.Content, selected.Image, strings.Repeat("x", 2048), in.Alt)
		}
		if err != nil || utf8.RuneCountInString(preview) > 50000 {
			return noteImageClarify(in.NoteID, "The note needs enough content capacity for a bounded image URL; shorten the content before uploading.")
		}
	}
	if d.noteImagePlanStore == nil {
		return nil, NoteImageOutput{}, errors.New("note-image plan store unavailable")
	}
	out := NoteImageOutput{Status: StatusOK, Outcome: "planned", NoteID: in.NoteID, SHA256: plan.SHA256}
	if action == "remove" {
		out.ImageID = in.ImageID
	}
	if action == "replace" {
		out.ReplacedImageID = in.ImageID
	}
	if in.DryRun {
		out.PlanHash, err = d.noteImagePlanStore.Issue(ctx, plan)
		if err != nil {
			return noteImageClarify(in.NoteID, "No confirmation plan is available; retry the preview later.")
		}
		return TextResult(StatusOK), out, nil
	}
	if !in.Confirm || in.PlanHash == "" || !d.noteImagePlanStore.Consume(ctx, in.PlanHash, plan) {
		return noteImageClarify(in.NoteID, "A matching dry run, confirm:true and unused plan_hash are required. State or source changes require a new preview.")
	}
	current, err := readNoteImageMutationState(ctx, c, in.NoteID)
	if err != nil || !reflect.DeepEqual(current, state) {
		return noteImageClarify(in.NoteID, "The note or image set changed before mutation. Prepare a fresh plan.")
	}
	content := state.Note.Content
	if action != "remove" {
		im, uploadErr := c.UploadNoteImage(ctx, in.NoteID, inventree.NoteImageUpload{Filename: source.Filename, ContentType: source.ContentType, Content: source.Content})
		if uploadErr != nil {
			if !ambiguousAdminMutation(uploadErr) {
				out.Status = StatusValidationFailed
				out.Outcome = "failed"
				out.RecoveryPlan = "Upload was rejected; no content PATCH was attempted. Check upstream change permission and image validation before preparing a new plan."
				return TextResult(out.Status), out, nil
			}
			return noteImagePartial(out, "outcome_unknown", "Upload outcome is unknown; no content PATCH was attempted.")
		}
		out.ImageID = im.PK
		current, err = readNoteImageMutationState(ctx, c, in.NoteID)
		if err != nil {
			return noteImagePartial(out, "uploaded_but_not_embedded", "The upload succeeded, but the note/image state could not be re-read. No PATCH was attempted.")
		}
		stored, exists := findNoteImage(current, im.PK)
		if !exists {
			return noteImagePartial(out, "vanished_upload", "The uploaded image disappeared before embedding. No PATCH was attempted.")
		}
		expected := append(append([]inventree.NoteImage{}, state.Images...), im)
		slices.SortFunc(expected, func(a, b inventree.NoteImage) int { return a.PK - b.PK })
		if !reflect.DeepEqual(current.Note, state.Note) || !reflect.DeepEqual(current.Images, expected) || !noteImagesReferenced(current, im.PK) || !reflect.DeepEqual(stored, im) {
			return noteImagePartial(out, "uploaded_but_not_embedded", "The note or image set changed after upload. No PATCH was attempted.")
		}
		download, readErr := c.DownloadNoteImage(ctx, in.NoteID, im.PK, companyImageMaxBytes(&d))
		if readErr != nil || !bytes.Equal(download.Content, source.Content) {
			return noteImagePartial(out, "failed_verification", "Uploaded bytes could not be verified. No PATCH was attempted.")
		}
		if action == "add" {
			content += `<p><img src="` + escapeNoteImageAttribute(im.Image) + `" alt="` + escapeNoteImageAttribute(in.Alt) + `"></p>`
		} else {
			content, err = editNoteImageHTML(content, selected.Image, im.Image, in.Alt)
			out.ReplacedImageID = in.ImageID
		}
	} else {
		content, err = editNoteImageHTML(content, selected.Image, "", "")
	}
	if err != nil || utf8.RuneCountInString(content) > 50000 {
		if action != "remove" {
			return noteImagePartial(out, "uploaded_but_not_embedded", "The resulting note content exceeds its bounds or cannot preserve existing content. No PATCH was attempted.")
		}
		return noteImageClarify(in.NoteID, "The requested content change cannot be safely constructed.")
	}
	// Recheck after the potentially slow media verification, before the PATCH.
	beforePatch, readErr := readNoteImageMutationState(ctx, c, in.NoteID)
	if readErr != nil || !reflect.DeepEqual(beforePatch, current) {
		if action == "remove" {
			return noteImageClarify(in.NoteID, "The note or image set changed before removal; prepare a fresh plan.")
		}
		return noteImagePartial(out, "uploaded_but_not_embedded", "The note or image set changed before PATCH. No PATCH was attempted.")
	}
	_, mutationErr := c.UpdateNote(ctx, in.NoteID, inventree.PatchFields{"content": inventree.Set(content)})
	after, readErr := readNoteImageMutationState(ctx, c, in.NoteID)
	target := state.Note
	target.Content = content
	wanted := append([]inventree.NoteImage{}, current.Images...)
	if action != "add" {
		wanted = slices.DeleteFunc(wanted, func(im inventree.NoteImage) bool { return im.PK == in.ImageID })
	}
	if readErr == nil && noteTargetMatches(after.Note, target) && reflect.DeepEqual(after.Images, wanted) {
		out.Verified = true
		if action == "remove" {
			out.Outcome = "verified_removed"
		} else {
			out.Outcome = "verified_embedded"
		}
		return TextResult(StatusOK), out, nil
	}
	if mutationErr != nil {
		if !ambiguousAdminMutation(mutationErr) && readErr == nil && reflect.DeepEqual(after, current) {
			if action == "remove" {
				out.Status = StatusValidationFailed
				out.Outcome = "failed"
				out.RecoveryPlan = "Removal was rejected and read-back still matches the previewed state. Check upstream change permission before preparing a fresh plan."
				return TextResult(out.Status), out, nil
			}
			return noteImagePartial(out, "uploaded_but_not_embedded", "The content PATCH was rejected; the uploaded file is still pending.")
		}
		return noteImagePartial(out, "outcome_unknown", "The content-write outcome could not be proved from read-back.")
	}
	return noteImagePartial(out, "failed_verification", "The content write returned, but expected note content and image state could not be verified.")
}

func escapeNoteImageAttribute(s string) string {
	r := strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// Tokenization changes only selected img elements, preserving raw bytes of
// every unrelated token. Refuse non-img references rather than alter prose,
// links, Markdown, or URLs belonging to another note.
func editNoteImageHTML(content, oldURL, newURL, alt string) (string, error) {
	z := html.NewTokenizer(strings.NewReader(content))
	var out strings.Builder
	found := false
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return "", errors.New("invalid note HTML")
			}
			break
		}
		raw := string(z.Raw())
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			tok := z.Token()
			if tok.Data == "img" {
				match := false
				for _, a := range tok.Attr {
					if a.Key == "src" && a.Val == oldURL {
						match = true
					}
				}
				if match {
					found = true
					if newURL == "" {
						continue
					}
					out.WriteString("<img")
					hasAlt := false
					for _, a := range tok.Attr {
						if a.Key == "src" {
							a.Val = newURL
						}
						if a.Key == "alt" {
							hasAlt = true
							if alt != "" {
								a.Val = alt
							}
						}
						fmt.Fprintf(&out, ` %s="%s"`, a.Key, escapeNoteImageAttribute(a.Val))
					}
					if !hasAlt {
						fmt.Fprintf(&out, ` alt="%s"`, escapeNoteImageAttribute(alt))
					}
					out.WriteString(">")
					continue
				}
			}
		}
		out.WriteString(raw)
	}
	result := out.String()
	if !found || noteImageStillReferenced(result, oldURL) {
		return "", errors.New("image has unsupported references")
	}
	return result, nil
}
