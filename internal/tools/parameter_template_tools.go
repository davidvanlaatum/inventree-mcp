package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxTemplateReferenceScan = 1000

const parameterTemplateModelTypes = "build.build, company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, order.returnorder, order.salesorder, order.salesordershipment, order.transferorder, part.part, part.partcategory, stock.stocklocation, or empty"

type ParameterTemplateAdminClient interface {
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	CreateParameterTemplate(context.Context, inventree.ParameterTemplateCreate) (inventree.ParameterTemplate, error)
	UpdateParameterTemplate(context.Context, int, inventree.PatchFields) (inventree.ParameterTemplate, error)
	DeleteParameterTemplate(context.Context, int) error
	SearchTemplateParametersPage(context.Context, inventree.TemplateParameterQuery) (inventree.PartParameterPage, error)
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
	GetPartParameter(context.Context, int) (inventree.Parameter, error)
	UpdatePartParameter(context.Context, int, inventree.PatchFields) (inventree.Parameter, error)
}

type CreateParameterTemplateInput struct {
	Name            string  `json:"name" jsonschema:"Explicit nonblank parameter-template name."`
	Units           *string `json:"units" jsonschema:"Explicit units; use an empty string for a unitless template."`
	Description     *string `json:"description" jsonschema:"Explicit description; use an empty string when no description is needed."`
	ModelType       *string `json:"model_type" jsonschema:"Explicit InvenTree model-type restriction; use an empty string for no restriction. Supported values: build.build, company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, order.returnorder, order.salesorder, order.salesordershipment, order.transferorder, part.part, part.partcategory, stock.stocklocation."`
	Checkbox        *bool   `json:"checkbox" jsonschema:"Explicit checkbox behavior."`
	Choices         *string `json:"choices" jsonschema:"Explicit comma-separated choices; use an empty string for free-form values."`
	SelectionListID *int    `json:"selection_list_id,omitempty" jsonschema:"Optional existing InvenTree selection-list primary key."`
	Enabled         *bool   `json:"enabled" jsonschema:"Explicit enabled state."`
}

type UpdateParameterTemplateInput struct {
	TemplateID         int     `json:"template_id" jsonschema:"Stable parameter-template primary key."`
	Name               *string `json:"name,omitempty" jsonschema:"Optional replacement name; empty names are rejected."`
	Units              *string `json:"units,omitempty" jsonschema:"Optional replacement units, including an explicit empty string."`
	Description        *string `json:"description,omitempty" jsonschema:"Optional replacement description, including an explicit empty string."`
	ModelType          *string `json:"model_type,omitempty" jsonschema:"Optional replacement model-type restriction, including an explicit empty string; nonempty values must use the documented InvenTree model-type enum."`
	Checkbox           *bool   `json:"checkbox,omitempty" jsonschema:"Optional explicit checkbox behavior."`
	Choices            *string `json:"choices,omitempty" jsonschema:"Optional replacement choices, including an explicit empty string."`
	SelectionListID    *int    `json:"selection_list_id,omitempty" jsonschema:"Optional replacement selection-list primary key."`
	ClearSelectionList bool    `json:"clear_selection_list,omitempty" jsonschema:"Explicitly clear the current selection-list link; mutually exclusive with selection_list_id."`
	Enabled            *bool   `json:"enabled,omitempty" jsonschema:"Optional explicit enabled state."`
}

type DeleteParameterTemplateInput struct {
	TemplateID int  `json:"template_id" jsonschema:"Stable parameter-template primary key."`
	Confirm    bool `json:"confirm,omitempty" jsonschema:"Required true after reviewing an unreferenced template."`
}

type TemplateReferenceSummary struct {
	ParameterIDs    []int `json:"parameter_ids,omitempty"`
	CategoryLinkIDs []int `json:"category_link_ids,omitempty"`
}

type ParameterTemplateOutput struct {
	Status        string                       `json:"status"`
	Record        *inventree.ParameterTemplate `json:"record,omitempty"`
	References    TemplateReferenceSummary     `json:"references,omitempty"`
	Verified      bool                         `json:"verified,omitempty"`
	Clarification *ClarificationResponse       `json:"clarification,omitempty"`
}

type MergeParameterTemplatesInput struct {
	SourceTemplateID int               `json:"source_template_id" jsonschema:"Template whose part-parameter rows will move to the target."`
	TargetTemplateID int               `json:"target_template_id" jsonschema:"Existing destination template."`
	ValueMap         map[string]string `json:"value_map,omitempty" jsonschema:"Optional exact source-to-target value normalization map; all unmapped values are preserved."`
	DryRun           bool              `json:"dry_run,omitempty" jsonschema:"Return the current merge plan without writing."`
	Confirm          bool              `json:"confirm,omitempty" jsonschema:"Required true to execute a reviewed plan and delete the empty source template."`
	PlanHash         string            `json:"plan_hash,omitempty" jsonschema:"Exact current-state hash returned by dry_run:true."`
}

type ParameterTemplateMergeAction struct {
	ParameterID int    `json:"parameter_id,omitempty"`
	ModelType   string `json:"model_type"`
	ModelID     int    `json:"model_id"`
	PartID      int    `json:"part_id,omitempty"`
	FromValue   string `json:"from_value,omitempty"`
	ToValue     string `json:"to_value,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

type CategoryTemplateReference struct {
	LinkID     int `json:"link_id"`
	CategoryID int `json:"category_id"`
}

type MergeParameterTemplatesOutput struct {
	Status            string                         `json:"status"`
	SourceTemplate    inventree.ParameterTemplate    `json:"source_template"`
	TargetTemplate    inventree.ParameterTemplate    `json:"target_template"`
	Actions           []ParameterTemplateMergeAction `json:"actions"`
	CategoryLinkIDs   []int                          `json:"category_link_ids,omitempty"`
	CategoryLinks     []CategoryTemplateReference    `json:"category_links,omitempty"`
	PlanHash          string                         `json:"plan_hash,omitempty"`
	SourceDeleted     bool                           `json:"source_deleted"`
	ReadbackVerified  bool                           `json:"readback_verified"`
	ResidualDecisions []string                       `json:"residual_decisions,omitempty"`
	Clarification     *ClarificationResponse         `json:"clarification,omitempty"`
}

func registerParameterTemplateTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreateParameterTemplateToolName, "Create parameter template", "Creates an explicit parameter template after collision preflight.", createParameterTemplate(deps))
	addWriteTool(server, deps, UpdateParameterTemplateToolName, "Update parameter template", "Partially updates one parameter template while preserving omitted fields.", updateParameterTemplate(deps))
	addWriteTool(server, deps, DeleteParameterTemplateToolName, "Delete parameter template", "Deletes one unreferenced parameter template after explicit confirmation.", deleteParameterTemplate(deps))
	addWriteTool(server, deps, MergeParameterTemplatesToolName, "Merge parameter templates", "Plans or confirms migration of non-conflicting part-parameter rows and deletes the source only when empty.", mergeParameterTemplates(deps))
}

func createParameterTemplate(deps Dependencies) mcp.ToolHandlerFor[CreateParameterTemplateInput, ParameterTemplateOutput] {
	return LookupHandler[ParameterTemplateAdminClient, CreateParameterTemplateInput, ParameterTemplateOutput](deps, CreateParameterTemplateToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterTemplateAdminClient, input CreateParameterTemplateInput) (*mcp.CallToolResult, ParameterTemplateOutput, error) {
			if strings.TrimSpace(input.Name) == "" || input.Units == nil || input.Description == nil || input.ModelType == nil || input.Checkbox == nil || input.Choices == nil || input.Enabled == nil {
				return templateClarification("Which explicit template definition should be created?", "template", "name, units, description, model_type, checkbox, choices, and enabled must all be supplied explicitly", "template", map[string]any{"name": input.Name})
			}
			if !validParameterTemplateModelType(*input.ModelType) {
				return templateClarification("Which supported model type should restrict this template?", "model_type", "model_type must be one of "+parameterTemplateModelTypes, "model_type", map[string]any{"name": input.Name})
			}
			if input.SelectionListID != nil && *input.SelectionListID <= 0 {
				return templateClarification("Which selection list should be linked?", "selection_list_id", "selection_list_id must be positive when provided", "selection_list_id", map[string]any{"name": input.Name})
			}
			collisions, err := exactTemplateNameMatches(ctx, client, input.Name, 0)
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			if len(collisions) > 0 {
				return templateCandidatesClarification("Should an existing parameter template be used instead?", "template", "one or more templates already use this case-insensitive name", "template_id", collisions, map[string]any{"name": input.Name})
			}
			record, err := client.CreateParameterTemplate(ctx, inventree.ParameterTemplateCreate{Name: strings.TrimSpace(input.Name), Units: *input.Units, Description: *input.Description, ModelType: *input.ModelType, Checkbox: *input.Checkbox, Choices: *input.Choices, SelectionList: input.SelectionListID, Enabled: *input.Enabled})
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			return TextResult(StatusOK), ParameterTemplateOutput{Status: StatusOK, Record: &record}, nil
		})
}

func updateParameterTemplate(deps Dependencies) mcp.ToolHandlerFor[UpdateParameterTemplateInput, ParameterTemplateOutput] {
	return LookupHandler[ParameterTemplateAdminClient, UpdateParameterTemplateInput, ParameterTemplateOutput](deps, UpdateParameterTemplateToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterTemplateAdminClient, input UpdateParameterTemplateInput) (*mcp.CallToolResult, ParameterTemplateOutput, error) {
			if input.TemplateID <= 0 {
				return templateClarification("Which parameter template should be updated?", "template_id", "template_id must be positive", "template_id", nil)
			}
			if input.SelectionListID != nil && input.ClearSelectionList {
				return templateClarification("How should the selection-list link change?", "selection_list_id", "selection_list_id and clear_selection_list cannot be supplied together", "selection_list_id", map[string]any{"template_id": input.TemplateID})
			}
			if input.SelectionListID != nil && *input.SelectionListID <= 0 {
				return templateClarification("Which selection list should be linked?", "selection_list_id", "selection_list_id must be positive", "selection_list_id", map[string]any{"template_id": input.TemplateID})
			}
			if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
				return templateClarification("What nonblank name should the template use?", "name", "template name cannot be blank", "name", map[string]any{"template_id": input.TemplateID})
			}
			if input.ModelType != nil && !validParameterTemplateModelType(*input.ModelType) {
				return templateClarification("Which supported model type should restrict this template?", "model_type", "model_type must be one of "+parameterTemplateModelTypes, "model_type", map[string]any{"template_id": input.TemplateID})
			}
			existing, err := client.GetParameterTemplate(ctx, input.TemplateID)
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), ParameterTemplateOutput{Status: StatusNotFound}, nil
				}
				return nil, ParameterTemplateOutput{}, err
			}
			name := existing.Name
			if input.Name != nil {
				name = *input.Name
			}
			collisions, err := exactTemplateNameMatches(ctx, client, name, input.TemplateID)
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			if len(collisions) > 0 {
				return templateCandidatesClarification("Which unique parameter-template identity should be used?", "template", "the updated name collides case-insensitively with another template", "template_id", collisions, map[string]any{"template_id": input.TemplateID, "name": name})
			}
			fields := parameterTemplatePatch(input)
			if len(fields) == 0 {
				return templateClarification("Which template fields should be updated?", "fields", "at least one explicit replacement field is required", "fields", map[string]any{"template_id": input.TemplateID})
			}
			record, err := client.UpdateParameterTemplate(ctx, input.TemplateID, fields)
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			return TextResult(StatusOK), ParameterTemplateOutput{Status: StatusOK, Record: &record}, nil
		})
}

func deleteParameterTemplate(deps Dependencies) mcp.ToolHandlerFor[DeleteParameterTemplateInput, ParameterTemplateOutput] {
	return LookupHandler[ParameterTemplateAdminClient, DeleteParameterTemplateInput, ParameterTemplateOutput](deps, DeleteParameterTemplateToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterTemplateAdminClient, input DeleteParameterTemplateInput) (*mcp.CallToolResult, ParameterTemplateOutput, error) {
			if input.TemplateID <= 0 {
				return templateClarification("Which parameter template should be deleted?", "template_id", "template_id must be positive", "template_id", nil)
			}
			record, err := client.GetParameterTemplate(ctx, input.TemplateID)
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), ParameterTemplateOutput{Status: StatusNotFound}, nil
				}
				return nil, ParameterTemplateOutput{}, err
			}
			parameters, err := scanTemplateParameters(ctx, client, input.TemplateID)
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			links, err := sourceCategoryLinks(ctx, client, input.TemplateID)
			if err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			references := templateReferences(parameters, links)
			if len(parameters) > 0 || len(links) > 0 {
				clarification := NewClarification("How should this referenced template be cleaned up?", "references", "referenced templates cannot be deleted directly; migrate parameter rows through merge_parameter_templates and remove category-default links explicitly", "merge_parameter_templates", true, []ClarificationCandidate{templateCandidate(record)}, map[string]any{"source_template_id": input.TemplateID, "dry_run": true})
				return TextResult(StatusClarificationRequired), ParameterTemplateOutput{Status: StatusClarificationRequired, Record: &record, References: references, Clarification: &clarification}, nil
			}
			if !input.Confirm {
				clarification := NewClarification("Delete this unreferenced parameter template?", "confirm", "delete_parameter_template requires confirm:true after reference preflight", "confirm", true, []ClarificationCandidate{templateCandidate(record)}, map[string]any{"template_id": input.TemplateID})
				return TextResult(StatusClarificationRequired), ParameterTemplateOutput{Status: StatusClarificationRequired, Record: &record, References: references, Clarification: &clarification}, nil
			}
			if err := client.DeleteParameterTemplate(ctx, input.TemplateID); err != nil {
				return nil, ParameterTemplateOutput{}, err
			}
			_, err = client.GetParameterTemplate(ctx, input.TemplateID)
			if err == nil {
				return nil, ParameterTemplateOutput{}, fmt.Errorf("parameter template %d still exists after deletion", input.TemplateID)
			}
			if !isNotFound(err) {
				return nil, ParameterTemplateOutput{}, fmt.Errorf("verify parameter template %d deletion: %w", input.TemplateID, err)
			}
			return TextResult(StatusOK), ParameterTemplateOutput{Status: StatusOK, Record: &record, References: references, Verified: true}, nil
		})
}

func mergeParameterTemplates(deps Dependencies) mcp.ToolHandlerFor[MergeParameterTemplatesInput, MergeParameterTemplatesOutput] {
	return LookupHandler[ParameterTemplateAdminClient, MergeParameterTemplatesInput, MergeParameterTemplatesOutput](deps, MergeParameterTemplatesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterTemplateAdminClient, input MergeParameterTemplatesInput) (*mcp.CallToolResult, MergeParameterTemplatesOutput, error) {
			if input.SourceTemplateID <= 0 || input.TargetTemplateID <= 0 || input.SourceTemplateID == input.TargetTemplateID {
				return mergeClarification(MergeParameterTemplatesOutput{}, "Which distinct templates should be merged?", "templates", "source_template_id and target_template_id must be distinct positive IDs", "templates", map[string]any{"source_template_id": input.SourceTemplateID, "target_template_id": input.TargetTemplateID})
			}
			plan, err := buildTemplateMergePlan(ctx, client, input)
			if err != nil {
				return nil, MergeParameterTemplatesOutput{}, err
			}
			if input.DryRun {
				plan.Status = StatusOK
				return TextResult(StatusOK), plan, nil
			}
			if !input.Confirm {
				return mergeClarification(plan, "Should this current merge plan be executed?", "confirm", "run dry_run:true first, then provide its plan_hash with confirm:true", "dry_run", map[string]any{"source_template_id": input.SourceTemplateID, "target_template_id": input.TargetTemplateID, "dry_run": true, "value_map": input.ValueMap})
			}
			if input.PlanHash == "" || input.PlanHash != plan.PlanHash {
				return mergeClarification(plan, "Which current merge plan should authorize execution?", "plan_hash", "plan_hash must match a dry run for the current templates, rows, category links, and value map", "plan_hash", map[string]any{"source_template_id": input.SourceTemplateID, "target_template_id": input.TargetTemplateID, "dry_run": true, "value_map": input.ValueMap})
			}
			for i := range plan.Actions {
				action := &plan.Actions[i]
				if action.Status != "planned" {
					continue
				}
				existing, err := client.GetPartParameter(ctx, action.ParameterID)
				if err != nil || existing.Template != input.SourceTemplateID || existing.ModelType != "part.part" || existing.ModelID != action.PartID || existing.Data != action.FromValue {
					action.Status = "failed"
					action.Reason = "source row changed after planning; rerun dry_run:true"
					plan.Status = "partial_failure"
					plan.ResidualDecisions = append(plan.ResidualDecisions, fmt.Sprintf("parameter row %d requires a fresh plan", action.ParameterID))
					return TextResult(plan.Status), plan, nil
				}
				if _, err := client.UpdatePartParameter(ctx, action.ParameterID, inventree.PatchFields{"template": inventree.Set(input.TargetTemplateID), "data": inventree.Set(action.ToValue)}); err != nil {
					action.Status = "failed"
					action.Reason = "upstream row update failed; inspect current state before retry"
					plan.Status = "partial_failure"
					plan.ResidualDecisions = append(plan.ResidualDecisions, fmt.Sprintf("parameter row %d update failed; inspect current state before retry", action.ParameterID))
					return TextResult(plan.Status), plan, nil
				}
				readback, err := client.GetPartParameter(ctx, action.ParameterID)
				if err != nil || readback.Template != input.TargetTemplateID || readback.Data != action.ToValue {
					action.Status = "failed"
					action.Reason = "updated row did not match the planned target on read-back"
					plan.Status = "partial_failure"
					plan.ResidualDecisions = append(plan.ResidualDecisions, fmt.Sprintf("parameter row %d has an unknown post-update state", action.ParameterID))
					return TextResult(plan.Status), plan, nil
				}
				action.Status = "applied"
			}
			remaining, err := scanTemplateParameters(ctx, client, input.SourceTemplateID)
			if err != nil {
				return nil, MergeParameterTemplatesOutput{}, err
			}
			remainingLinks, err := sourceCategoryLinks(ctx, client, input.SourceTemplateID)
			if err != nil {
				return nil, MergeParameterTemplatesOutput{}, err
			}
			plan.CategoryLinkIDs, plan.CategoryLinks = categoryLinkReferences(remainingLinks)
			if len(remaining) > 0 {
				remainingIDs := make([]int, 0, len(remaining))
				for _, row := range remaining {
					remainingIDs = append(remainingIDs, row.PK)
				}
				plan.ResidualDecisions = append(plan.ResidualDecisions, fmt.Sprintf("current source parameter rows %v prevent deletion; resolve them, then rerun dry_run:true", remainingIDs))
			}
			if len(remainingLinks) > 0 {
				plan.ResidualDecisions = append(plan.ResidualDecisions, "current source category-default links prevent deletion; remove or migrate them through the InvenTree UI/API, then rerun dry_run:true")
			}
			if len(remaining) == 0 && len(remainingLinks) == 0 {
				if err := client.DeleteParameterTemplate(ctx, input.SourceTemplateID); err != nil {
					plan.Status = "partial_failure"
					plan.ResidualDecisions = append(plan.ResidualDecisions, "all rows moved but source-template deletion failed; inspect and delete the now-unreferenced source explicitly")
					return TextResult(plan.Status), plan, nil
				}
				_, err = client.GetParameterTemplate(ctx, input.SourceTemplateID)
				if err == nil || !isNotFound(err) {
					plan.Status = "partial_failure"
					plan.ResidualDecisions = append(plan.ResidualDecisions, "source-template deletion could not be verified")
					return TextResult(plan.Status), plan, nil
				}
				plan.SourceDeleted = true
			}
			plan.ReadbackVerified = true
			plan.Status = StatusOK
			return TextResult(StatusOK), plan, nil
		})
}

func buildTemplateMergePlan(ctx context.Context, client ParameterTemplateAdminClient, input MergeParameterTemplatesInput) (MergeParameterTemplatesOutput, error) {
	source, err := client.GetParameterTemplate(ctx, input.SourceTemplateID)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	target, err := client.GetParameterTemplate(ctx, input.TargetTemplateID)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	sourceRows, err := scanTemplateParameters(ctx, client, input.SourceTemplateID)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	targetRows, err := scanTemplateParameters(ctx, client, input.TargetTemplateID)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	links, err := sourceCategoryLinks(ctx, client, input.SourceTemplateID)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	targetByObject := map[string]inventree.Parameter{}
	for _, row := range targetRows {
		targetByObject[fmt.Sprintf("%s:%d", row.ModelType, row.ModelID)] = row
	}
	out := MergeParameterTemplatesOutput{SourceTemplate: source, TargetTemplate: target, Actions: make([]ParameterTemplateMergeAction, 0, len(sourceRows))}
	out.CategoryLinkIDs, out.CategoryLinks = categoryLinkReferences(links)
	if len(links) > 0 {
		out.ResidualDecisions = append(out.ResidualDecisions, "source category-default links must be removed or migrated explicitly before the source template can be deleted")
	}
	for _, row := range sourceRows {
		action := ParameterTemplateMergeAction{ParameterID: row.PK, ModelType: row.ModelType, ModelID: row.ModelID, FromValue: row.Data, ToValue: row.Data, Status: "planned"}
		if row.ModelType != "part.part" {
			action.Status = "skipped"
			action.Reason = fmt.Sprintf("unsupported model type %q requires manual migration", row.ModelType)
			out.ResidualDecisions = append(out.ResidualDecisions, fmt.Sprintf("parameter row %d uses unsupported model type %q", row.PK, row.ModelType))
		} else if targetRow, exists := targetByObject[fmt.Sprintf("%s:%d", row.ModelType, row.ModelID)]; exists {
			action.PartID = row.ModelID
			action.Status = "skipped"
			action.Reason = fmt.Sprintf("part already has target-template row %d; neither value will be overwritten", targetRow.PK)
			out.ResidualDecisions = append(out.ResidualDecisions, fmt.Sprintf("part %d has both source row %d and target row %d", row.ModelID, row.PK, targetRow.PK))
		} else {
			action.PartID = row.ModelID
			if normalized, ok := input.ValueMap[row.Data]; ok {
				action.ToValue = normalized
			}
		}
		out.Actions = append(out.Actions, action)
	}
	hashInput := struct {
		Source   inventree.ParameterTemplate
		Target   inventree.ParameterTemplate
		Actions  []ParameterTemplateMergeAction
		Links    []int
		ValueMap map[string]string
	}{source, target, out.Actions, out.CategoryLinkIDs, input.ValueMap}
	payload, err := json.Marshal(hashInput)
	if err != nil {
		return MergeParameterTemplatesOutput{}, err
	}
	digest := sha256.Sum256(payload)
	out.PlanHash = hex.EncodeToString(digest[:])
	return out, nil
}

func scanTemplateParameters(ctx context.Context, client ParameterTemplateAdminClient, templateID int) ([]inventree.Parameter, error) {
	var rows []inventree.Parameter
	for offset := 0; offset < maxTemplateReferenceScan; {
		page, err := client.SearchTemplateParametersPage(ctx, inventree.TemplateParameterQuery{TemplateID: templateID, Limit: partParameterPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		rows = append(rows, page.Results...)
		offset += len(page.Results)
		if !page.HasMore || len(page.Results) == 0 {
			slices.SortFunc(rows, func(a, b inventree.Parameter) int { return a.PK - b.PK })
			return rows, nil
		}
	}
	return nil, fmt.Errorf("template %d has more than the %d-row safety bound; narrow or clean it up manually", templateID, maxTemplateReferenceScan)
}

func sourceCategoryLinks(ctx context.Context, client ParameterTemplateAdminClient, templateID int) ([]inventree.CategoryParameterTemplate, error) {
	var filtered []inventree.CategoryParameterTemplate
	for offset := 0; offset < maxTemplateReferenceScan; {
		page, err := client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{Limit: partParameterPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, link := range page.Results {
			if link.Template == templateID {
				filtered = append(filtered, link)
			}
		}
		offset += len(page.Results)
		if page.Next == nil || *page.Next == "" || len(page.Results) == 0 {
			slices.SortFunc(filtered, func(a, b inventree.CategoryParameterTemplate) int { return a.PK - b.PK })
			return filtered, nil
		}
	}
	return nil, fmt.Errorf("category parameter defaults exceed the %d-row safety bound; clean them up or narrow administration manually", maxTemplateReferenceScan)
}

func categoryLinkReferences(links []inventree.CategoryParameterTemplate) ([]int, []CategoryTemplateReference) {
	ids := make([]int, 0, len(links))
	references := make([]CategoryTemplateReference, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.PK)
		references = append(references, CategoryTemplateReference{LinkID: link.PK, CategoryID: link.Category})
	}
	return ids, references
}

func validParameterTemplateModelType(modelType string) bool {
	switch modelType {
	case "", "build.build", "company.company", "company.manufacturerpart", "company.supplierpart", "order.purchaseorder", "order.returnorder", "order.salesorder", "order.salesordershipment", "order.transferorder", "part.part", "part.partcategory", "stock.stocklocation":
		return true
	default:
		return false
	}
}

func exactTemplateNameMatches(ctx context.Context, client ParameterTemplateAdminClient, name string, excludeID int) ([]inventree.ParameterTemplate, error) {
	records, err := client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: strings.TrimSpace(name), Limit: MaxLookupLimit})
	if err != nil {
		return nil, err
	}
	exact := records[:0]
	for _, record := range records {
		if record.PK != excludeID && strings.EqualFold(strings.TrimSpace(record.Name), strings.TrimSpace(name)) {
			exact = append(exact, record)
		}
	}
	return exact, nil
}

func parameterTemplatePatch(input UpdateParameterTemplateInput) inventree.PatchFields {
	fields := inventree.PatchFields{}
	if input.Name != nil {
		fields["name"] = inventree.Set(strings.TrimSpace(*input.Name))
	}
	if input.Units != nil {
		fields["units"] = inventree.Set(*input.Units)
	}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.ModelType != nil {
		fields["model_type"] = inventree.Set(*input.ModelType)
	}
	if input.Checkbox != nil {
		fields["checkbox"] = inventree.Set(*input.Checkbox)
	}
	if input.Choices != nil {
		fields["choices"] = inventree.Set(*input.Choices)
	}
	if input.SelectionListID != nil {
		fields["selectionlist"] = inventree.Set(*input.SelectionListID)
	}
	if input.ClearSelectionList {
		fields["selectionlist"] = inventree.Null()
	}
	if input.Enabled != nil {
		fields["enabled"] = inventree.Set(*input.Enabled)
	}
	return fields
}

func templateReferences(parameters []inventree.Parameter, links []inventree.CategoryParameterTemplate) TemplateReferenceSummary {
	out := TemplateReferenceSummary{}
	for _, row := range parameters {
		out.ParameterIDs = append(out.ParameterIDs, row.PK)
	}
	for _, link := range links {
		out.CategoryLinkIDs = append(out.CategoryLinkIDs, link.PK)
	}
	return out
}

func templateCandidate(record inventree.ParameterTemplate) ClarificationCandidate {
	return ClarificationCandidate{ID: fmt.Sprint(record.PK), Label: record.Name, URL: fmt.Sprintf("/api/parameter/template/%d/", record.PK), Fields: map[string]any{"units": record.Units, "choices": record.Choices, "checkbox": record.Checkbox, "enabled": record.Enabled}}
}

func templateClarification(question, field, reason, retry string, retryValues map[string]any) (*mcp.CallToolResult, ParameterTemplateOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), ParameterTemplateOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func templateCandidatesClarification(question, field, reason, retry string, records []inventree.ParameterTemplate, retryValues map[string]any) (*mcp.CallToolResult, ParameterTemplateOutput, error) {
	candidates := make([]ClarificationCandidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, templateCandidate(record))
	}
	clarification := NewClarification(question, field, reason, retry, false, candidates, retryValues)
	return TextResult(StatusClarificationRequired), ParameterTemplateOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func mergeClarification(out MergeParameterTemplatesOutput, question, field, reason, retry string, retryValues map[string]any) (*mcp.CallToolResult, MergeParameterTemplatesOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}
