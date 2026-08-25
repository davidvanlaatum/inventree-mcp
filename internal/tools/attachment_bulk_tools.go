package tools

import (
	"context"
	"errors"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// attachment_bulk_tools.go adds F-S80's first bulk tool built on
// internal/batch (F-S76): bounded, independent-item attachment metadata
// updates across existing attachments. It reuses update_attachment_metadata's
// own field allowlist, model-type scope, and AttachmentMetadata read-back
// projection unchanged, following catalog_bulk_tools.go's (F-S77) shape.
//
// Uploads, downloads, link creation, deletion, and primary-image assignment
// remain out of scope; use the existing single-item attachment tools for
// those.

type attachmentBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.Attachment
	Fields inventree.PatchFields
}

type attachmentBulkPlan struct {
	Items []attachmentBulkPlanItem
}

func (p attachmentBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p attachmentBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

type BulkUpdateAttachmentItem struct {
	ID       int      `json:"id" jsonschema:"Stable attachment primary key."`
	Filename *string  `json:"filename,omitempty" jsonschema:"Optional replacement filename."`
	Comment  *string  `json:"comment,omitempty" jsonschema:"Optional replacement comment."`
	Tags     []string `json:"tags,omitempty" jsonschema:"Optional replacement tags."`
}

type BulkUpdateAttachmentsInput struct {
	Items    []BulkUpdateAttachmentItem `json:"items" jsonschema:"Ordered batch of independent attachment metadata updates, 1 to 25 items."`
	DryRun   bool                       `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                       `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                     `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

func registerAttachmentBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.attachmentBulkPlanStore == nil {
		deps.attachmentBulkPlanStore = mustBulkStore(batch.Options[attachmentBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p attachmentBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	addWriteTool(server, deps, BulkUpdateAttachmentsToolName, "Bulk update attachment metadata", "Plans or confirms a bounded independent-item metadata update batch across existing attachments, using update_attachment_metadata's own field allowlist. Uploads, downloads, links, deletes, and primary-image assignment are not supported here.", bulkUpdateAttachments(deps))
}

func buildAttachmentBulkPlan(ctx context.Context, client AttachmentWriteClient, items []BulkUpdateAttachmentItem) attachmentBulkPlan {
	plan := attachmentBulkPlan{Items: make([]attachmentBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildAttachmentBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildAttachmentBulkPlanItem(ctx context.Context, client AttachmentWriteClient, item BulkUpdateAttachmentItem) attachmentBulkPlanItem {
	if item.ID <= 0 {
		return attachmentBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetAttachmentMetadata(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return attachmentBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable attachment"}}
	}
	if err := validateAttachmentModelType(before.ModelType); err != nil {
		return attachmentBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	fields := attachmentMetadataFields(item.Filename, item.Comment, item.Tags)
	if len(fields) == 0 {
		return attachmentBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	return attachmentBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type attachmentBulkAdapter struct {
	client AttachmentWriteClient
	bulkRecordStore[AttachmentMetadata]
}

func (a *attachmentBulkAdapter) Preflight(ctx context.Context, item attachmentBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetAttachmentMetadata(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("attachment identity verification failed")
	}
	if patchMatches(item.Fields, attachmentValues(current)) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, attachmentValues(current), attachmentValues(item.Before)) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *attachmentBulkAdapter) Mutate(ctx context.Context, item attachmentBulkPlanItem) error {
	if _, err := a.client.UpdateAttachmentMetadata(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetAttachmentMetadata(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !patchMatches(item.Fields, attachmentValues(current)) {
			return err
		}
	}
	return nil
}

func (a *attachmentBulkAdapter) Verify(ctx context.Context, item attachmentBulkPlanItem) error {
	current, err := a.client.GetAttachmentMetadata(ctx, item.ID)
	if err != nil || current.PK != item.ID || !patchMatches(item.Fields, attachmentValues(current)) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizeAttachment(current))
	return nil
}

func bulkUpdateAttachments(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateAttachmentsInput, BulkUpdateOutput[AttachmentMetadata]] {
	return LookupHandler[AttachmentWriteClient, BulkUpdateAttachmentsInput, BulkUpdateOutput[AttachmentMetadata]](deps, BulkUpdateAttachmentsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input BulkUpdateAttachmentsInput) (*mcp.CallToolResult, BulkUpdateOutput[AttachmentMetadata], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[AttachmentMetadata](BulkUpdateAttachmentsToolName, len(input.Items))
			}
			plan := buildAttachmentBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[AttachmentMetadata]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[attachmentBulkPlanItem, AttachmentMetadata](plan.Items)
				token, err := deps.attachmentBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[AttachmentMetadata]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[AttachmentMetadata]()
			}
			if input.PlanHash == "" || !deps.attachmentBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[AttachmentMetadata]()
			}
			adapter := &attachmentBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[attachmentBulkPlanItem, AttachmentMetadata](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}
