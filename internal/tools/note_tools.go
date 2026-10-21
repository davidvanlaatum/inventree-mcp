package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	SearchNotesToolName     = "search_notes"
	GetNoteToolName         = "get_note"
	CreateNoteToolName      = "create_note"
	UpdateNoteToolName      = "update_note"
	DeleteNoteToolName      = "delete_note"
	BulkCreateNotesToolName = "bulk_create_notes"
	BulkUpdateNotesToolName = "bulk_update_notes"
)

type NoteReadClient interface {
	SearchNotesPage(context.Context, inventree.NoteQuery) (inventree.Page[inventree.Note], error)
	GetNote(context.Context, int) (inventree.Note, error)
}
type NoteWriteClient interface {
	NoteReadClient
	CreateNote(context.Context, inventree.NoteCreate) (inventree.Note, error)
	UpdateNote(context.Context, int, inventree.PatchFields) (inventree.Note, error)
	DeleteNote(context.Context, int) error
}

type noteModelType string

type SearchNotesInput struct {
	ModelType noteModelType `json:"model_type" jsonschema:"Notes endpoint model type: part.part, company.company, company.supplierpart, company.manufacturerpart, stock.stockitem, or order.purchaseorder."`
	ModelID   int           `json:"model_id,omitempty" jsonschema:"Optional exact parent object ID."`
	Search    string        `json:"search,omitempty" jsonschema:"Optional text matched against note title, description, and content."`
	Limit     int           `json:"limit,omitempty" jsonschema:"Page size, default 20, maximum 100."`
	Offset    int           `json:"offset,omitempty" jsonschema:"Offset from 0 through 5000; pages are not a snapshot."`
}
type NoteSearchOutput struct {
	Status  string           `json:"status"`
	Count   int              `json:"count"`
	Results []inventree.Note `json:"results"`
	HasMore bool             `json:"has_more"`
}

// NoteMutationItem is shared by the single-record and batch tools. No input
// permits reparenting an existing note or changing its template status.
type NoteMutationItem struct {
	NoteID         int           `json:"note_id,omitempty" jsonschema:"Required stable note ID for editing or deletion; omitted on creation."`
	ModelType      noteModelType `json:"model_type,omitempty" jsonschema:"Required on creation: part.part, company.company, company.supplierpart, company.manufacturerpart, stock.stockitem, or order.purchaseorder."`
	ModelID        int           `json:"model_id,omitempty" jsonschema:"Required existing parent ID on creation; existing notes cannot be reparented."`
	Title          *string       `json:"title,omitempty" jsonschema:"Required non-empty title on creation, at most 100 characters."`
	Description    *string       `json:"description,omitempty" jsonschema:"Optional replacement description up to 250 characters; empty string explicitly clears it."`
	Content        *string       `json:"content,omitempty" jsonschema:"Note text/Markdown up to 50000 characters; empty string explicitly clears it. Templates and image uploads are excluded."`
	Primary        *bool         `json:"primary,omitempty" jsonschema:"Optional primary selection. The first note is automatically primary upstream. Promotion demotes an existing primary and requires a reviewed plan."`
	AllowDuplicate bool          `json:"allow_duplicate,omitempty" jsonschema:"Explicitly allow creation with a title already present on this parent; default false. Inspect existing notes before retrying an ambiguous creation."`
}
type NoteWriteInput struct {
	NoteMutationItem
	DryRun   bool   `json:"dry_run,omitempty" jsonschema:"Prepare a current-state-bound preview before writing."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Execute only with true and the matching plan_hash."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Principal-bound single-use token from a matching dry run."`
}
type BulkNotesInput struct {
	Items    []NoteMutationItem `json:"items" jsonschema:"Independent note creations or edits; at most the configured bulk limit. Each parent may occur only once per batch."`
	DryRun   bool               `json:"dry_run,omitempty"`
	Confirm  bool               `json:"confirm,omitempty"`
	PlanHash string             `json:"plan_hash,omitempty"`
}
type NoteMutationResult struct {
	ItemIndex    int             `json:"item_index"`
	NoteID       int             `json:"note_id,omitempty"`
	Outcome      string          `json:"outcome"`
	Message      string          `json:"message,omitempty"`
	RecoveryPlan string          `json:"recovery_plan,omitempty"`
	Record       *inventree.Note `json:"record,omitempty"`
}
type NoteWriteOutput struct {
	Status        string                 `json:"status"`
	DryRun        bool                   `json:"dry_run"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Items         []NoteMutationResult   `json:"items,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	Timing        *BulkTimingEvidence    `json:"timing,omitempty"`
}
type notePlanItem struct {
	Index      int
	Input      NoteMutationItem
	Before     *inventree.Note
	Target     inventree.Note
	Siblings   []inventree.Note
	FailReason string
}
type notePlan struct {
	Action string
	Items  []notePlanItem
}

func (p notePlan) Digest() (string, error) { return batch.JSONDigest(p) }

func addNoteTool[In, Out any](server *mcp.Server, deps Dependencies, name, title, description string, handler mcp.ToolHandlerFor[In, Out]) {
	schema, err := noteInputSchema[In]()
	if err != nil {
		panic(fmt.Sprintf("build %s input schema: %v", name, err))
	}
	tool := ToolDescriptor(name, title, description)
	tool.InputSchema = schema
	mcp.AddTool(server, tool, GuardTool(deps, name, handler))
}

func noteInputSchema[In any]() (*jsonschema.Schema, error) {
	return jsonschema.For[In](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[noteModelType](): stringEnumSchema([]string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"})}})
}

func registerNoteReadTools(server *mcp.Server, deps Dependencies) {
	addNoteTool(server, deps, SearchNotesToolName, "Search notes", "Lists a bounded page of ordinary notes for one supported object type, optionally one object. Searches title, description and content; omits user expansions, templates, image administration, and pagination URLs.", searchNotes(deps))
	addNoteTool(server, deps, GetNoteToolName, "Get note", "Reads an ordinary note by stable note ID after verifying its supported readable parent.", getNote(deps))
}
func registerNoteWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.notePlanStore == nil {
		deps.notePlanStore = mustBulkStore(batch.Options[notePlan]{IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal, SupersedeKey: func(p notePlan) string {
			keys := make([]string, len(p.Items))
			for i, item := range p.Items {
				keys[i] = fmt.Sprintf("%s:%d:%d", item.Target.ModelType, item.Target.ModelID, item.Input.NoteID)
			}
			slices.Sort(keys)
			return p.Action + fmt.Sprint(keys)
		}})
	}
	for _, operation := range []struct{ name, action, title string }{{CreateNoteToolName, "create", "Create note"}, {UpdateNoteToolName, "update", "Update note"}, {DeleteNoteToolName, "delete", "Delete note"}} {
		addNoteTool(server, deps, operation.name, operation.title, "Previews or confirms one ordinary note mutation. Run dry_run:true first, then repeat with confirm:true and plan_hash. Stable note IDs identify edits/deletion; parent type/ID identifies creation. Templates, reparenting, and image upload are excluded.", noteWrite(deps, operation.name, operation.action))
	}
	addNoteTool(server, deps, BulkCreateNotesToolName, "Bulk create notes", "Previews or confirms independent note creations using the shared batch executor. One parent per batch; partial outcomes are explicit. Never retry ambiguous creation blindly.", bulkNotes(deps, BulkCreateNotesToolName, "create"))
	addNoteTool(server, deps, BulkUpdateNotesToolName, "Bulk update notes", "Previews or confirms independent note edits using stable note IDs and the shared batch executor. One parent per batch prevents conflicting primary-note changes. No bulk deletion.", bulkNotes(deps, BulkUpdateNotesToolName, "update"))
}
func searchNotes(deps Dependencies) mcp.ToolHandlerFor[SearchNotesInput, NoteSearchOutput] {
	return LookupHandler[NoteReadClient, SearchNotesInput, NoteSearchOutput](deps, SearchNotesToolName, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteReadClient, input SearchNotesInput) (*mcp.CallToolResult, NoteSearchOutput, error) {
		page, err := c.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: string(input.ModelType), ModelID: input.ModelID, Search: input.Search, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
		if err != nil {
			return nil, NoteSearchOutput{}, err
		}
		return TextResult(StatusOK), NoteSearchOutput{Status: StatusOK, Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, nil
	})
}
func getNote(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.Note]] {
	return LookupHandler[NoteReadClient, IDInput, RecordOutput[inventree.Note]](deps, GetNoteToolName, func(ctx context.Context, _ *mcp.CallToolRequest, c NoteReadClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.Note], error) {
		n, err := c.GetNote(ctx, input.ID)
		return recordOutput(n, err)
	})
}

func noteWrite(deps Dependencies, name, action string) mcp.ToolHandlerFor[NoteWriteInput, NoteWriteOutput] {
	return LookupHandler[NoteWriteClient, NoteWriteInput, NoteWriteOutput](deps, name, func(ctx context.Context, req *mcp.CallToolRequest, c NoteWriteClient, input NoteWriteInput) (*mcp.CallToolResult, NoteWriteOutput, error) {
		return executeNotes(ctx, req, deps, c, name, action, []NoteMutationItem{input.NoteMutationItem}, input.DryRun, input.Confirm, input.PlanHash)
	})
}
func bulkNotes(deps Dependencies, name, action string) mcp.ToolHandlerFor[BulkNotesInput, NoteWriteOutput] {
	return LookupHandler[NoteWriteClient, BulkNotesInput, NoteWriteOutput](deps, name, func(ctx context.Context, req *mcp.CallToolRequest, c NoteWriteClient, input BulkNotesInput) (*mcp.CallToolResult, NoteWriteOutput, error) {
		return executeNotes(ctx, req, deps, c, name, action, input.Items, input.DryRun, input.Confirm, input.PlanHash)
	})
}

func noteSiblings(ctx context.Context, c NoteReadClient, modelType string, id int) ([]inventree.Note, error) {
	var notes []inventree.Note
	for offset := 0; offset < 1000; offset += 100 {
		page, err := c.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: modelType, ModelID: id, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		notes = append(notes, page.Results...)
		if page.Next == nil || *page.Next == "" {
			slices.SortFunc(notes, func(a, b inventree.Note) int { return a.PK - b.PK })
			return notes, nil
		}
		if len(page.Results) == 0 {
			return nil, errors.New("notes scan made no progress")
		}
	}
	return nil, errors.New("notes scan safety limit exceeded")
}
func noteFields(input NoteMutationItem) inventree.PatchFields {
	f := inventree.PatchFields{}
	if input.Title != nil {
		f["title"] = inventree.Set(*input.Title)
	}
	if input.Description != nil {
		f["description"] = inventree.Set(*input.Description)
	}
	if input.Content != nil {
		f["content"] = inventree.Set(*input.Content)
	}
	if input.Primary != nil {
		f["primary"] = inventree.Set(*input.Primary)
	}
	return f
}
func buildNotePlan(ctx context.Context, c NoteReadClient, action string, inputs []NoteMutationItem) notePlan {
	p := notePlan{Action: action, Items: make([]notePlanItem, len(inputs))}
	parents := map[string]int{}
	for i, input := range inputs {
		item := notePlanItem{Index: i, Input: input}
		p.Items[i] = item
		fail := func(reason string) { item.FailReason = reason; p.Items[i] = item }
		if action == "create" {
			if input.NoteID != 0 || !inventree.ValidNoteModelType(string(input.ModelType)) || input.ModelID <= 0 || input.Title == nil {
				fail("creation requires a supported model_type, positive model_id, and title; omit note_id")
				continue
			}
			item.Target = inventree.Note{ModelType: string(input.ModelType), ModelID: input.ModelID}
		} else {
			if input.NoteID <= 0 || input.ModelID != 0 || input.ModelType != "" || input.AllowDuplicate {
				fail("editing/deletion requires only a stable note_id and approved editable fields")
				continue
			}
			before, err := c.GetNote(ctx, input.NoteID)
			if err != nil || before.PK != input.NoteID || before.Template || !inventree.ValidNoteModelType(before.ModelType) {
				fail("note_id does not identify a supported readable note")
				continue
			}
			item.Before = &before
			item.Target = before
		}
		if action == "delete" && len(noteFields(input)) > 0 {
			fail("deletion does not accept replacement fields")
			continue
		}
		if action == "update" && len(noteFields(input)) == 0 {
			fail("at least one editable note field is required")
			continue
		}
		if input.Title != nil {
			item.Target.Title = *input.Title
		}
		if input.Description != nil {
			item.Target.Description = *input.Description
		}
		if input.Content != nil {
			item.Target.Content = *input.Content
		}
		if input.Primary != nil {
			item.Target.Primary = *input.Primary
		}
		if action != "delete" && (strings.TrimSpace(item.Target.Title) == "" || utf8.RuneCountInString(item.Target.Title) > 100 || utf8.RuneCountInString(item.Target.Description) > 250 || utf8.RuneCountInString(item.Target.Content) > 50000) {
			fail("note title/content exceeds its schema bounds or title is empty")
			continue
		}
		siblings, err := noteSiblings(ctx, c, item.Target.ModelType, item.Target.ModelID)
		if err != nil {
			fail("parent notes could not be safely inspected")
			continue
		}
		item.Siblings = siblings
		if action == "create" {
			duplicate := false
			for _, n := range siblings {
				if n.Title == item.Target.Title {
					duplicate = true
				}
			}
			if duplicate && !input.AllowDuplicate {
				fail("a note with this title already exists; select its note_id or explicitly allow_duplicate")
				continue
			}
		}
		if action != "delete" && ((action == "create" && len(siblings) == 0) || (action == "update" && len(siblings) == 1)) {
			if input.Primary != nil && !*input.Primary {
				fail("InvenTree requires the sole note to be primary")
				continue
			}
			item.Target.Primary = true
		}
		key := fmt.Sprintf("%s:%d", item.Target.ModelType, item.Target.ModelID)
		if prev, ok := parents[key]; ok {
			p.Items[prev].FailReason = "each parent may appear only once per batch"
			item.FailReason = "each parent may appear only once per batch"
		} else {
			parents[key] = i
		}
		p.Items[i] = item
	}
	return p
}

type noteAdapter struct {
	client NoteWriteClient
	action string
	bulkRecordStore[inventree.Note]
}

func (a *noteAdapter) Preflight(ctx context.Context, item notePlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	notes, err := noteSiblings(ctx, a.client, item.Target.ModelType, item.Target.ModelID)
	if err != nil {
		return false, bulkReasonReadFailed, errors.New("note preflight read failed")
	}
	if !reflect.DeepEqual(notes, item.Siblings) {
		return false, bulkReasonDrifted, errors.New(bulkReasonDrifted)
	}
	if item.Before != nil {
		current, err := a.client.GetNote(ctx, item.Before.PK)
		if err != nil {
			return false, bulkReasonReadFailed, errors.New("note preflight read failed")
		}
		if !reflect.DeepEqual(current, *item.Before) {
			return false, bulkReasonDrifted, errors.New(bulkReasonDrifted)
		}
		if a.action == "update" && noteTargetMatches(current, item.Target) {
			a.store(item.Index, current)
			return true, "already at target state", nil
		}
	}
	return false, "", nil
}
func (a *noteAdapter) Mutate(ctx context.Context, item notePlanItem) error {
	var n inventree.Note
	var err error
	switch a.action {
	case "create":
		n, err = a.client.CreateNote(ctx, inventree.NoteCreate{ModelType: item.Target.ModelType, ModelID: item.Target.ModelID, Title: item.Target.Title, Description: item.Target.Description, Content: item.Target.Content, Primary: &item.Target.Primary})
	case "update":
		n, err = a.client.UpdateNote(ctx, item.Input.NoteID, noteFields(item.Input))
	case "delete":
		err = a.client.DeleteNote(ctx, item.Input.NoteID)
	}
	if err == nil && a.action != "delete" {
		a.store(item.Index, n)
	}
	if err != nil {
		return errors.New("note mutation failed; read current state before retrying")
	}
	return nil
}
func noteTargetMatches(n, target inventree.Note) bool {
	return !n.Template && n.ModelType == target.ModelType && n.ModelID == target.ModelID && n.Title == target.Title && n.Description == target.Description && n.Content == target.Content && n.Primary == target.Primary
}
func (a *noteAdapter) Verify(ctx context.Context, item notePlanItem) error {
	if a.action == "delete" {
		_, err := a.client.GetNote(ctx, item.Input.NoteID)
		if isNotFound(err) {
			return nil
		}
		return errors.New("note deletion could not be verified")
	}
	n, ok := a.get(item.Index)
	if !ok {
		return errors.New("note write did not return a record")
	}
	n, err := a.client.GetNote(ctx, n.PK)
	if err != nil || !noteTargetMatches(n, item.Target) {
		return errors.New("note read-back differs from the reviewed target")
	}
	siblings, err := noteSiblings(ctx, a.client, n.ModelType, n.ModelID)
	if err != nil {
		return errors.New("note sibling verification failed")
	}
	for _, before := range item.Siblings {
		if before.PK == n.PK {
			continue
		}
		index := slices.IndexFunc(siblings, func(v inventree.Note) bool { return v.PK == before.PK })
		if index < 0 {
			return errors.New("a sibling note disappeared")
		}
		after := siblings[index]
		expected := before
		if n.Primary {
			expected.Primary = false
		}
		if !noteTargetMatches(after, expected) {
			return errors.New("a sibling changed outside the reviewed primary selection")
		}
	}
	a.store(item.Index, n)
	return nil
}
func executeNotes(ctx context.Context, req *mcp.CallToolRequest, deps Dependencies, c NoteWriteClient, name, action string, inputs []NoteMutationItem, dry, confirm bool, token string) (*mcp.CallToolResult, NoteWriteOutput, error) {
	out := NoteWriteOutput{Status: StatusOK, DryRun: dry}
	clarify := func(reason string) (*mcp.CallToolResult, NoteWriteOutput, error) {
		v := NewClarification("Which reviewed notes plan should be used?", "plan_hash", reason, "dry_run", true, nil, nil)
		out.Status = StatusClarificationRequired
		out.Clarification = &v
		return TextResult(out.Status), out, nil
	}
	if len(inputs) == 0 || len(inputs) > effectiveBulkMaxItems(deps) {
		return clarify("item count must be within the configured bulk limit")
	}
	if deps.notePlanStore == nil {
		return nil, out, errors.New("notes plan store unavailable")
	}
	plan := buildNotePlan(ctx, c, action, inputs)
	if dry {
		for _, item := range plan.Items {
			v := NoteMutationResult{ItemIndex: item.Index + 1, NoteID: item.Input.NoteID, Outcome: bulkOutcomePlanned, Record: &item.Target}
			if item.FailReason != "" {
				v.Outcome = bulkOutcomeFailed
				v.Message = item.FailReason
				v.Record = nil
			}
			out.Items = append(out.Items, v)
		}
		var err error
		out.PlanHash, err = deps.notePlanStore.Issue(ctx, plan)
		if errors.Is(err, batch.ErrCapacity) {
			return clarify("too many outstanding notes plans")
		}
		return TextResult(StatusOK), out, err
	}
	if !confirm || token == "" || !deps.notePlanStore.Consume(ctx, token, plan) {
		return clarify("a matching dry-run plan and confirm:true are required; plans are single-use and current-state-bound")
	}
	adapter := &noteAdapter{client: c, action: action}
	progress := newBulkProgressReporter(req, name)
	results, timing := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: effectiveBulkConcurrency(deps), OnProgress: func(done, total int) { progress.report(ctx, done, total) }})
	for _, result := range results {
		v := NoteMutationResult{ItemIndex: result.Item.Index + 1, NoteID: result.Item.Input.NoteID, Outcome: string(result.Outcome), Message: result.Message, RecoveryPlan: result.RecoveryPlan}
		if n, ok := adapter.get(result.Item.Index); ok {
			v.NoteID = n.PK
			v.Record = &n
		}
		if result.Outcome != batch.OutcomeApplied && result.Outcome != batch.OutcomeSkipped {
			out.Status = StatusPartialFailure
		}
		out.Items = append(out.Items, v)
	}
	out.Timing = bulkTimingEvidence(timing, len(plan.Items), effectiveBulkConcurrency(deps))
	return TextResult(out.Status), out, nil
}
