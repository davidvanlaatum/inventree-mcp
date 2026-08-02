package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const categoryAdminScanLimit = 1000
const categoryHierarchyDepthLimit = 100

var (
	errCategoryAdminScanLimit      = errors.New("category administration scan safety limit exceeded")
	errCategorySelfParent          = errors.New("a category cannot be its own parent")
	errCategoryDescendantParent    = errors.New("a category cannot be reparented beneath one of its descendants")
	errCategoryExistingParentCycle = errors.New("the proposed parent chain already contains a cycle")
	errCategoryHierarchyDepth      = errors.New("parent-chain validation exceeds the safety bound")
)

type CategoryAdminClient interface {
	GetPartCategory(context.Context, int) (inventree.Category, error)
	SearchPartCategoriesPage(context.Context, inventree.CategoryQuery) (inventree.CategoryPage, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error)
	CreatePartCategory(context.Context, inventree.CategoryCreate) (inventree.Category, error)
	UpdatePartCategory(context.Context, int, inventree.PatchFields) (inventree.Category, error)
}

type CreatePartCategoryInput struct {
	Name              string  `json:"name" jsonschema:"Category name. Surrounding whitespace is removed before duplicate checks and creation."`
	Description       *string `json:"description,omitempty" jsonschema:"Optional category description; an explicit empty string is preserved."`
	ParentID          *int    `json:"parent_id,omitempty" jsonschema:"Optional existing parent category primary key. Omit for a root category."`
	DefaultLocationID *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing default stock-location primary key."`
	DefaultKeywords   *string `json:"default_keywords,omitempty" jsonschema:"Optional default part keywords; an explicit empty string is preserved."`
	Structural        *bool   `json:"structural,omitempty" jsonschema:"Optional explicit structural flag; false is preserved."`
	Icon              *string `json:"icon,omitempty" jsonschema:"Optional InvenTree category icon identifier; an explicit empty string is preserved."`
}

type UpdatePartCategoryInput struct {
	ID                   int     `json:"id" jsonschema:"Stable InvenTree category primary key."`
	Name                 *string `json:"name,omitempty" jsonschema:"Optional replacement name; surrounding whitespace is removed."`
	Description          *string `json:"description,omitempty" jsonschema:"Optional replacement description; an explicit empty string is preserved."`
	ParentID             *int    `json:"parent_id,omitempty" jsonschema:"Optional existing replacement parent category primary key."`
	ClearParent          bool    `json:"clear_parent,omitempty" jsonschema:"Explicitly PATCH parent to null and make the category a root; mutually exclusive with parent_id."`
	DefaultLocationID    *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing replacement default stock-location primary key."`
	ClearDefaultLocation bool    `json:"clear_default_location,omitempty" jsonschema:"Explicitly PATCH default_location to null; mutually exclusive with default_location_id."`
	DefaultKeywords      *string `json:"default_keywords,omitempty" jsonschema:"Optional replacement default keywords; an explicit empty string is preserved."`
	ClearDefaultKeywords bool    `json:"clear_default_keywords,omitempty" jsonschema:"Explicitly PATCH default_keywords to null; mutually exclusive with default_keywords."`
	Structural           *bool   `json:"structural,omitempty" jsonschema:"Optional explicit structural flag. Any change requires confirm:true; promotion is refused while direct parts exist."`
	Icon                 *string `json:"icon,omitempty" jsonschema:"Optional replacement icon identifier; an explicit empty string is preserved."`
	ClearIcon            bool    `json:"clear_icon,omitempty" jsonschema:"Explicitly PATCH icon to null; mutually exclusive with icon."`
	Confirm              bool    `json:"confirm,omitempty" jsonschema:"Required after reviewing hierarchy context for reparenting or a structural-state change."`
}

type CategoryHierarchyContext struct {
	Current          inventree.Category  `json:"current"`
	TargetParent     *inventree.Category `json:"target_parent,omitempty"`
	DirectPartCount  int                 `json:"direct_part_count"`
	DirectChildCount int                 `json:"direct_child_count"`
}

type CategoryMutationOutput struct {
	Status        string                    `json:"status"`
	Record        *inventree.Category       `json:"record,omitempty"`
	Before        *inventree.Category       `json:"before,omitempty"`
	Hierarchy     *CategoryHierarchyContext `json:"hierarchy,omitempty"`
	Recovered     bool                      `json:"recovered,omitempty"`
	RecoveryPlan  string                    `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse    `json:"clarification,omitempty"`
}

func registerCategoryAdminTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreatePartCategoryToolName, "Create part category", "Creates a guarded part category after bounded same-parent duplicate and reference preflight.", createPartCategory(deps))
	addWriteTool(server, deps, UpdatePartCategoryToolName, "Update part category", "Partially updates a stable part category with cycle safeguards and confirmed hierarchy or structural changes.", updatePartCategory(deps))
}

func createPartCategory(deps Dependencies) mcp.ToolHandlerFor[CreatePartCategoryInput, CategoryMutationOutput] {
	return LookupHandler[CategoryAdminClient, CreatePartCategoryInput, CategoryMutationOutput](deps, CreatePartCategoryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryAdminClient, input CreatePartCategoryInput) (*mcp.CallToolResult, CategoryMutationOutput, error) {
			name := strings.TrimSpace(input.Name)
			if name == "" {
				return categoryClarification("What nonblank name should the category use?", "name", "create_part_category requires an explicit nonblank name", "name", true, nil, map[string]any{"name": name})
			}
			if input.ParentID != nil {
				if *input.ParentID <= 0 {
					return categoryClarification("Which existing parent category should be used?", "parent_id", "parent_id must be positive when provided", "parent_id", true, nil, map[string]any{"name": name})
				}
				parent, err := client.GetPartCategory(ctx, *input.ParentID)
				if err != nil {
					if categoryReferenceInvalid(err) {
						return categoryClarification("Which existing parent category should be used?", "parent_id", fmt.Sprintf("parent_id %d does not identify a readable category", *input.ParentID), "parent_id", true, nil, categoryCreateRetry(input, name))
					}
					return nil, CategoryMutationOutput{}, safeCategoryError("parent category lookup")
				}
				if parent.PK != *input.ParentID {
					return nil, CategoryMutationOutput{}, safeCategoryError("parent category identity verification")
				}
			}
			if input.DefaultLocationID != nil {
				if *input.DefaultLocationID <= 0 {
					return categoryClarification("Which existing default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", true, nil, map[string]any{"name": name})
				}
				location, err := client.GetStockLocation(ctx, *input.DefaultLocationID)
				if err != nil {
					if categoryReferenceInvalid(err) {
						return categoryClarification("Which existing default stock location should be used?", "default_location_id", fmt.Sprintf("default_location_id %d does not identify a readable stock location", *input.DefaultLocationID), "default_location_id", true, nil, categoryCreateRetry(input, name))
					}
					return nil, CategoryMutationOutput{}, safeCategoryError("default stock-location lookup")
				}
				if location.PK != *input.DefaultLocationID {
					return nil, CategoryMutationOutput{}, safeCategoryError("default stock-location identity verification")
				}
			}
			matches, err := categoryDuplicates(ctx, client, name, input.ParentID, 0)
			if err != nil {
				if errors.Is(err, errCategoryAdminScanLimit) {
					return categoryClarification("How should this duplicate check be narrowed or reviewed?", "category", err.Error(), "parent_id", true, nil, categoryCreateRetry(input, name))
				}
				return nil, CategoryMutationOutput{}, safeCategoryError("category duplicate preflight")
			}
			if len(matches) > 0 {
				clarification := NewClarification("Should an existing same-parent category be used?", "category", "a category with the same trimmed case-insensitive name already exists under this parent", "category_id", false, candidatesFor(matches), categoryCreateRetry(input, name))
				return TextResult(StatusClarificationRequired), CategoryMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}

			created, err := client.CreatePartCategory(ctx, inventree.CategoryCreate{
				Name: name, Description: input.Description, Parent: input.ParentID,
				DefaultLocation: input.DefaultLocationID, DefaultKeywords: input.DefaultKeywords,
				Structural: input.Structural, Icon: input.Icon,
			})
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, CategoryMutationOutput{}, safeCategoryError("category creation")
				}
				return recoverCategoryCreate(ctx, client, input, name)
			}
			refreshed, readErr := client.GetPartCategory(ctx, created.PK)
			if readErr != nil {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &created, RecoveryPlan: fmt.Sprintf("Creation returned category_id %d, but read-back failed. Call get_part_category with that stable ID before retrying any write.", created.PK)}, nil
			}
			if refreshed.PK != created.PK {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &created, RecoveryPlan: fmt.Sprintf("Creation returned category_id %d, but read-back returned a different identity. Call get_part_category with category_id %d before retrying any write.", created.PK, created.PK)}, nil
			}
			if !categoryMatchesCreate(refreshed, input, name) {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, RecoveryPlan: fmt.Sprintf("Creation returned category_id %d, but read-back does not match every supplied field. Review this record and same-parent siblings before another write.", refreshed.PK)}, nil
			}
			if recovery := verifyCategoryPostWrite(ctx, client, refreshed); recovery != "" {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, RecoveryPlan: recovery}, nil
			}
			return TextResult(StatusOK), CategoryMutationOutput{Status: StatusOK, Record: &refreshed}, nil
		})
}

func updatePartCategory(deps Dependencies) mcp.ToolHandlerFor[UpdatePartCategoryInput, CategoryMutationOutput] {
	return LookupHandler[CategoryAdminClient, UpdatePartCategoryInput, CategoryMutationOutput](deps, UpdatePartCategoryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryAdminClient, input UpdatePartCategoryInput) (*mcp.CallToolResult, CategoryMutationOutput, error) {
			if input.ID <= 0 {
				return categoryClarification("Which category should be updated?", "id", "update_part_category requires a positive stable ID", "id", true, nil, map[string]any{"id": input.ID})
			}
			if reason := categoryClearConflict(input); reason != "" {
				return categoryClarification("How should this nullable category field change?", "patch", reason, "id", true, nil, categoryUpdateRetry(input, false))
			}
			before, err := client.GetPartCategory(ctx, input.ID)
			if err != nil {
				if categoryReferenceInvalid(err) {
					return categoryClarification("Which existing category should be updated?", "id", fmt.Sprintf("id %d does not identify a readable category", input.ID), "id", true, nil, categoryUpdateRetry(input, false))
				}
				return nil, CategoryMutationOutput{}, safeCategoryError("category lookup")
			}
			if before.PK != input.ID {
				return nil, CategoryMutationOutput{}, safeCategoryError("category identity verification")
			}
			fields, targetName, targetParent, err := categoryPatch(input, before)
			if err != nil {
				return categoryClarification("Which valid category fields should be updated?", "patch", err.Error(), "id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false))
			}
			if len(fields) == 0 {
				return categoryClarification("Which category fields should be updated?", "patch", "update_part_category requires at least one PATCH field", "id", true, []ClarificationCandidate{candidateFor(before)}, map[string]any{"id": input.ID})
			}
			var targetParentRecord *inventree.Category
			if targetParent != nil {
				if *targetParent <= 0 {
					return categoryClarification("Which existing non-descendant parent should be used?", "parent_id", "parent_id must be positive when provided", "parent_id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false))
				}
				parent, parentErr := validateCategoryParent(ctx, client, input.ID, *targetParent)
				if parentErr != nil {
					if categoryReferenceInvalid(parentErr) {
						return categoryClarification("Which existing non-descendant parent should be used?", "parent_id", fmt.Sprintf("parent_id %d does not identify a readable category", *targetParent), "parent_id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false))
					}
					if categoryParentPolicyError(parentErr) {
						return categoryClarification("Which non-descendant parent should be used?", "parent_id", parentErr.Error(), "parent_id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false))
					}
					return nil, CategoryMutationOutput{}, safeCategoryError("parent category validation")
				}
				targetParentRecord = &parent
			}
			if input.DefaultLocationID != nil {
				if *input.DefaultLocationID <= 0 {
					return categoryClarification("Which existing default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", true, nil, categoryUpdateRetry(input, false))
				}
				location, err := client.GetStockLocation(ctx, *input.DefaultLocationID)
				if err != nil {
					if categoryReferenceInvalid(err) {
						return categoryClarification("Which existing default stock location should be used?", "default_location_id", fmt.Sprintf("default_location_id %d does not identify a readable stock location", *input.DefaultLocationID), "default_location_id", true, nil, categoryUpdateRetry(input, false))
					}
					return nil, CategoryMutationOutput{}, safeCategoryError("default stock-location lookup")
				}
				if location.PK != *input.DefaultLocationID {
					return nil, CategoryMutationOutput{}, safeCategoryError("default stock-location identity verification")
				}
			}
			if targetName != before.Name || !sameOptionalInt(targetParent, before.Parent) {
				matches, duplicateErr := categoryDuplicates(ctx, client, targetName, targetParent, input.ID)
				if duplicateErr != nil {
					if errors.Is(duplicateErr, errCategoryAdminScanLimit) {
						return categoryClarification("How should this duplicate check be narrowed or reviewed?", "category", duplicateErr.Error(), "parent_id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false))
					}
					return nil, CategoryMutationOutput{}, safeCategoryError("category duplicate preflight")
				}
				if len(matches) > 0 {
					clarification := NewClarification("Which same-parent category should remain?", "category", "the requested name and parent collide with an existing category", "category_id", true, candidatesFor(matches), categoryUpdateRetry(input, false))
					return TextResult(StatusClarificationRequired), CategoryMutationOutput{Status: StatusClarificationRequired, Before: &before, Clarification: &clarification}, nil
				}
			}

			directParts, err := directCategoryPartCount(ctx, client, input.ID)
			if err != nil {
				return nil, CategoryMutationOutput{}, safeCategoryError("direct-part lookup")
			}
			hierarchy := CategoryHierarchyContext{Current: before, TargetParent: targetParentRecord, DirectPartCount: directParts, DirectChildCount: optionalCount(before.Subcategories)}
			parentChanged := !sameOptionalInt(targetParent, before.Parent)
			structuralChanged := input.Structural != nil && *input.Structural != before.Structural
			if structuralChanged && !before.Structural && *input.Structural && directParts > 0 {
				return categoryClarificationWithContext("How should the directly assigned parts be handled first?", "structural", fmt.Sprintf("category %d has %d directly assigned parts and cannot be promoted to structural by this workflow", input.ID, directParts), "id", true, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, false), before, hierarchy)
			}
			if (parentChanged || structuralChanged) && !input.Confirm {
				reason := "reparenting requires confirmation after reviewing the affected hierarchy"
				if structuralChanged && parentChanged {
					reason = "reparenting and structural-state changes require confirmation after reviewing the affected hierarchy"
				} else if structuralChanged {
					reason = "structural-state changes require confirmation after reviewing the affected hierarchy"
				}
				return categoryClarificationWithContext("Apply this reviewed category hierarchy change?", "confirmation", reason, "confirm", false, []ClarificationCandidate{candidateFor(before)}, categoryUpdateRetry(input, true), before, hierarchy)
			}

			updated, err := client.UpdatePartCategory(ctx, input.ID, fields)
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, CategoryMutationOutput{}, safeCategoryError("category update")
				}
				return recoverCategoryUpdate(ctx, client, before, fields, hierarchy)
			}
			if updated.PK != input.ID {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: fmt.Sprintf("PATCH for category_id %d returned mismatched category_id %d. Read category_id %d before any retry.", input.ID, updated.PK, input.ID)}, nil
			}
			refreshed, readErr := client.GetPartCategory(ctx, input.ID)
			if readErr != nil {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: fmt.Sprintf("PATCH returned category_id %d, but read-back failed. Call get_part_category with that stable ID before retrying.", input.ID)}, nil
			}
			if refreshed.PK != input.ID {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: fmt.Sprintf("PATCH targeted category_id %d, but read-back returned a different identity. Call get_part_category with category_id %d before any retry.", input.ID, input.ID)}, nil
			}
			if !categoryMatchesPatch(refreshed, fields) {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: fmt.Sprintf("PATCH returned for category_id %d, but read-back does not match every requested field. Inspect current state before retrying.", input.ID)}, nil
			}
			if recovery := verifyCategoryPostWrite(ctx, client, refreshed); recovery != "" {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: recovery}, nil
			}
			return TextResult(StatusOK), CategoryMutationOutput{Status: StatusOK, Record: &refreshed, Before: &before, Hierarchy: &hierarchy}, nil
		})
}

func categoryDuplicates(ctx context.Context, client CategoryAdminClient, name string, parent *int, excludeID int) ([]inventree.Category, error) {
	var matches []inventree.Category
	for offset := 0; offset < categoryAdminScanLimit; offset += MaxLookupLimit {
		query := inventree.CategoryQuery{Limit: MaxLookupLimit, Offset: offset}
		if parent == nil {
			top := true
			query.TopLevel = &top
		} else {
			query.Parent = parent
		}
		page, err := client.SearchPartCategoriesPage(ctx, query)
		if err != nil {
			return nil, err
		}
		if page.Count > categoryAdminScanLimit {
			return nil, fmt.Errorf("%w: same-parent duplicate preflight exceeds %d records", errCategoryAdminScanLimit, categoryAdminScanLimit)
		}
		for _, category := range page.Results {
			if category.PK != excludeID && strings.EqualFold(strings.TrimSpace(category.Name), name) {
				matches = append(matches, category)
			}
		}
		if !page.HasMore {
			return matches, nil
		}
	}
	return nil, fmt.Errorf("%w: same-parent duplicate preflight exceeds %d records", errCategoryAdminScanLimit, categoryAdminScanLimit)
}

func validateCategoryParent(ctx context.Context, client CategoryAdminClient, categoryID, parentID int) (inventree.Category, error) {
	if parentID <= 0 {
		return inventree.Category{}, errors.New("parent_id must be positive when provided")
	}
	if parentID == categoryID {
		return inventree.Category{}, errCategorySelfParent
	}
	parent, err := client.GetPartCategory(ctx, parentID)
	if err != nil {
		return inventree.Category{}, err
	}
	if parent.PK != parentID {
		return inventree.Category{}, safeCategoryError("parent category identity verification")
	}
	cursor := parent
	visited := map[int]bool{}
	for depth := 0; depth < categoryHierarchyDepthLimit; depth++ {
		if visited[cursor.PK] {
			return inventree.Category{}, errCategoryExistingParentCycle
		}
		visited[cursor.PK] = true
		if cursor.PK == categoryID {
			return inventree.Category{}, errCategoryDescendantParent
		}
		if cursor.Parent == nil {
			return parent, nil
		}
		nextID := *cursor.Parent
		cursor, err = client.GetPartCategory(ctx, nextID)
		if err != nil {
			return inventree.Category{}, err
		}
		if cursor.PK != nextID {
			return inventree.Category{}, safeCategoryError("ancestor category identity verification")
		}
	}
	return inventree.Category{}, fmt.Errorf("%w of %d categories", errCategoryHierarchyDepth, categoryHierarchyDepthLimit)
}

func directCategoryPartCount(ctx context.Context, client CategoryAdminClient, categoryID int) (int, error) {
	cascade := false
	page, err := client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: categoryID, Cascade: &cascade, Limit: 1})
	return page.Count, err
}

func categoryPatch(input UpdatePartCategoryInput, before inventree.Category) (inventree.PatchFields, string, *int, error) {
	fields := inventree.PatchFields{}
	targetName := strings.TrimSpace(before.Name)
	targetParent := before.Parent
	if input.Name != nil {
		targetName = strings.TrimSpace(*input.Name)
		if targetName == "" {
			return nil, "", nil, errors.New("name must be nonblank when provided")
		}
		fields["name"] = inventree.Set(targetName)
	}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.ParentID != nil {
		targetParent = input.ParentID
		fields["parent"] = inventree.Set(*input.ParentID)
	} else if input.ClearParent {
		targetParent = nil
		fields["parent"] = inventree.Null()
	}
	if input.DefaultLocationID != nil {
		fields["default_location"] = inventree.Set(*input.DefaultLocationID)
	} else if input.ClearDefaultLocation {
		fields["default_location"] = inventree.Null()
	}
	if input.DefaultKeywords != nil {
		fields["default_keywords"] = inventree.Set(*input.DefaultKeywords)
	} else if input.ClearDefaultKeywords {
		fields["default_keywords"] = inventree.Null()
	}
	if input.Structural != nil {
		fields["structural"] = inventree.Set(*input.Structural)
	}
	if input.Icon != nil {
		fields["icon"] = inventree.Set(*input.Icon)
	} else if input.ClearIcon {
		fields["icon"] = inventree.Null()
	}
	return fields, targetName, targetParent, nil
}

func categoryClearConflict(input UpdatePartCategoryInput) string {
	switch {
	case input.ParentID != nil && input.ClearParent:
		return "parent_id and clear_parent are mutually exclusive"
	case input.DefaultLocationID != nil && input.ClearDefaultLocation:
		return "default_location_id and clear_default_location are mutually exclusive"
	case input.DefaultKeywords != nil && input.ClearDefaultKeywords:
		return "default_keywords and clear_default_keywords are mutually exclusive"
	case input.Icon != nil && input.ClearIcon:
		return "icon and clear_icon are mutually exclusive"
	default:
		return ""
	}
}

func recoverCategoryCreate(ctx context.Context, client CategoryAdminClient, input CreatePartCategoryInput, name string) (*mcp.CallToolResult, CategoryMutationOutput, error) {
	matches, err := categoryDuplicates(ctx, client, name, input.ParentID, 0)
	if err == nil && len(matches) == 1 {
		refreshed, readErr := client.GetPartCategory(ctx, matches[0].PK)
		if readErr == nil && refreshed.PK == matches[0].PK && categoryMatchesCreate(refreshed, input, name) {
			return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, Recovered: true, RecoveryPlan: fmt.Sprintf("The create response was lost, but unique same-parent recovery found category_id %d. Use this record and do not retry creation.", refreshed.PK)}, nil
		}
		if readErr == nil {
			if refreshed.PK != matches[0].PK {
				return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, RecoveryPlan: fmt.Sprintf("The create response was lost and unique candidate category_id %d returned a different detail identity. Call get_part_category for category_id %d before deciding whether any retry is safe.", matches[0].PK, matches[0].PK)}, nil
			}
			return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &refreshed, RecoveryPlan: fmt.Sprintf("The create response was lost and unique same-parent category_id %d does not match every supplied field. Review it before deciding whether any retry is safe.", refreshed.PK)}, nil
		}
	}
	if err == nil && len(matches) > 1 {
		clarification := NewClarification("Which recovered category is the intended create result?", "category_id", "the create result is unknown and multiple same-parent matches exist", "category_id", true, candidatesFor(matches), categoryCreateRetry(input, name))
		return TextResult(StatusClarificationRequired), CategoryMutationOutput{Status: StatusClarificationRequired, RecoveryPlan: "Read the candidates by stable ID before any retry.", Clarification: &clarification}, nil
	}
	return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "The create result is unknown. Search same-parent categories using the exact trimmed name and read any candidate by stable ID before retrying."}, nil
}

func recoverCategoryUpdate(ctx context.Context, client CategoryAdminClient, before inventree.Category, fields inventree.PatchFields, hierarchy CategoryHierarchyContext) (*mcp.CallToolResult, CategoryMutationOutput, error) {
	current, err := client.GetPartCategory(ctx, before.PK)
	if err == nil && current.PK == before.PK && categoryMatchesPatch(current, fields) {
		return TextResult(StatusPartialFailure), CategoryMutationOutput{Status: StatusPartialFailure, Record: &current, Before: &before, Hierarchy: &hierarchy, Recovered: true, RecoveryPlan: fmt.Sprintf("The PATCH response was lost, but category_id %d now matches every requested field. Use this record and do not replay the update.", current.PK)}, nil
	}
	out := CategoryMutationOutput{Status: StatusPartialFailure, Before: &before, Hierarchy: &hierarchy, RecoveryPlan: fmt.Sprintf("The PATCH result is unknown. Call get_part_category for category_id %d and compare every requested field before retrying.", before.PK)}
	if err == nil && current.PK == before.PK {
		out.Record = &current
	}
	return TextResult(StatusPartialFailure), out, nil
}

func categoryMatchesPatch(category inventree.Category, fields inventree.PatchFields) bool {
	payload, err := fields.MarshalJSON()
	if err != nil {
		return false
	}
	var patch map[string]json.RawMessage
	if json.Unmarshal(payload, &patch) != nil {
		return false
	}
	return patchStringMatches(patch, "name", category.Name) &&
		patchStringMatches(patch, "description", category.Description) &&
		patchBoolMatches(patch, "structural", category.Structural) &&
		patchNullableIntMatches(patch, "parent", category.Parent) &&
		patchNullableIntMatches(patch, "default_location", category.DefaultLocation) &&
		patchNullableStringMatches(patch, "default_keywords", category.DefaultKeywords) &&
		patchNullableStringMatches(patch, "icon", category.Icon)
}

func categoryMatchesCreate(category inventree.Category, input CreatePartCategoryInput, name string) bool {
	if category.Name != name || !sameOptionalInt(category.Parent, input.ParentID) {
		return false
	}
	if input.Description != nil && category.Description != *input.Description {
		return false
	}
	if input.DefaultLocationID != nil && !sameOptionalInt(category.DefaultLocation, input.DefaultLocationID) {
		return false
	}
	if input.DefaultKeywords != nil && !sameOptionalString(category.DefaultKeywords, input.DefaultKeywords) {
		return false
	}
	if input.Structural != nil && category.Structural != *input.Structural {
		return false
	}
	return input.Icon == nil || sameOptionalString(category.Icon, input.Icon)
}

func patchStringMatches(patch map[string]json.RawMessage, key, value string) bool {
	raw, ok := patch[key]
	if !ok {
		return true
	}
	var requested string
	return json.Unmarshal(raw, &requested) == nil && requested == value
}

func patchBoolMatches(patch map[string]json.RawMessage, key string, value bool) bool {
	raw, ok := patch[key]
	if !ok {
		return true
	}
	var requested bool
	return json.Unmarshal(raw, &requested) == nil && requested == value
}

func patchNullableIntMatches(patch map[string]json.RawMessage, key string, value *int) bool {
	raw, ok := patch[key]
	if !ok {
		return true
	}
	if string(raw) == "null" {
		return value == nil
	}
	var requested int
	return value != nil && json.Unmarshal(raw, &requested) == nil && requested == *value
}

func patchNullableStringMatches(patch map[string]json.RawMessage, key string, value *string) bool {
	raw, ok := patch[key]
	if !ok {
		return true
	}
	if string(raw) == "null" {
		return value == nil
	}
	var requested string
	return value != nil && json.Unmarshal(raw, &requested) == nil && requested == *value
}

func ambiguousCategoryMutation(err error) bool {
	var apiErr *inventree.APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return apiErr.StatusCode >= http.StatusInternalServerError || apiErr.StatusCode == http.StatusRequestTimeout || apiErr.StatusCode == http.StatusTooEarly || apiErr.StatusCode == http.StatusTooManyRequests
}

func categoryReferenceInvalid(err error) bool {
	var apiErr *inventree.APIError
	return errors.As(err, &apiErr) && (apiErr.Kind == inventree.ErrorKindNotFound || apiErr.Kind == inventree.ErrorKindValidation)
}

func safeCategoryError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}

func categoryParentPolicyError(err error) bool {
	return errors.Is(err, errCategorySelfParent) ||
		errors.Is(err, errCategoryDescendantParent) ||
		errors.Is(err, errCategoryExistingParentCycle) ||
		errors.Is(err, errCategoryHierarchyDepth)
}

func verifyCategoryPostWrite(ctx context.Context, client CategoryAdminClient, category inventree.Category) string {
	matches, err := categoryDuplicates(ctx, client, strings.TrimSpace(category.Name), category.Parent, category.PK)
	if err != nil {
		return fmt.Sprintf("Category %d was written, but post-write duplicate verification could not complete. Read the category and same-parent siblings before another write.", category.PK)
	}
	if len(matches) > 0 {
		return fmt.Sprintf("Category %d was written, but a concurrent same-parent duplicate is now present. Review category IDs before another write.", category.PK)
	}
	if category.Parent != nil {
		if _, err := validateCategoryParent(ctx, client, category.PK, *category.Parent); err != nil {
			return fmt.Sprintf("Category %d was written, but post-write hierarchy verification could not complete. Read the category and parent chain before another write.", category.PK)
		}
	}
	return ""
}

func categoryClarification(question, field, reason, retry string, hard bool, candidates []ClarificationCandidate, retryValues map[string]any) (*mcp.CallToolResult, CategoryMutationOutput, error) {
	clarification := NewClarification(question, field, reason, retry, hard, candidates, retryValues)
	return TextResult(StatusClarificationRequired), CategoryMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func categoryClarificationWithContext(question, field, reason, retry string, hard bool, candidates []ClarificationCandidate, retryValues map[string]any, before inventree.Category, hierarchy CategoryHierarchyContext) (*mcp.CallToolResult, CategoryMutationOutput, error) {
	clarification := NewClarification(question, field, reason, retry, hard, candidates, retryValues)
	return TextResult(StatusClarificationRequired), CategoryMutationOutput{Status: StatusClarificationRequired, Before: &before, Hierarchy: &hierarchy, Clarification: &clarification}, nil
}

func categoryCreateRetry(input CreatePartCategoryInput, name string) map[string]any {
	return map[string]any{"name": name, "description": input.Description, "parent_id": input.ParentID, "default_location_id": input.DefaultLocationID, "default_keywords": input.DefaultKeywords, "structural": input.Structural, "icon": input.Icon}
}

func categoryUpdateRetry(input UpdatePartCategoryInput, confirm bool) map[string]any {
	return map[string]any{"id": input.ID, "name": input.Name, "description": input.Description, "parent_id": input.ParentID, "clear_parent": input.ClearParent, "default_location_id": input.DefaultLocationID, "clear_default_location": input.ClearDefaultLocation, "default_keywords": input.DefaultKeywords, "clear_default_keywords": input.ClearDefaultKeywords, "structural": input.Structural, "icon": input.Icon, "clear_icon": input.ClearIcon, "confirm": confirm}
}

func sameOptionalInt(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameOptionalString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func optionalCount(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
