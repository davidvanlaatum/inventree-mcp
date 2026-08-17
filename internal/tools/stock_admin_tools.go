package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stockLocationScanLimit      = 1000
	stockLocationPageSize       = 100
	stockLocationHierarchyLimit = 100
)

var (
	errStockLocationScanLimit     = errors.New("stock-location duplicate scan safety limit exceeded")
	errStockLocationSelfParent    = errors.New("a stock location cannot be its own parent")
	errStockLocationParentCycle   = errors.New("a stock location cannot be moved beneath one of its descendants")
	errStockLocationDepth         = errors.New("stock-location parent validation exceeds the safety bound")
	errStockAdminInvalidReference = errors.New("stock administration reference is invalid")
)

type StockAdminClient interface {
	GetCompany(context.Context, int) (inventree.Company, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchStockLocationsPage(context.Context, inventree.StockLocationQuery) (inventree.StockLocationPage, error)
	GetStockLocationType(context.Context, int) (inventree.StockLocationType, error)
	CreateStockLocation(context.Context, inventree.StockLocationCreate) (inventree.StockLocation, error)
	UpdateStockLocation(context.Context, int, inventree.PatchFields) (inventree.StockLocation, error)
	GetStockItem(context.Context, int) (inventree.StockItem, error)
	UpdateStockItem(context.Context, int, inventree.PatchFields) (inventree.StockItem, error)
}

type CreateStockLocationInput struct {
	Name           string  `json:"name" jsonschema:"Required stock-location name; surrounding whitespace is removed."`
	Description    *string `json:"description,omitempty" jsonschema:"Optional description; an explicit empty string is preserved."`
	ParentID       *int    `json:"parent_id,omitempty" jsonschema:"Optional existing parent location. Omit for a root location."`
	OwnerID        *int    `json:"owner_id,omitempty" jsonschema:"Optional existing company owner primary key."`
	CustomIcon     *string `json:"custom_icon,omitempty" jsonschema:"Optional custom icon identifier; an explicit empty string is preserved."`
	Structural     *bool   `json:"structural,omitempty" jsonschema:"Optional structural flag; false is preserved."`
	External       *bool   `json:"external,omitempty" jsonschema:"Optional external-location flag; false is preserved."`
	LocationTypeID *int    `json:"location_type_id,omitempty" jsonschema:"Optional existing stock-location-type primary key."`
}

type UpdateStockLocationInput struct {
	ID                int     `json:"id" jsonschema:"Stable stock-location primary key."`
	Name              *string `json:"name,omitempty" jsonschema:"Optional replacement name; surrounding whitespace is removed."`
	Description       *string `json:"description,omitempty" jsonschema:"Optional replacement description; an explicit empty string is preserved."`
	OwnerID           *int    `json:"owner_id,omitempty" jsonschema:"Optional existing company owner primary key."`
	ClearOwner        bool    `json:"clear_owner,omitempty" jsonschema:"Explicitly clear owner; mutually exclusive with owner_id."`
	CustomIcon        *string `json:"custom_icon,omitempty" jsonschema:"Optional replacement custom icon; an explicit empty string is preserved."`
	ClearCustomIcon   bool    `json:"clear_custom_icon,omitempty" jsonschema:"Explicitly clear custom_icon; mutually exclusive with custom_icon."`
	LocationTypeID    *int    `json:"location_type_id,omitempty" jsonschema:"Optional existing stock-location-type primary key."`
	ClearLocationType bool    `json:"clear_location_type,omitempty" jsonschema:"Explicitly clear location_type; mutually exclusive with location_type_id."`
}

type RestructureStockLocationInput struct {
	ID          int    `json:"id" jsonschema:"Stable stock-location primary key."`
	ParentID    *int   `json:"parent_id,omitempty" jsonschema:"Optional existing non-descendant replacement parent."`
	ClearParent bool   `json:"clear_parent,omitempty" jsonschema:"Explicitly make the location a root; mutually exclusive with parent_id."`
	Structural  *bool  `json:"structural,omitempty" jsonschema:"Optional replacement structural flag."`
	External    *bool  `json:"external,omitempty" jsonschema:"Optional replacement external flag."`
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound restructuring plan without writing."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true to execute the reviewed restructuring plan."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Exact hash returned by dry_run:true for the current hierarchy state."`
}

type UpdateStockItemMetadataInput struct {
	ID              int     `json:"id" jsonschema:"Stable stock-item primary key."`
	Batch           *string `json:"batch,omitempty" jsonschema:"Optional replacement batch code; an explicit empty string is preserved."`
	ClearBatch      bool    `json:"clear_batch,omitempty" jsonschema:"Explicitly clear batch; mutually exclusive with batch."`
	ExpiryDate      *string `json:"expiry_date,omitempty" jsonschema:"Optional replacement expiry date in YYYY-MM-DD form."`
	ClearExpiryDate bool    `json:"clear_expiry_date,omitempty" jsonschema:"Explicitly clear expiry_date; mutually exclusive with expiry_date."`
	Packaging       *string `json:"packaging,omitempty" jsonschema:"Optional replacement packaging; an explicit empty string is preserved."`
	ClearPackaging  bool    `json:"clear_packaging,omitempty" jsonschema:"Explicitly clear packaging; mutually exclusive with packaging."`
	Notes           *string `json:"notes,omitempty" jsonschema:"Optional replacement notes; an explicit empty string is preserved."`
	ClearNotes      bool    `json:"clear_notes,omitempty" jsonschema:"Explicitly clear notes; mutually exclusive with notes."`
	Link            *string `json:"link,omitempty" jsonschema:"Optional complete HTTP(S) external link without userinfo; query parameters and fragments are preserved, and an explicit empty string clears it."`
	DryRun          bool    `json:"dry_run,omitempty" jsonschema:"Return a current-state-bound metadata plan without writing."`
	Confirm         bool    `json:"confirm,omitempty" jsonschema:"Required true to execute the reviewed metadata plan."`
	PlanHash        string  `json:"plan_hash,omitempty" jsonschema:"Exact hash returned by dry_run:true for the current stock state."`
}

type StockLocationPlan struct {
	Before       StockLocationPlanState    `json:"before"`
	After        StockLocationPlanState    `json:"after"`
	SourcePath   string                    `json:"source_path"`
	SourceTree   []inventree.TreePath      `json:"source_tree,omitempty"`
	TargetParent *StockLocationPlanContext `json:"target_parent,omitempty"`
}

type StockLocationPlanState struct {
	inventree.WebLinkFields
	ID         int  `json:"id"`
	ParentID   *int `json:"parent_id"`
	Structural bool `json:"structural"`
	External   bool `json:"external"`
}

type StockLocationPlanContext struct {
	inventree.WebLinkFields
	ID         int                  `json:"id"`
	Name       string               `json:"name"`
	PathString string               `json:"path"`
	ParentID   *int                 `json:"parent_id"`
	Path       []inventree.TreePath `json:"path_ids,omitempty"`
	Structural bool                 `json:"structural"`
	External   bool                 `json:"external"`
}

type StockMetadataState struct {
	inventree.WebLinkFields
	ID              int                      `json:"id"`
	PartID          int                      `json:"part_id"`
	LocationID      *int                     `json:"location_id"`
	Quantity        float64                  `json:"quantity"`
	Serial          *string                  `json:"serial"`
	Status          int                      `json:"status"`
	StatusText      *string                  `json:"status_text"`
	StatusCustomKey *int                     `json:"status_custom_key"`
	DeleteOnDeplete bool                     `json:"delete_on_deplete"`
	Allocated       *float64                 `json:"allocated,omitempty"`
	OwnerID         *int                     `json:"owner_id"`
	SupplierPartID  *int                     `json:"supplier_part_id"`
	BuildID         *int                     `json:"build_id"`
	ConsumedByID    *int                     `json:"consumed_by_id"`
	CustomerID      *int                     `json:"customer_id"`
	SalesOrderID    *int                     `json:"sales_order_id"`
	BelongsToID     *int                     `json:"belongs_to_id"`
	ParentID        *int                     `json:"parent_id"`
	PurchaseOrderID *int                     `json:"purchase_order_id"`
	PurchasePrice   *inventree.DecimalString `json:"purchase_price"`
	PriceCurrency   string                   `json:"purchase_price_currency"`
	InStock         bool                     `json:"in_stock"`
	IsBuilding      bool                     `json:"is_building"`
	Batch           *string                  `json:"batch"`
	ExpiryDate      *string                  `json:"expiry_date"`
	Packaging       *string                  `json:"packaging"`
	Notes           *string                  `json:"notes"`
	Link            string                   `json:"link"`
}

type StockMetadataPlan struct {
	Before StockMetadataState `json:"before"`
	After  StockMetadataState `json:"after"`
}

type StockLocationMutationOutput struct {
	Status        string                   `json:"status"`
	DryRun        bool                     `json:"dry_run,omitempty"`
	Record        *inventree.StockLocation `json:"record,omitempty"`
	Plan          *StockLocationPlan       `json:"plan,omitempty"`
	PlanHash      string                   `json:"plan_hash,omitempty"`
	Recovered     bool                     `json:"recovered,omitempty"`
	RecoveryPlan  string                   `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse   `json:"clarification,omitempty"`
}

type StockMetadataMutationOutput struct {
	Status        string                 `json:"status"`
	DryRun        bool                   `json:"dry_run,omitempty"`
	Record        *inventree.StockItem   `json:"record,omitempty"`
	Plan          *StockMetadataPlan     `json:"plan,omitempty"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Recovered     bool                   `json:"recovered,omitempty"`
	RecoveryPlan  string                 `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

func registerStockAdminTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreateStockLocationToolName, "Create stock location", "Creates a guarded stock location after bounded same-parent duplicate and reference checks.", createStockLocation(deps))
	addWriteTool(server, deps, UpdateStockLocationToolName, "Update stock location", "Updates ordinary stock-location metadata without changing hierarchy, structural, or external state.", updateStockLocation(deps))
	addWriteTool(server, deps, RestructureStockLocationToolName, "Restructure stock location", "Plans or confirms operational parent, structural, or external changes for one stock location.", restructureStockLocation(deps))
	addWriteTool(server, deps, UpdateStockItemMetadataToolName, "Update stock item metadata", "Plans or confirms a constrained non-location stock metadata update.", updateStockItemMetadata(deps))
}

func createStockLocation(deps Dependencies) mcp.ToolHandlerFor[CreateStockLocationInput, StockLocationMutationOutput] {
	return LookupHandler[StockAdminClient, CreateStockLocationInput, StockLocationMutationOutput](deps, CreateStockLocationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdminClient, input CreateStockLocationInput) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
			name := strings.TrimSpace(input.Name)
			if name == "" {
				return stockLocationClarification("What nonblank name should the stock location use?", "name", "name is required", "name", map[string]any{"name": name})
			}
			if err := validateLocationReferences(ctx, client, input.ParentID, input.OwnerID, input.LocationTypeID); err != nil {
				if errors.Is(err, errStockAdminInvalidReference) {
					return stockLocationClarification("Which existing stock-location references should be used?", "reference", err.Error(), "id", stockLocationCreateRetry(input, name))
				}
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location reference lookup")
			}
			matches, err := stockLocationDuplicates(ctx, client, name, input.ParentID, 0)
			if err != nil {
				return stockLocationDuplicateFailure(err, stockLocationCreateRetry(input, name))
			}
			if len(matches) > 0 {
				return stockLocationCandidates("Should an existing same-parent stock location be used?", "a trimmed case-insensitive same-parent location already exists", matches, stockLocationCreateRetry(input, name))
			}
			created, err := client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: name, Description: input.Description, Parent: input.ParentID, Owner: input.OwnerID, CustomIcon: input.CustomIcon, Structural: input.Structural, External: input.External, LocationType: input.LocationTypeID})
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location creation")
				}
				return recoverStockLocationCreate(ctx, client, input, name)
			}
			return verifyStockLocationWrite(ctx, client, created.PK, nil, locationCreateFields(input, name), false)
		})
}

func updateStockLocation(deps Dependencies) mcp.ToolHandlerFor[UpdateStockLocationInput, StockLocationMutationOutput] {
	return LookupHandler[StockAdminClient, UpdateStockLocationInput, StockLocationMutationOutput](deps, UpdateStockLocationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdminClient, input UpdateStockLocationInput) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
			if input.ID <= 0 {
				return stockLocationClarification("Which stock location should be updated?", "id", "id must be positive", "id", map[string]any{"id": input.ID})
			}
			before, err := client.GetStockLocation(ctx, input.ID)
			if err != nil || before.PK != input.ID {
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location lookup")
			}
			fields, name, err := ordinaryLocationPatch(input, before)
			if err != nil {
				return stockLocationClarification("Which ordinary stock-location fields should change?", "patch", err.Error(), "id", map[string]any{"id": input.ID})
			}
			if err := validateLocationReferences(ctx, client, nil, input.OwnerID, input.LocationTypeID); err != nil {
				if errors.Is(err, errStockAdminInvalidReference) {
					return stockLocationClarification("Which existing owner or location type should be used?", "reference", err.Error(), "id", map[string]any{"id": input.ID})
				}
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location reference lookup")
			}
			if name != before.Name {
				matches, duplicateErr := stockLocationDuplicates(ctx, client, name, before.Parent, input.ID)
				if duplicateErr != nil {
					return stockLocationDuplicateFailure(duplicateErr, map[string]any{"id": input.ID})
				}
				if len(matches) > 0 {
					return stockLocationCandidates("Which same-parent stock location should remain?", "the requested name collides with an existing same-parent location", matches, map[string]any{"id": input.ID})
				}
			}
			updated, err := client.UpdateStockLocation(ctx, input.ID, fields)
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location update")
				}
				return verifyStockLocationWrite(ctx, client, input.ID, &before, fields, true)
			}
			if updated.PK != input.ID {
				return stockLocationPartial(before, fmt.Sprintf("PATCH for location_id %d returned a mismatched identity; read the stable ID before retrying", input.ID))
			}
			return verifyStockLocationWrite(ctx, client, input.ID, &before, fields, false)
		})
}

func restructureStockLocation(deps Dependencies) mcp.ToolHandlerFor[RestructureStockLocationInput, StockLocationMutationOutput] {
	return LookupHandler[StockAdminClient, RestructureStockLocationInput, StockLocationMutationOutput](deps, RestructureStockLocationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdminClient, input RestructureStockLocationInput) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
			if input.ID <= 0 || (input.ParentID != nil && input.ClearParent) {
				return stockLocationClarification("Which valid location restructuring should be planned?", "patch", "a positive id is required and parent_id conflicts with clear_parent", "id", map[string]any{"id": input.ID})
			}
			before, err := client.GetStockLocation(ctx, input.ID)
			if err != nil || before.PK != input.ID {
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location lookup")
			}
			fields := inventree.PatchFields{}
			after := before
			var targetParent *StockLocationPlanContext
			if input.ParentID != nil {
				parent, parentErr := validateStockLocationParent(ctx, client, input.ID, *input.ParentID)
				if parentErr != nil {
					if errors.Is(parentErr, errStockAdminInvalidReference) || errors.Is(parentErr, errStockLocationSelfParent) || errors.Is(parentErr, errStockLocationParentCycle) || errors.Is(parentErr, errStockLocationDepth) {
						return stockLocationClarification("Which non-descendant parent should be used?", "parent_id", parentErr.Error(), "parent_id", map[string]any{"id": input.ID})
					}
					return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location parent validation")
				}
				fields["parent"] = inventree.Set(parent.PK)
				after.Parent = &parent.PK
				targetParent = &StockLocationPlanContext{ID: parent.PK, Name: parent.Name, PathString: parent.PathString, ParentID: parent.Parent, Path: parent.Path, Structural: parent.Structural, External: parent.External}
			} else if input.ClearParent {
				fields["parent"] = inventree.Null()
				after.Parent = nil
			}
			if input.Structural != nil {
				fields["structural"] = inventree.Set(*input.Structural)
				after.Structural = *input.Structural
			}
			if input.External != nil {
				fields["external"] = inventree.Set(*input.External)
				after.External = *input.External
			}
			if len(fields) == 0 || (sameOptionalInt(after.Parent, before.Parent) && after.Structural == before.Structural && after.External == before.External) {
				return stockLocationClarification("Which hierarchy, structural, or external state should change?", "patch", "the requested restructuring is empty or a no-op", "id", map[string]any{"id": input.ID})
			}
			matches, duplicateErr := stockLocationDuplicates(ctx, client, before.Name, after.Parent, input.ID)
			if duplicateErr != nil {
				return stockLocationDuplicateFailure(duplicateErr, map[string]any{"id": input.ID})
			}
			if len(matches) > 0 {
				return stockLocationCandidates("Which same-parent stock location should remain?", "the requested parent creates a same-name collision", matches, map[string]any{"id": input.ID})
			}
			plan := StockLocationPlan{Before: stockLocationPlanState(before), After: stockLocationPlanState(after), SourcePath: before.PathString, SourceTree: before.Path, TargetParent: targetParent}
			hash, hashErr := stockAdminPlanHash(plan)
			if hashErr != nil {
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location plan")
			}
			out := StockLocationMutationOutput{Status: StatusOK, DryRun: input.DryRun, Plan: &plan, PlanHash: hash}
			if input.DryRun {
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm || input.PlanHash != hash {
				return stockLocationPlanClarification(out, "Run dry_run:true and provide its current plan_hash with confirm:true")
			}
			_, err = client.UpdateStockLocation(ctx, input.ID, fields)
			if err != nil && !ambiguousCategoryMutation(err) {
				return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location restructuring")
			}
			return verifyStockLocationWrite(ctx, client, input.ID, &before, fields, err != nil)
		})
}

func updateStockItemMetadata(deps Dependencies) mcp.ToolHandlerFor[UpdateStockItemMetadataInput, StockMetadataMutationOutput] {
	return LookupHandler[StockAdminClient, UpdateStockItemMetadataInput, StockMetadataMutationOutput](deps, UpdateStockItemMetadataToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdminClient, input UpdateStockItemMetadataInput) (*mcp.CallToolResult, StockMetadataMutationOutput, error) {
			if input.ID <= 0 {
				return stockMetadataClarification("Which stock item should be updated?", "id", "id must be positive", "id", map[string]any{"id": input.ID})
			}
			before, err := client.GetStockItem(ctx, input.ID)
			if err != nil || before.PK != input.ID {
				return nil, StockMetadataMutationOutput{}, safeStockAdminError("stock-item lookup")
			}
			fields, after, err := stockMetadataPatch(input, before)
			if err != nil {
				return stockMetadataClarification("Which approved stock metadata should change?", "patch", err.Error(), "id", map[string]any{"id": input.ID})
			}
			plan := StockMetadataPlan{Before: stockMetadataState(before), After: stockMetadataState(after)}
			hash, hashErr := stockAdminPlanHash(plan)
			if hashErr != nil {
				return nil, StockMetadataMutationOutput{}, safeStockAdminError("stock metadata plan")
			}
			publicPlan := projectStockMetadataPlan(plan)
			out := StockMetadataMutationOutput{Status: StatusOK, DryRun: input.DryRun, Plan: &publicPlan, PlanHash: hash}
			if input.DryRun {
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm || input.PlanHash != hash {
				return stockMetadataPlanClarification(out, "Run dry_run:true and provide its current plan_hash with confirm:true")
			}
			_, err = client.UpdateStockItem(ctx, input.ID, fields)
			if err != nil && !ambiguousCategoryMutation(err) {
				return nil, StockMetadataMutationOutput{}, safeStockAdminError("stock metadata update")
			}
			current, readErr := client.GetStockItem(ctx, input.ID)
			if readErr != nil || current.PK != input.ID {
				out.Status = StatusPartialFailure
				redactStockMetadataOutputURLs(&out)
				out.RecoveryPlan = fmt.Sprintf("Stock item %d may have changed; call get_stock_item before preparing a new plan", input.ID)
				return TextResult(StatusPartialFailure), out, nil
			}
			if !stockMetadataStateEqual(stockMetadataState(current), plan.After) {
				out.Status = StatusPartialFailure
				recovery := stockItemRecoveryProjection(current)
				out.Record = &recovery
				redactStockMetadataOutputURLs(&out)
				out.RecoveryPlan = fmt.Sprintf("Stock item %d read-back does not match the reviewed plan; inspect it before another write", input.ID)
				return TextResult(StatusPartialFailure), out, nil
			}
			sanitized := sanitizedStockItem(current)
			out.Record = &sanitized
			out.Recovered = err != nil
			return TextResult(StatusOK), out, nil
		})
}

func validateLocationReferences(ctx context.Context, client StockAdminClient, parentID, ownerID, typeID *int) error {
	for label, ref := range map[string]*int{"parent_id": parentID, "owner_id": ownerID, "location_type_id": typeID} {
		if ref != nil && *ref <= 0 {
			return fmt.Errorf("%w: %s must be positive when provided", errStockAdminInvalidReference, label)
		}
	}
	if parentID != nil {
		value, err := client.GetStockLocation(ctx, *parentID)
		if err != nil {
			if categoryReferenceInvalid(err) {
				return fmt.Errorf("%w: parent_id %d does not identify a readable stock location", errStockAdminInvalidReference, *parentID)
			}
			return err
		}
		if value.PK != *parentID {
			return fmt.Errorf("%w: parent_id %d returned a mismatched identity", errStockAdminInvalidReference, *parentID)
		}
	}
	if ownerID != nil {
		value, err := client.GetCompany(ctx, *ownerID)
		if err != nil {
			if categoryReferenceInvalid(err) {
				return fmt.Errorf("%w: owner_id %d does not identify a readable company", errStockAdminInvalidReference, *ownerID)
			}
			return err
		}
		if value.PK != *ownerID {
			return fmt.Errorf("%w: owner_id %d returned a mismatched identity", errStockAdminInvalidReference, *ownerID)
		}
	}
	if typeID != nil {
		value, err := client.GetStockLocationType(ctx, *typeID)
		if err != nil {
			if categoryReferenceInvalid(err) {
				return fmt.Errorf("%w: location_type_id %d does not identify a readable stock location type", errStockAdminInvalidReference, *typeID)
			}
			return err
		}
		if value.PK != *typeID {
			return fmt.Errorf("%w: location_type_id %d returned a mismatched identity", errStockAdminInvalidReference, *typeID)
		}
	}
	return nil
}

func stockLocationDuplicates(ctx context.Context, client StockAdminClient, name string, parent *int, exclude int) ([]inventree.StockLocation, error) {
	matches := make([]inventree.StockLocation, 0)
	for offset := 0; offset < stockLocationScanLimit; offset += stockLocationPageSize {
		query := inventree.StockLocationQuery{Search: name, Parent: parent, Limit: stockLocationPageSize, Offset: offset}
		if parent == nil {
			root := true
			query.TopLevel = &root
		}
		page, err := client.SearchStockLocationsPage(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, location := range page.Results {
			if location.PK != exclude && sameOptionalInt(location.Parent, parent) && strings.EqualFold(strings.TrimSpace(location.Name), strings.TrimSpace(name)) {
				matches = append(matches, location)
			}
		}
		if !page.HasMore {
			slices.SortFunc(matches, func(a, b inventree.StockLocation) int { return a.PK - b.PK })
			return matches, nil
		}
	}
	return nil, errStockLocationScanLimit
}

func validateStockLocationParent(ctx context.Context, client StockAdminClient, locationID, parentID int) (inventree.StockLocation, error) {
	if parentID <= 0 {
		return inventree.StockLocation{}, fmt.Errorf("%w: parent_id must be positive", errStockAdminInvalidReference)
	}
	if locationID == parentID {
		return inventree.StockLocation{}, errStockLocationSelfParent
	}
	seen := map[int]bool{}
	currentID := parentID
	var first inventree.StockLocation
	for depth := 0; depth < stockLocationHierarchyLimit; depth++ {
		if currentID == locationID || seen[currentID] {
			return inventree.StockLocation{}, errStockLocationParentCycle
		}
		seen[currentID] = true
		current, err := client.GetStockLocation(ctx, currentID)
		if err != nil {
			if categoryReferenceInvalid(err) {
				return inventree.StockLocation{}, fmt.Errorf("%w: parent_id %d does not identify a readable stock location", errStockAdminInvalidReference, currentID)
			}
			return inventree.StockLocation{}, err
		}
		if current.PK != currentID {
			return inventree.StockLocation{}, fmt.Errorf("%w: parent_id %d returned a mismatched identity", errStockAdminInvalidReference, currentID)
		}
		if depth == 0 {
			first = current
		}
		if current.Parent == nil {
			return first, nil
		}
		currentID = *current.Parent
	}
	return inventree.StockLocation{}, errStockLocationDepth
}

func ordinaryLocationPatch(input UpdateStockLocationInput, before inventree.StockLocation) (inventree.PatchFields, string, error) {
	if input.OwnerID != nil && input.ClearOwner || input.CustomIcon != nil && input.ClearCustomIcon || input.LocationTypeID != nil && input.ClearLocationType {
		return nil, "", errors.New("replacement values conflict with their clear flags")
	}
	fields := inventree.PatchFields{}
	name := before.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, "", errors.New("name must be nonblank")
		}
		fields["name"] = inventree.Set(name)
	}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	setNullableIntPatch(fields, "owner", input.OwnerID, input.ClearOwner)
	setNullableStringPatch(fields, "custom_icon", input.CustomIcon, input.ClearCustomIcon)
	setNullableIntPatch(fields, "location_type", input.LocationTypeID, input.ClearLocationType)
	if len(fields) == 0 {
		return nil, "", errors.New("at least one ordinary metadata field is required")
	}
	return fields, name, nil
}

func locationCreateFields(input CreateStockLocationInput, name string) inventree.PatchFields {
	fields := inventree.PatchFields{"name": inventree.Set(name)}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.ParentID != nil {
		fields["parent"] = inventree.Set(*input.ParentID)
	}
	if input.OwnerID != nil {
		fields["owner"] = inventree.Set(*input.OwnerID)
	}
	if input.CustomIcon != nil {
		fields["custom_icon"] = inventree.Set(*input.CustomIcon)
	}
	if input.Structural != nil {
		fields["structural"] = inventree.Set(*input.Structural)
	}
	if input.External != nil {
		fields["external"] = inventree.Set(*input.External)
	}
	if input.LocationTypeID != nil {
		fields["location_type"] = inventree.Set(*input.LocationTypeID)
	}
	return fields
}

func stockMetadataPatch(input UpdateStockItemMetadataInput, before inventree.StockItem) (inventree.PatchFields, inventree.StockItem, error) {
	if input.Batch != nil && input.ClearBatch || input.ExpiryDate != nil && input.ClearExpiryDate || input.Packaging != nil && input.ClearPackaging || input.Notes != nil && input.ClearNotes {
		return nil, before, errors.New("replacement values conflict with their clear flags")
	}
	if input.ExpiryDate != nil && !validDate(*input.ExpiryDate) {
		return nil, before, errors.New("expiry_date must use YYYY-MM-DD")
	}
	if input.Link != nil {
		link, err := validateExternalURL(*input.Link)
		if err != nil {
			return nil, before, errors.New("link must be an HTTP(S) URL without credentials or an explicit empty string")
		}
		*input.Link = link
	}
	fields := inventree.PatchFields{}
	after := before
	applyNullableString := func(key string, value **string, replacement *string, clear bool) {
		if replacement != nil {
			fields[key] = inventree.Set(*replacement)
			copied := *replacement
			*value = &copied
		}
		if clear {
			fields[key] = inventree.Null()
			*value = nil
		}
	}
	applyNullableString("batch", &after.Batch, input.Batch, input.ClearBatch)
	applyNullableString("expiry_date", &after.ExpiryDate, input.ExpiryDate, input.ClearExpiryDate)
	applyNullableString("packaging", &after.Packaging, input.Packaging, input.ClearPackaging)
	applyNullableString("notes", &after.Notes, input.Notes, input.ClearNotes)
	if input.Link != nil {
		fields["link"] = inventree.Set(*input.Link)
		after.Link = *input.Link
	}
	if len(fields) == 0 || stockMetadataStateEqual(stockMetadataState(before), stockMetadataState(after)) {
		return nil, before, errors.New("at least one non-no-op approved metadata field is required")
	}
	return fields, after, nil
}

func verifyStockLocationWrite(ctx context.Context, client StockAdminClient, id int, before *inventree.StockLocation, fields inventree.PatchFields, recovered bool) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	current, err := client.GetStockLocation(ctx, id)
	if err != nil || current.PK != id {
		base := inventree.StockLocation{PK: id}
		if before != nil {
			base = *before
		}
		return stockLocationPartial(base, fmt.Sprintf("Location %d may have changed; call get_stock_location before retrying", id))
	}
	if !locationFieldsMatch(current, fields) {
		return stockLocationPartial(current, fmt.Sprintf("Location %d read-back does not match the requested fields; inspect it before another write", id))
	}
	matches, duplicateErr := stockLocationDuplicates(ctx, client, current.Name, current.Parent, current.PK)
	if duplicateErr != nil || len(matches) > 0 {
		return stockLocationPartial(current, fmt.Sprintf("Location %d was written but same-parent uniqueness could not be verified; inspect siblings before another write", id))
	}
	return TextResult(StatusOK), StockLocationMutationOutput{Status: StatusOK, Record: &current, Recovered: recovered}, nil
}

func recoverStockLocationCreate(ctx context.Context, client StockAdminClient, input CreateStockLocationInput, name string) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	matches, err := stockLocationDuplicates(ctx, client, name, input.ParentID, 0)
	if err != nil || len(matches) != 1 || !locationFieldsMatch(matches[0], locationCreateFields(input, name)) {
		return TextResult(StatusPartialFailure), StockLocationMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "Creation result is unknown. Search the exact parent for the requested normalized name and inspect every supplied field before retrying."}, nil
	}
	return verifyStockLocationWrite(ctx, client, matches[0].PK, nil, locationCreateFields(input, name), true)
}

func locationFieldsMatch(location inventree.StockLocation, fields inventree.PatchFields) bool {
	data, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	var patch map[string]json.RawMessage
	if json.Unmarshal(data, &patch) != nil {
		return false
	}
	checks := map[string]any{"name": location.Name, "description": location.Description, "parent": location.Parent, "owner": location.Owner, "custom_icon": location.CustomIcon, "structural": location.Structural, "external": location.External, "location_type": location.LocationType}
	for key, raw := range patch {
		actual, ok := checks[key]
		if !ok {
			return false
		}
		actualJSON, _ := json.Marshal(actual)
		if string(actualJSON) != string(raw) {
			return false
		}
	}
	return true
}

func stockMetadataState(item inventree.StockItem) StockMetadataState {
	return StockMetadataState{ID: item.PK, PartID: item.Part, LocationID: item.Location, Quantity: item.Quantity, Serial: item.Serial, Status: item.Status, StatusText: item.StatusText, StatusCustomKey: item.StatusCustomKey, DeleteOnDeplete: item.DeleteOnDeplete, Allocated: item.Allocated, OwnerID: item.Owner, SupplierPartID: item.SupplierPart, BuildID: item.Build, ConsumedByID: item.ConsumedBy, CustomerID: item.Customer, SalesOrderID: item.SalesOrder, BelongsToID: item.BelongsTo, ParentID: item.Parent, PurchaseOrderID: item.PurchaseOrder, PurchasePrice: item.PurchasePrice, PriceCurrency: item.PurchasePriceCurrency, InStock: item.InStock, IsBuilding: item.IsBuilding, Batch: item.Batch, ExpiryDate: item.ExpiryDate, Packaging: item.Packaging, Notes: item.Notes, Link: item.Link}
}

func stockLocationPlanState(location inventree.StockLocation) StockLocationPlanState {
	return StockLocationPlanState{ID: location.PK, ParentID: location.Parent, Structural: location.Structural, External: location.External}
}

func sanitizedStockItem(item inventree.StockItem) inventree.StockItem {
	if item.Link != "" {
		item.Link = projectExternalURL(&item.Link)
	}
	return item
}

func sanitizedStockItemDetail(item inventree.StockItemDetail) inventree.StockItemDetail {
	if item.Link != "" {
		item.Link = projectExternalURL(&item.Link)
	}
	return item
}

func projectStockMetadataPlan(plan StockMetadataPlan) StockMetadataPlan {
	plan.Before.Link = projectExternalURL(&plan.Before.Link)
	plan.After.Link = projectExternalURL(&plan.After.Link)
	return plan
}

func stockItemRecoveryProjection(item inventree.StockItem) inventree.StockItem {
	item.Link = ""
	return item
}

func redactStockMetadataOutputURLs(out *StockMetadataMutationOutput) {
	if out.Plan != nil {
		out.Plan.Before.Link = ""
		out.Plan.After.Link = ""
	}
	if out.Record != nil {
		out.Record.Link = ""
	}
}

func stockMetadataStateEqual(a, b StockMetadataState) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func stockAdminPlanHash(plan any) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func setNullableIntPatch(fields inventree.PatchFields, key string, value *int, clear bool) {
	if value != nil {
		fields[key] = inventree.Set(*value)
	}
	if clear {
		fields[key] = inventree.Null()
	}
}

func setNullableStringPatch(fields inventree.PatchFields, key string, value *string, clear bool) {
	if value != nil {
		fields[key] = inventree.Set(*value)
	}
	if clear {
		fields[key] = inventree.Null()
	}
}

func stockLocationCreateRetry(input CreateStockLocationInput, name string) map[string]any {
	return map[string]any{"name": name, "parent_id": input.ParentID, "owner_id": input.OwnerID, "location_type_id": input.LocationTypeID}
}

func stockLocationClarification(question, field, reason, retry string, values map[string]any) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, values)
	return TextResult(StatusClarificationRequired), StockLocationMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func stockLocationCandidates(question, reason string, matches []inventree.StockLocation, values map[string]any) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	clarification := NewClarification(question, "stock_location", reason, "location_id", false, candidatesFor(matches), values)
	return TextResult(StatusClarificationRequired), StockLocationMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func stockLocationDuplicateFailure(err error, values map[string]any) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	if errors.Is(err, errStockLocationScanLimit) {
		return stockLocationClarification("How should this completeness-sensitive duplicate scan be narrowed?", "stock_location", err.Error(), "parent_id", values)
	}
	return nil, StockLocationMutationOutput{}, safeStockAdminError("stock-location duplicate preflight")
}

func stockLocationPlanClarification(out StockLocationMutationOutput, reason string) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	clarification := NewClarification("Apply this reviewed stock-location restructuring?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"dry_run": true})
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func stockMetadataClarification(question, field, reason, retry string, values map[string]any) (*mcp.CallToolResult, StockMetadataMutationOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, values)
	return TextResult(StatusClarificationRequired), StockMetadataMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func stockMetadataPlanClarification(out StockMetadataMutationOutput, reason string) (*mcp.CallToolResult, StockMetadataMutationOutput, error) {
	clarification := NewClarification("Apply this reviewed stock metadata update?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"dry_run": true})
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	redactStockMetadataOutputURLs(&out)
	return TextResult(StatusClarificationRequired), out, nil
}

func stockLocationPartial(record inventree.StockLocation, recovery string) (*mcp.CallToolResult, StockLocationMutationOutput, error) {
	return TextResult(StatusPartialFailure), StockLocationMutationOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: recovery}, nil
}

func safeStockAdminError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
