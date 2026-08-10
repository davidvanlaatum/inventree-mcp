package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const companyAdminScanLimit = 1000

type CompanyAdminClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error)
	SearchCompaniesPage(context.Context, inventree.SearchQuery) (inventree.CompanyPage, error)
	GetSupplierPartDetail(context.Context, int) (inventree.SupplierPartDetail, error)
	SearchSupplierPartsPage(context.Context, inventree.SupplierPartQuery) (inventree.SupplierPartPage, error)
	GetManufacturerPartDetail(context.Context, int) (inventree.ManufacturerPartDetail, error)
	SearchManufacturerPartsPage(context.Context, inventree.ManufacturerPartQuery) (inventree.ManufacturerPartPage, error)
	UpdateCompany(context.Context, int, inventree.PatchFields) (inventree.CompanyDetail, error)
	UpdateSupplierPart(context.Context, int, inventree.PatchFields) (inventree.SupplierPartDetail, error)
	UpdateManufacturerPart(context.Context, int, inventree.PatchFields) (inventree.ManufacturerPartDetail, error)
}

type CompanyView struct {
	inventree.WebLinkFields
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Website        string  `json:"website"`
	Currency       string  `json:"currency"`
	Active         bool    `json:"active"`
	IsSupplier     bool    `json:"is_supplier"`
	IsManufacturer bool    `json:"is_manufacturer"`
	IsCustomer     bool    `json:"is_customer"`
	Notes          *string `json:"notes,omitempty"`
}

type CompanyRecoveryView struct {
	inventree.WebLinkFields
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Currency       string `json:"currency"`
	Active         bool   `json:"active"`
	IsSupplier     bool   `json:"is_supplier"`
	IsManufacturer bool   `json:"is_manufacturer"`
	IsCustomer     bool   `json:"is_customer"`
}

type SupplierPartView struct {
	inventree.WebLinkFields
	ID                 int     `json:"id"`
	PartID             int     `json:"part_id"`
	SupplierID         int     `json:"supplier_id"`
	SKU                string  `json:"sku"`
	Description        *string `json:"description"`
	Link               string  `json:"link,omitempty"`
	Active             bool    `json:"active"`
	Primary            bool    `json:"primary"`
	ManufacturerPartID *int    `json:"manufacturer_part_id,omitempty"`
	Packaging          *string `json:"packaging,omitempty"`
	PackQuantity       string  `json:"pack_quantity"`
	Note               *string `json:"note,omitempty"`
}

type SupplierPartRecoveryView struct {
	inventree.WebLinkFields
	ID                 int    `json:"id"`
	PartID             int    `json:"part_id"`
	SupplierID         int    `json:"supplier_id"`
	SKU                string `json:"sku"`
	Active             bool   `json:"active"`
	Primary            bool   `json:"primary"`
	ManufacturerPartID *int   `json:"manufacturer_part_id,omitempty"`
}

type ManufacturerPartView struct {
	inventree.WebLinkFields
	ID             int     `json:"id"`
	PartID         int     `json:"part_id"`
	ManufacturerID int     `json:"manufacturer_id"`
	MPN            *string `json:"mpn"`
	Description    *string `json:"description"`
	Link           string  `json:"link,omitempty"`
}

type ManufacturerPartRecoveryView struct {
	inventree.WebLinkFields
	ID             int     `json:"id"`
	PartID         int     `json:"part_id"`
	ManufacturerID int     `json:"manufacturer_id"`
	MPN            *string `json:"mpn"`
}

type CompanyAdminSearchOutput[T any] struct {
	Status  string `json:"status"`
	Count   int    `json:"count"`
	Results []T    `json:"results"`
}

type CompanyAdminRecordOutput[T any] struct {
	Status string `json:"status"`
	Record *T     `json:"record,omitempty"`
}

type CompanyMutationOutput[T any, R any] struct {
	Status                string                     `json:"status"`
	Record                *T                         `json:"record,omitempty"`
	Before                *R                         `json:"before,omitempty"`
	Current               *R                         `json:"current,omitempty"`
	Candidates            []R                        `json:"candidates,omitempty"`
	BlockingSupplierParts []SupplierPartRecoveryView `json:"blocking_supplier_parts,omitempty"`
	Recovered             bool                       `json:"recovered,omitempty"`
	RecoveryPlan          string                     `json:"recovery_plan,omitempty"`
}

type SupplierPartSearchInput struct {
	Search     string `json:"search,omitempty" jsonschema:"Optional free-text search."`
	PartID     int    `json:"part_id,omitempty" jsonschema:"Optional exact base-part primary key."`
	SupplierID int    `json:"supplier_id,omitempty" jsonschema:"Optional exact supplier company primary key."`
	SKU        string `json:"sku,omitempty" jsonschema:"Optional exact supplier SKU."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum results from 1 to 100; defaults to 20."`
	Offset     int    `json:"offset,omitempty" jsonschema:"Non-negative pagination offset."`
}

type ManufacturerPartSearchInput struct {
	Search         string `json:"search,omitempty" jsonschema:"Optional free-text search."`
	PartID         int    `json:"part_id,omitempty" jsonschema:"Optional exact base-part primary key."`
	ManufacturerID int    `json:"manufacturer_id,omitempty" jsonschema:"Optional exact manufacturer company primary key."`
	MPN            string `json:"mpn,omitempty" jsonschema:"Optional exact manufacturer MPN."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum results from 1 to 100; defaults to 20."`
	Offset         int    `json:"offset,omitempty" jsonschema:"Non-negative pagination offset."`
}

type UpdateCompanyInput struct {
	ID             int     `json:"id" jsonschema:"Stable company primary key."`
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Website        *string `json:"website,omitempty"`
	Currency       *string `json:"currency,omitempty"`
	Active         *bool   `json:"active,omitempty"`
	IsSupplier     *bool   `json:"is_supplier,omitempty"`
	IsManufacturer *bool   `json:"is_manufacturer,omitempty"`
	Notes          *string `json:"notes,omitempty" jsonschema:"Replacement markdown notes. Notes are never included in recovery or error projections."`
	ClearNotes     bool    `json:"clear_notes,omitempty" jsonschema:"Explicitly PATCH notes to null; mutually exclusive with notes."`
	Confirm        bool    `json:"confirm,omitempty" jsonschema:"Required when removing supplier or manufacturer role."`
}

type UpdateSupplierPartInput struct {
	ID                    int     `json:"id"`
	PartID                *int    `json:"part_id,omitempty"`
	SupplierID            *int    `json:"supplier_id,omitempty"`
	SKU                   *string `json:"sku,omitempty"`
	Description           *string `json:"description,omitempty"`
	ClearDescription      bool    `json:"clear_description,omitempty"`
	Link                  *string `json:"link,omitempty"`
	ClearLink             bool    `json:"clear_link,omitempty"`
	Active                *bool   `json:"active,omitempty"`
	Primary               *bool   `json:"primary,omitempty"`
	ManufacturerPartID    *int    `json:"manufacturer_part_id,omitempty"`
	ClearManufacturerPart bool    `json:"clear_manufacturer_part,omitempty"`
	Packaging             *string `json:"packaging,omitempty"`
	ClearPackaging        bool    `json:"clear_packaging,omitempty"`
	PackQuantity          *string `json:"pack_quantity,omitempty"`
	Note                  *string `json:"note,omitempty"`
	ClearNote             bool    `json:"clear_note,omitempty"`
}

type UpdateManufacturerPartInput struct {
	ID               int     `json:"id"`
	PartID           *int    `json:"part_id,omitempty"`
	ManufacturerID   *int    `json:"manufacturer_id,omitempty"`
	MPN              *string `json:"mpn,omitempty"`
	ClearMPN         bool    `json:"clear_mpn,omitempty"`
	Description      *string `json:"description,omitempty"`
	ClearDescription bool    `json:"clear_description,omitempty"`
	Link             *string `json:"link,omitempty"`
	ClearLink        bool    `json:"clear_link,omitempty"`
}

func registerCompanyAdminLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, GetCompanyToolName, "Get company", "Retrieves one company by stable ID, including approved notes and role state.", getCompanyAdmin(deps))
	addReadOnlyTool(server, deps, SearchSupplierPartsToolName, "Search supplier parts", "Searches supplier-part links with bounded schema-backed filters and deterministic ID ordering.", searchSupplierPartsAdmin(deps))
	addReadOnlyTool(server, deps, GetSupplierPartToolName, "Get supplier part", "Retrieves one supplier-part link by stable ID.", getSupplierPartAdmin(deps))
	addReadOnlyTool(server, deps, SearchManufacturerPartsToolName, "Search manufacturer parts", "Searches manufacturer-part links with bounded schema-backed filters and deterministic ID ordering.", searchManufacturerPartsAdmin(deps))
	addReadOnlyTool(server, deps, GetManufacturerPartToolName, "Get manufacturer part", "Retrieves one manufacturer-part link by stable ID.", getManufacturerPartAdmin(deps))
}

func registerCompanyAdminWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, UpdateCompanyToolName, "Update company", "Partially updates approved company fields after role and dependency preflight.", updateCompanyAdmin(deps))
	addWriteTool(server, deps, UpdateSupplierPartToolName, "Update supplier part", "Partially updates a supplier-part link after identity and duplicate preflight.", updateSupplierPartAdmin(deps))
	addWriteTool(server, deps, UpdateManufacturerPartToolName, "Update manufacturer part", "Partially updates a manufacturer-part link after identity and duplicate preflight.", updateManufacturerPartAdmin(deps))
}

func getCompanyAdmin(deps Dependencies) mcp.ToolHandlerFor[IDInput, CompanyAdminRecordOutput[CompanyView]] {
	return LookupHandler[CompanyAdminClient, IDInput, CompanyAdminRecordOutput[CompanyView]](deps, GetCompanyToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input IDInput) (*mcp.CallToolResult, CompanyAdminRecordOutput[CompanyView], error) {
		record, err := client.GetCompanyDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyAdminRecordOutput[CompanyView]{}, safeCompanyAdminError("company lookup", err)
		}
		if record.PK != input.ID {
			return nil, CompanyAdminRecordOutput[CompanyView]{}, errors.New("company identity verification failed")
		}
		view := companyView(record)
		return TextResult(StatusOK), CompanyAdminRecordOutput[CompanyView]{Status: StatusOK, Record: &view}, nil
	})
}

func getSupplierPartAdmin(deps Dependencies) mcp.ToolHandlerFor[IDInput, CompanyAdminRecordOutput[SupplierPartView]] {
	return LookupHandler[CompanyAdminClient, IDInput, CompanyAdminRecordOutput[SupplierPartView]](deps, GetSupplierPartToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input IDInput) (*mcp.CallToolResult, CompanyAdminRecordOutput[SupplierPartView], error) {
		record, err := client.GetSupplierPartDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyAdminRecordOutput[SupplierPartView]{}, safeCompanyAdminError("supplier-part lookup", err)
		}
		if record.PK != input.ID {
			return nil, CompanyAdminRecordOutput[SupplierPartView]{}, errors.New("supplier-part identity verification failed")
		}
		view := supplierPartView(record)
		return TextResult(StatusOK), CompanyAdminRecordOutput[SupplierPartView]{Status: StatusOK, Record: &view}, nil
	})
}

func getManufacturerPartAdmin(deps Dependencies) mcp.ToolHandlerFor[IDInput, CompanyAdminRecordOutput[ManufacturerPartView]] {
	return LookupHandler[CompanyAdminClient, IDInput, CompanyAdminRecordOutput[ManufacturerPartView]](deps, GetManufacturerPartToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input IDInput) (*mcp.CallToolResult, CompanyAdminRecordOutput[ManufacturerPartView], error) {
		record, err := client.GetManufacturerPartDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyAdminRecordOutput[ManufacturerPartView]{}, safeCompanyAdminError("manufacturer-part lookup", err)
		}
		if record.PK != input.ID {
			return nil, CompanyAdminRecordOutput[ManufacturerPartView]{}, errors.New("manufacturer-part identity verification failed")
		}
		view := manufacturerPartView(record)
		return TextResult(StatusOK), CompanyAdminRecordOutput[ManufacturerPartView]{Status: StatusOK, Record: &view}, nil
	})
}

func searchSupplierPartsAdmin(deps Dependencies) mcp.ToolHandlerFor[SupplierPartSearchInput, CompanyAdminSearchOutput[SupplierPartRecoveryView]] {
	return LookupHandler[CompanyAdminClient, SupplierPartSearchInput, CompanyAdminSearchOutput[SupplierPartRecoveryView]](deps, SearchSupplierPartsToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input SupplierPartSearchInput) (*mcp.CallToolResult, CompanyAdminSearchOutput[SupplierPartRecoveryView], error) {
		limit, err := adminPage(input.Limit, input.Offset)
		if err != nil {
			return nil, CompanyAdminSearchOutput[SupplierPartRecoveryView]{}, err
		}
		if input.Offset > companyAdminScanLimit-limit {
			return nil, CompanyAdminSearchOutput[SupplierPartRecoveryView]{}, errors.New("offset plus limit must not exceed the 1000-record safety bound")
		}
		page, err := client.SearchSupplierPartsPage(ctx, inventree.SupplierPartQuery{Search: input.Search, Part: input.PartID, Supplier: input.SupplierID, SKU: input.SKU, Ordering: "SKU", Limit: companyAdminScanLimit})
		if err != nil {
			return nil, CompanyAdminSearchOutput[SupplierPartRecoveryView]{}, safeCompanyAdminError("supplier-part search", err)
		}
		if page.HasMore {
			return nil, CompanyAdminSearchOutput[SupplierPartRecoveryView]{}, errors.New("supplier-part search exceeds the 1000-record safety bound; add filters")
		}
		slices.SortFunc(page.Results, func(a, b inventree.SupplierPartDetail) int { return cmp.Compare(a.PK, b.PK) })
		window := adminWindow(page.Results, input.Offset, limit)
		results := make([]SupplierPartRecoveryView, 0, len(window))
		for _, record := range window {
			results = append(results, supplierPartRecovery(record))
		}
		return TextResult(StatusOK), CompanyAdminSearchOutput[SupplierPartRecoveryView]{Status: StatusOK, Count: page.Count, Results: results}, nil
	})
}

func searchManufacturerPartsAdmin(deps Dependencies) mcp.ToolHandlerFor[ManufacturerPartSearchInput, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]] {
	return LookupHandler[CompanyAdminClient, ManufacturerPartSearchInput, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]](deps, SearchManufacturerPartsToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input ManufacturerPartSearchInput) (*mcp.CallToolResult, CompanyAdminSearchOutput[ManufacturerPartRecoveryView], error) {
		limit, err := adminPage(input.Limit, input.Offset)
		if err != nil {
			return nil, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]{}, err
		}
		if input.Offset > companyAdminScanLimit-limit {
			return nil, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]{}, errors.New("offset plus limit must not exceed the 1000-record safety bound")
		}
		page, err := client.SearchManufacturerPartsPage(ctx, inventree.ManufacturerPartQuery{Search: input.Search, Part: input.PartID, Manufacturer: input.ManufacturerID, MPN: input.MPN, Ordering: "MPN", Limit: companyAdminScanLimit})
		if err != nil {
			return nil, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]{}, safeCompanyAdminError("manufacturer-part search", err)
		}
		if page.HasMore {
			return nil, CompanyAdminSearchOutput[ManufacturerPartRecoveryView]{}, errors.New("manufacturer-part search exceeds the 1000-record safety bound; add filters")
		}
		slices.SortFunc(page.Results, func(a, b inventree.ManufacturerPartDetail) int { return cmp.Compare(a.PK, b.PK) })
		window := adminWindow(page.Results, input.Offset, limit)
		results := make([]ManufacturerPartRecoveryView, 0, len(window))
		for _, record := range window {
			results = append(results, manufacturerPartRecovery(record))
		}
		return TextResult(StatusOK), CompanyAdminSearchOutput[ManufacturerPartRecoveryView]{Status: StatusOK, Count: page.Count, Results: results}, nil
	})
}

func updateCompanyAdmin(deps Dependencies) mcp.ToolHandlerFor[UpdateCompanyInput, CompanyMutationOutput[CompanyView, CompanyRecoveryView]] {
	return LookupHandler[CompanyAdminClient, UpdateCompanyInput, CompanyMutationOutput[CompanyView, CompanyRecoveryView]](deps, UpdateCompanyToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input UpdateCompanyInput) (*mcp.CallToolResult, CompanyMutationOutput[CompanyView, CompanyRecoveryView], error) {
		before, err := client.GetCompanyDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{}, safeCompanyAdminError("company lookup", err)
		}
		beforeRecovery := companyRecovery(before)
		if before.PK != input.ID {
			return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &beforeRecovery}, errors.New("company identity verification failed")
		}
		fields, err := companyPatch(input)
		if err != nil {
			return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &beforeRecovery}, err
		}
		if len(fields) == 0 {
			return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &beforeRecovery}, errors.New("update_company requires at least one approved PATCH field")
		}
		if input.IsSupplier != nil && before.IsSupplier && !*input.IsSupplier {
			if !input.Confirm {
				return clarificationCompanyRole(beforeRecovery, "supplier")
			}
			used, scanErr := hasSupplierLinks(ctx, client, input.ID)
			if scanErr != nil {
				return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &beforeRecovery}, scanErr
			}
			if used {
				return clarificationCompanyRoleUsed(beforeRecovery, "supplier")
			}
		}
		if input.IsManufacturer != nil && before.IsManufacturer && !*input.IsManufacturer {
			if !input.Confirm {
				return clarificationCompanyRole(beforeRecovery, "manufacturer")
			}
			used, scanErr := hasManufacturerLinks(ctx, client, input.ID)
			if scanErr != nil {
				return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &beforeRecovery}, scanErr
			}
			if used {
				return clarificationCompanyRoleUsed(beforeRecovery, "manufacturer")
			}
		}
		updated, err := client.UpdateCompany(ctx, input.ID, fields)
		if err != nil {
			return recoverCompanyUpdate(ctx, client, input.ID, fields, beforeRecovery, err)
		}
		return verifyCompanyUpdate(ctx, client, updated, fields, beforeRecovery)
	})
}

func updateSupplierPartAdmin(deps Dependencies) mcp.ToolHandlerFor[UpdateSupplierPartInput, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]] {
	return LookupHandler[CompanyAdminClient, UpdateSupplierPartInput, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]](deps, UpdateSupplierPartToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input UpdateSupplierPartInput) (*mcp.CallToolResult, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView], error) {
		before, err := client.GetSupplierPartDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{}, safeCompanyAdminError("supplier-part lookup", err)
		}
		beforeRecovery := supplierPartRecovery(before)
		if before.PK != input.ID {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, errors.New("supplier-part identity verification failed")
		}
		fields, target, err := supplierPartPatch(input, before)
		if err != nil {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, err
		}
		if len(fields) == 0 {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, errors.New("update_supplier_part requires at least one approved PATCH field")
		}
		if err := validatePartAndCompany(ctx, client, target.Part, target.Supplier, true); err != nil {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, err
		}
		if target.ManufacturerPart != nil {
			linked, getErr := client.GetManufacturerPartDetail(ctx, *target.ManufacturerPart)
			if getErr != nil || linked.PK != *target.ManufacturerPart || linked.Part != target.Part {
				return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, errors.New("manufacturer_part_id must identify a readable manufacturer part for the selected base part")
			}
		}
		matches, err := supplierDuplicates(ctx, client, target.Supplier, target.SKU, input.ID)
		if err != nil {
			return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &beforeRecovery}, err
		}
		if len(matches) > 0 {
			candidates := make([]SupplierPartRecoveryView, 0, len(matches))
			for _, match := range matches {
				candidates = append(candidates, supplierPartRecovery(match))
			}
			return TextResult(StatusClarificationRequired), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusClarificationRequired, Before: &beforeRecovery, Candidates: candidates, RecoveryPlan: "Use search_supplier_parts and get_supplier_part to choose or reconcile the existing normalized supplier+SKU identity before retrying."}, nil
		}
		updated, err := client.UpdateSupplierPart(ctx, input.ID, fields)
		if err != nil {
			return recoverSupplierPartUpdate(ctx, client, input.ID, fields, beforeRecovery, err)
		}
		return verifySupplierPartUpdate(ctx, client, updated, fields, beforeRecovery)
	})
}

func updateManufacturerPartAdmin(deps Dependencies) mcp.ToolHandlerFor[UpdateManufacturerPartInput, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]] {
	return LookupHandler[CompanyAdminClient, UpdateManufacturerPartInput, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]](deps, UpdateManufacturerPartToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input UpdateManufacturerPartInput) (*mcp.CallToolResult, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView], error) {
		before, err := client.GetManufacturerPartDetail(ctx, input.ID)
		if err != nil {
			return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{}, safeCompanyAdminError("manufacturer-part lookup", err)
		}
		beforeRecovery := manufacturerPartRecovery(before)
		if before.PK != input.ID {
			return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, errors.New("manufacturer-part identity verification failed")
		}
		fields, target, err := manufacturerPartPatch(input, before)
		if err != nil {
			return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, err
		}
		if len(fields) == 0 {
			return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, errors.New("update_manufacturer_part requires at least one approved PATCH field")
		}
		if err := validatePartAndCompany(ctx, client, target.Part, target.Manufacturer, false); err != nil {
			return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, err
		}
		if normalized(derefString(target.MPN)) != "" {
			matches, err := manufacturerDuplicates(ctx, client, target.Manufacturer, derefString(target.MPN), input.ID)
			if err != nil {
				return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, err
			}
			if len(matches) > 0 {
				candidates := make([]ManufacturerPartRecoveryView, 0, len(matches))
				for _, match := range matches {
					candidates = append(candidates, manufacturerPartRecovery(match))
				}
				return TextResult(StatusClarificationRequired), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusClarificationRequired, Before: &beforeRecovery, Candidates: candidates, RecoveryPlan: "Use search_manufacturer_parts and get_manufacturer_part to choose or reconcile the existing normalized manufacturer+MPN identity before retrying."}, nil
			}
		}
		if target.Part != before.Part {
			links, linkErr := supplierLinksForManufacturerPart(ctx, client, before.PK)
			if linkErr != nil {
				return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &beforeRecovery}, linkErr
			}
			for _, link := range links {
				if link.Part != target.Part {
					blocking := []SupplierPartRecoveryView{supplierPartRecovery(link)}
					return TextResult(StatusClarificationRequired), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusClarificationRequired, Before: &beforeRecovery, BlockingSupplierParts: blocking, RecoveryPlan: fmt.Sprintf("Supplier part %d still links this manufacturer part to base part %d. Reconcile that sourcing link before changing part_id.", link.PK, link.Part)}, nil
				}
			}
		}
		updated, err := client.UpdateManufacturerPart(ctx, input.ID, fields)
		if err != nil {
			return recoverManufacturerPartUpdate(ctx, client, input.ID, fields, beforeRecovery, err)
		}
		return verifyManufacturerPartUpdate(ctx, client, updated, fields, beforeRecovery)
	})
}

func companyPatch(input UpdateCompanyInput) (inventree.PatchFields, error) {
	if input.Notes != nil && input.ClearNotes {
		return nil, errors.New("notes and clear_notes are mutually exclusive")
	}
	fields := inventree.PatchFields{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, errors.New("company name cannot be blank")
		}
		fields["name"] = inventree.Set(name)
	}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.Website != nil {
		fields["website"] = inventree.Set(*input.Website)
	}
	if input.Currency != nil {
		currency := strings.TrimSpace(*input.Currency)
		if currency == "" {
			return nil, errors.New("company currency cannot be blank")
		}
		fields["currency"] = inventree.Set(currency)
	}
	if input.Active != nil {
		fields["active"] = inventree.Set(*input.Active)
	}
	if input.IsSupplier != nil {
		fields["is_supplier"] = inventree.Set(*input.IsSupplier)
	}
	if input.IsManufacturer != nil {
		fields["is_manufacturer"] = inventree.Set(*input.IsManufacturer)
	}
	if input.Notes != nil {
		fields["notes"] = inventree.Set(*input.Notes)
	} else if input.ClearNotes {
		fields["notes"] = inventree.Null()
	}
	return fields, nil
}

func supplierPartPatch(input UpdateSupplierPartInput, before inventree.SupplierPartDetail) (inventree.PatchFields, inventree.SupplierPartDetail, error) {
	if conflict(input.Description != nil, input.ClearDescription) || conflict(input.Link != nil, input.ClearLink) || conflict(input.ManufacturerPartID != nil, input.ClearManufacturerPart) || conflict(input.Packaging != nil, input.ClearPackaging) || conflict(input.Note != nil, input.ClearNote) {
		return nil, before, errors.New("a nullable field value and its clear flag are mutually exclusive")
	}
	fields := inventree.PatchFields{}
	target := before
	if input.PartID != nil {
		if *input.PartID <= 0 {
			return nil, before, errors.New("part_id must be positive")
		}
		fields["part"] = inventree.Set(*input.PartID)
		target.Part = *input.PartID
	}
	if input.SupplierID != nil {
		if *input.SupplierID <= 0 {
			return nil, before, errors.New("supplier_id must be positive")
		}
		fields["supplier"] = inventree.Set(*input.SupplierID)
		target.Supplier = *input.SupplierID
	}
	if input.SKU != nil {
		sku := strings.TrimSpace(*input.SKU)
		if sku == "" {
			return nil, before, errors.New("supplier SKU cannot be blank")
		}
		fields["SKU"] = inventree.Set(sku)
		target.SKU = sku
	}
	setNullableString(fields, "description", input.Description, input.ClearDescription)
	if input.Description != nil {
		target.Description = input.Description
	} else if input.ClearDescription {
		target.Description = nil
	}
	setNullableString(fields, "link", input.Link, input.ClearLink)
	if input.Link != nil {
		target.Link = input.Link
	} else if input.ClearLink {
		target.Link = nil
	}
	if input.Active != nil {
		fields["active"] = inventree.Set(*input.Active)
		target.Active = *input.Active
	}
	if input.Primary != nil {
		fields["primary"] = inventree.Set(*input.Primary)
		target.Primary = *input.Primary
	}
	if input.ManufacturerPartID != nil {
		if *input.ManufacturerPartID <= 0 {
			return nil, before, errors.New("manufacturer_part_id must be positive")
		}
		fields["manufacturer_part"] = inventree.Set(*input.ManufacturerPartID)
		target.ManufacturerPart = input.ManufacturerPartID
	} else if input.ClearManufacturerPart {
		fields["manufacturer_part"] = inventree.Null()
		target.ManufacturerPart = nil
	}
	setNullableString(fields, "packaging", input.Packaging, input.ClearPackaging)
	if input.Packaging != nil {
		target.Packaging = input.Packaging
	} else if input.ClearPackaging {
		target.Packaging = nil
	}
	if input.PackQuantity != nil {
		fields["pack_quantity"] = inventree.Set(*input.PackQuantity)
		target.PackQuantity = *input.PackQuantity
	}
	setNullableString(fields, "note", input.Note, input.ClearNote)
	if input.Note != nil {
		target.Note = input.Note
	} else if input.ClearNote {
		target.Note = nil
	}
	return fields, target, nil
}

func manufacturerPartPatch(input UpdateManufacturerPartInput, before inventree.ManufacturerPartDetail) (inventree.PatchFields, inventree.ManufacturerPartDetail, error) {
	if conflict(input.MPN != nil, input.ClearMPN) || conflict(input.Description != nil, input.ClearDescription) || conflict(input.Link != nil, input.ClearLink) {
		return nil, before, errors.New("a nullable field value and its clear flag are mutually exclusive")
	}
	fields := inventree.PatchFields{}
	target := before
	if input.PartID != nil {
		if *input.PartID <= 0 {
			return nil, before, errors.New("part_id must be positive")
		}
		fields["part"] = inventree.Set(*input.PartID)
		target.Part = *input.PartID
	}
	if input.ManufacturerID != nil {
		if *input.ManufacturerID <= 0 {
			return nil, before, errors.New("manufacturer_id must be positive")
		}
		fields["manufacturer"] = inventree.Set(*input.ManufacturerID)
		target.Manufacturer = *input.ManufacturerID
	}
	if input.MPN != nil {
		mpn := strings.TrimSpace(*input.MPN)
		fields["MPN"] = inventree.Set(mpn)
		target.MPN = &mpn
	} else if input.ClearMPN {
		fields["MPN"] = inventree.Null()
		target.MPN = nil
	}
	setNullableString(fields, "description", input.Description, input.ClearDescription)
	if input.Description != nil {
		target.Description = input.Description
	} else if input.ClearDescription {
		target.Description = nil
	}
	setNullableString(fields, "link", input.Link, input.ClearLink)
	if input.Link != nil {
		target.Link = input.Link
	} else if input.ClearLink {
		target.Link = nil
	}
	return fields, target, nil
}

func validatePartAndCompany(ctx context.Context, client CompanyAdminClient, partID, companyID int, supplier bool) error {
	part, err := client.GetPart(ctx, partID)
	if err != nil || part.PK != partID {
		return errors.New("part_id must identify a readable part")
	}
	company, err := client.GetCompanyDetail(ctx, companyID)
	if err != nil || company.PK != companyID {
		return errors.New("company ID must identify a readable company")
	}
	if supplier && !company.IsSupplier {
		return errors.New("supplier_id must identify a company with the supplier role")
	}
	if !supplier && !company.IsManufacturer {
		return errors.New("manufacturer_id must identify a company with the manufacturer role")
	}
	return nil
}

func supplierDuplicates(ctx context.Context, client CompanyAdminClient, supplier int, sku string, exclude int) ([]inventree.SupplierPartDetail, error) {
	var matches []inventree.SupplierPartDetail
	page, err := client.SearchSupplierPartsPage(ctx, inventree.SupplierPartQuery{Company: supplier, Ordering: "SKU", Limit: companyAdminScanLimit})
	if err != nil {
		return nil, safeCompanyAdminError("supplier-part duplicate preflight", err)
	}
	if page.HasMore {
		return nil, errors.New("supplier-part duplicate preflight exceeds the 1000-record safety bound")
	}
	for _, item := range page.Results {
		if item.Supplier == supplier && item.PK != exclude && normalized(item.SKU) == normalized(sku) {
			matches = append(matches, item)
		}
	}
	return matches, nil
}

func manufacturerDuplicates(ctx context.Context, client CompanyAdminClient, manufacturer int, mpn string, exclude int) ([]inventree.ManufacturerPartDetail, error) {
	var matches []inventree.ManufacturerPartDetail
	page, err := client.SearchManufacturerPartsPage(ctx, inventree.ManufacturerPartQuery{Manufacturer: manufacturer, Ordering: "MPN", Limit: companyAdminScanLimit})
	if err != nil {
		return nil, safeCompanyAdminError("manufacturer-part duplicate preflight", err)
	}
	if page.HasMore {
		return nil, errors.New("manufacturer-part duplicate preflight exceeds the 1000-record safety bound")
	}
	for _, item := range page.Results {
		if item.PK != exclude && normalized(derefString(item.MPN)) == normalized(mpn) {
			matches = append(matches, item)
		}
	}
	return matches, nil
}

func hasSupplierLinks(ctx context.Context, client CompanyAdminClient, companyID int) (bool, error) {
	page, err := client.SearchSupplierPartsPage(ctx, inventree.SupplierPartQuery{Company: companyID, Ordering: "SKU", Limit: companyAdminScanLimit})
	if err != nil {
		return false, safeCompanyAdminError("supplier-role dependency preflight", err)
	}
	if page.HasMore {
		return false, errors.New("supplier-role dependency preflight exceeds the 1000-record safety bound")
	}
	for _, record := range page.Results {
		if record.Supplier == companyID {
			return true, nil
		}
	}
	return false, nil
}

func hasManufacturerLinks(ctx context.Context, client CompanyAdminClient, companyID int) (bool, error) {
	page, err := client.SearchManufacturerPartsPage(ctx, inventree.ManufacturerPartQuery{Ordering: "MPN", Limit: companyAdminScanLimit})
	if err != nil {
		return false, safeCompanyAdminError("manufacturer-role dependency preflight", err)
	}
	if page.HasMore {
		return false, errors.New("manufacturer-role dependency preflight exceeds the 1000-record safety bound")
	}
	for _, record := range page.Results {
		if record.Manufacturer == companyID {
			return true, nil
		}
	}
	return false, nil
}

func supplierLinksForManufacturerPart(ctx context.Context, client CompanyAdminClient, manufacturerPartID int) ([]inventree.SupplierPartDetail, error) {
	var links []inventree.SupplierPartDetail
	page, err := client.SearchSupplierPartsPage(ctx, inventree.SupplierPartQuery{ManufacturerPart: manufacturerPartID, Ordering: "SKU", Limit: companyAdminScanLimit})
	if err != nil {
		return nil, safeCompanyAdminError("manufacturer-part reverse-link preflight", err)
	}
	if page.HasMore {
		return nil, errors.New("manufacturer-part reverse-link preflight exceeds the 1000-record safety bound")
	}
	links = append(links, page.Results...)
	return links, nil
}

func companyRoleIntegrity(ctx context.Context, client CompanyAdminClient, current inventree.CompanyDetail, fields inventree.PatchFields) error {
	if patchSetsFalse(fields, "is_supplier") {
		used, err := hasSupplierLinks(ctx, client, current.PK)
		if err != nil || used {
			return errors.New("supplier role dependency postflight failed")
		}
	}
	if patchSetsFalse(fields, "is_manufacturer") {
		used, err := hasManufacturerLinks(ctx, client, current.PK)
		if err != nil || used {
			return errors.New("manufacturer role dependency postflight failed")
		}
	}
	return nil
}

func supplierPartIntegrity(ctx context.Context, client CompanyAdminClient, current inventree.SupplierPartDetail) error {
	if err := validatePartAndCompany(ctx, client, current.Part, current.Supplier, true); err != nil {
		return err
	}
	if current.ManufacturerPart == nil {
		return nil
	}
	linked, err := client.GetManufacturerPartDetail(ctx, *current.ManufacturerPart)
	if err != nil || linked.PK != *current.ManufacturerPart || linked.Part != current.Part {
		return errors.New("manufacturer-part relationship postflight failed")
	}
	return nil
}

func manufacturerPartIntegrity(ctx context.Context, client CompanyAdminClient, current inventree.ManufacturerPartDetail) ([]SupplierPartRecoveryView, error) {
	if err := validatePartAndCompany(ctx, client, current.Part, current.Manufacturer, false); err != nil {
		return nil, err
	}
	links, err := supplierLinksForManufacturerPart(ctx, client, current.PK)
	if err != nil {
		return nil, err
	}
	blocking := make([]SupplierPartRecoveryView, 0)
	for _, link := range links {
		if link.Part != current.Part {
			blocking = append(blocking, supplierPartRecovery(link))
		}
	}
	return blocking, nil
}

func patchSetsFalse(fields inventree.PatchFields, key string) bool {
	field, ok := fields[key]
	if !ok {
		return false
	}
	value, ok := field.Value().(bool)
	return ok && !value
}

func supplierRecoveryViews(records []inventree.SupplierPartDetail) []SupplierPartRecoveryView {
	views := make([]SupplierPartRecoveryView, 0, len(records))
	for _, record := range records {
		views = append(views, supplierPartRecovery(record))
	}
	return views
}

func manufacturerRecoveryViews(records []inventree.ManufacturerPartDetail) []ManufacturerPartRecoveryView {
	views := make([]ManufacturerPartRecoveryView, 0, len(records))
	for _, record := range records {
		views = append(views, manufacturerPartRecovery(record))
	}
	return views
}

func verifyCompanyUpdate(ctx context.Context, client CompanyAdminClient, updated inventree.CompanyDetail, fields inventree.PatchFields, before CompanyRecoveryView) (*mcp.CallToolResult, CompanyMutationOutput[CompanyView, CompanyRecoveryView], error) {
	if updated.PK != before.ID {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read company_id %d before retrying; PATCH returned a mismatched identity.", before.ID)}, nil
	}
	current, err := client.GetCompanyDetail(ctx, updated.PK)
	if err != nil || !companyFieldsMatch(current, fields) {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read company_id %d before retrying; the PATCH result could not be fully verified.", updated.PK)}, nil
	}
	if integrityErr := companyRoleIntegrity(ctx, client, current, fields); integrityErr != nil {
		recovery := companyRecovery(current)
		return TextResult(StatusPartialFailure), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: &recovery, RecoveryPlan: "The PATCH applied, but postflight did not prove that removed company roles remain free of sourcing links. Inspect the company and its sourcing links before another write."}, nil
	}
	view := companyView(current)
	return TextResult(StatusOK), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusOK, Record: &view, Before: &before}, nil
}

func recoverCompanyUpdate(ctx context.Context, client CompanyAdminClient, id int, fields inventree.PatchFields, before CompanyRecoveryView, mutationErr error) (*mcp.CallToolResult, CompanyMutationOutput[CompanyView, CompanyRecoveryView], error) {
	if !ambiguousAdminMutation(mutationErr) {
		return nil, CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Before: &before}, safeCompanyAdminError("company update", mutationErr)
	}
	current, err := client.GetCompanyDetail(ctx, id)
	if err == nil && companyFieldsMatch(current, fields) {
		recovery := companyRecovery(current)
		if integrityErr := companyRoleIntegrity(ctx, client, current, fields); integrityErr != nil {
			return TextResult(StatusPartialFailure), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusPartialFailure, Current: &recovery, Before: &before, Recovered: true, RecoveryPlan: "The PATCH was recovered, but postflight did not prove that removed company roles remain free of sourcing links. Inspect the company and its sourcing links before another write."}, nil
		}
		return TextResult(StatusOK), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusOK, Current: &recovery, Before: &before, Recovered: true}, nil
	}
	return TextResult(StatusPartialFailure), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read company_id %d and compare the requested fields before retrying.", id)}, nil
}

func verifySupplierPartUpdate(ctx context.Context, client CompanyAdminClient, updated inventree.SupplierPartDetail, fields inventree.PatchFields, before SupplierPartRecoveryView) (*mcp.CallToolResult, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView], error) {
	if updated.PK != before.ID {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read supplier_part_id %d before retrying; PATCH returned a mismatched identity.", before.ID)}, nil
	}
	current, err := client.GetSupplierPartDetail(ctx, updated.PK)
	if err != nil || !supplierPartFieldsMatch(current, fields) {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read supplier_part_id %d and compare requested fields before retrying.", updated.PK)}, nil
	}
	if integrityErr := supplierPartIntegrity(ctx, client, current); integrityErr != nil {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrSupplierRecovery(current), RecoveryPlan: "The PATCH applied, but postflight did not prove the supplier role or linked part/manufacturer-part relationships. Inspect the stable IDs before another write."}, nil
	}
	matches, duplicateErr := supplierDuplicates(ctx, client, current.Supplier, current.SKU, current.PK)
	if duplicateErr != nil || len(matches) > 0 {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrSupplierRecovery(current), Candidates: supplierRecoveryViews(matches), RecoveryPlan: "The PATCH applied, but normalized duplicate postflight did not prove a unique supplier+SKU identity. Inspect the current record and candidates before another write."}, nil
	}
	view := supplierPartView(current)
	return TextResult(StatusOK), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusOK, Record: &view, Before: &before}, nil
}

func recoverSupplierPartUpdate(ctx context.Context, client CompanyAdminClient, id int, fields inventree.PatchFields, before SupplierPartRecoveryView, mutationErr error) (*mcp.CallToolResult, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView], error) {
	if !ambiguousAdminMutation(mutationErr) {
		return nil, CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Before: &before}, safeCompanyAdminError("supplier-part update", mutationErr)
	}
	current, err := client.GetSupplierPartDetail(ctx, id)
	if err == nil && supplierPartFieldsMatch(current, fields) {
		if integrityErr := supplierPartIntegrity(ctx, client, current); integrityErr != nil {
			return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrSupplierRecovery(current), Recovered: true, RecoveryPlan: "The PATCH was recovered, but postflight did not prove the supplier role or linked part/manufacturer-part relationships. Inspect the stable IDs before another write."}, nil
		}
		matches, duplicateErr := supplierDuplicates(ctx, client, current.Supplier, current.SKU, current.PK)
		if duplicateErr != nil || len(matches) > 0 {
			return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrSupplierRecovery(current), Candidates: supplierRecoveryViews(matches), Recovered: true, RecoveryPlan: "The PATCH was recovered, but normalized duplicate postflight did not prove a unique supplier+SKU identity. Inspect the current record and candidates before another write."}, nil
		}
		recovery := supplierPartRecovery(current)
		return TextResult(StatusOK), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusOK, Current: &recovery, Before: &before, Recovered: true}, nil
	}
	return TextResult(StatusPartialFailure), CompanyMutationOutput[SupplierPartView, SupplierPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read supplier_part_id %d and compare requested fields before retrying.", id)}, nil
}

func verifyManufacturerPartUpdate(ctx context.Context, client CompanyAdminClient, updated inventree.ManufacturerPartDetail, fields inventree.PatchFields, before ManufacturerPartRecoveryView) (*mcp.CallToolResult, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView], error) {
	if updated.PK != before.ID {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read manufacturer_part_id %d before retrying; PATCH returned a mismatched identity.", before.ID)}, nil
	}
	current, err := client.GetManufacturerPartDetail(ctx, updated.PK)
	if err != nil || !manufacturerPartFieldsMatch(current, fields) {
		return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read manufacturer_part_id %d and compare requested fields before retrying.", updated.PK)}, nil
	}
	blocking, integrityErr := manufacturerPartIntegrity(ctx, client, current)
	if integrityErr != nil || len(blocking) > 0 {
		recovery := manufacturerPartRecovery(current)
		return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: &recovery, BlockingSupplierParts: blocking, RecoveryPlan: "The PATCH applied, but postflight did not prove the manufacturer role or supplier-link base-part relationships. Inspect the stable IDs and blocking supplier parts before another write."}, nil
	}
	if normalized(derefString(current.MPN)) != "" {
		matches, duplicateErr := manufacturerDuplicates(ctx, client, current.Manufacturer, derefString(current.MPN), current.PK)
		if duplicateErr != nil || len(matches) > 0 {
			recovery := manufacturerPartRecovery(current)
			return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: &recovery, Candidates: manufacturerRecoveryViews(matches), RecoveryPlan: "The PATCH applied, but normalized duplicate postflight did not prove a unique manufacturer+MPN identity. Inspect the current record and candidates before another write."}, nil
		}
	}
	view := manufacturerPartView(current)
	return TextResult(StatusOK), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusOK, Record: &view, Before: &before}, nil
}

func recoverManufacturerPartUpdate(ctx context.Context, client CompanyAdminClient, id int, fields inventree.PatchFields, before ManufacturerPartRecoveryView, mutationErr error) (*mcp.CallToolResult, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView], error) {
	if !ambiguousAdminMutation(mutationErr) {
		return nil, CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Before: &before}, safeCompanyAdminError("manufacturer-part update", mutationErr)
	}
	current, err := client.GetManufacturerPartDetail(ctx, id)
	if err == nil && manufacturerPartFieldsMatch(current, fields) {
		blocking, integrityErr := manufacturerPartIntegrity(ctx, client, current)
		if integrityErr != nil || len(blocking) > 0 {
			return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrManufacturerRecovery(current), BlockingSupplierParts: blocking, Recovered: true, RecoveryPlan: "The PATCH was recovered, but postflight did not prove the manufacturer role or supplier-link base-part relationships. Inspect the stable IDs and blocking supplier parts before another write."}, nil
		}
		if normalized(derefString(current.MPN)) != "" {
			matches, duplicateErr := manufacturerDuplicates(ctx, client, current.Manufacturer, derefString(current.MPN), current.PK)
			if duplicateErr != nil || len(matches) > 0 {
				return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, Current: ptrManufacturerRecovery(current), Candidates: manufacturerRecoveryViews(matches), Recovered: true, RecoveryPlan: "The PATCH was recovered, but normalized duplicate postflight did not prove a unique manufacturer+MPN identity. Inspect the current record and candidates before another write."}, nil
			}
		}
		recovery := manufacturerPartRecovery(current)
		return TextResult(StatusOK), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusOK, Current: &recovery, Before: &before, Recovered: true}, nil
	}
	return TextResult(StatusPartialFailure), CompanyMutationOutput[ManufacturerPartView, ManufacturerPartRecoveryView]{Status: StatusPartialFailure, Before: &before, RecoveryPlan: fmt.Sprintf("Read manufacturer_part_id %d and compare requested fields before retrying.", id)}, nil
}

func companyFieldsMatch(record inventree.CompanyDetail, fields inventree.PatchFields) bool {
	return patchMatches(fields, map[string]any{"name": record.Name, "description": record.Description, "website": record.Website, "currency": record.Currency, "active": record.Active, "is_supplier": record.IsSupplier, "is_manufacturer": record.IsManufacturer, "notes": record.Notes})
}
func supplierPartFieldsMatch(record inventree.SupplierPartDetail, fields inventree.PatchFields) bool {
	return patchMatches(fields, map[string]any{"part": record.Part, "supplier": record.Supplier, "SKU": record.SKU, "description": record.Description, "link": record.Link, "active": record.Active, "primary": record.Primary, "manufacturer_part": record.ManufacturerPart, "packaging": record.Packaging, "pack_quantity": record.PackQuantity, "note": record.Note})
}
func manufacturerPartFieldsMatch(record inventree.ManufacturerPartDetail, fields inventree.PatchFields) bool {
	return patchMatches(fields, map[string]any{"part": record.Part, "manufacturer": record.Manufacturer, "MPN": record.MPN, "description": record.Description, "link": record.Link})
}

func patchMatches(fields inventree.PatchFields, values map[string]any) bool {
	for key, field := range fields {
		got, ok := values[key]
		if !ok || !reflect.DeepEqual(comparablePatchValue(got), comparablePatchValue(field.Value())) {
			return false
		}
	}
	return true
}
func comparablePatchValue(value any) any {
	switch typed := value.(type) {
	case *string:
		if typed == nil {
			return nil
		}
		return *typed
	case *int:
		if typed == nil {
			return nil
		}
		return *typed
	default:
		return value
	}
}

func companyView(record inventree.CompanyDetail) CompanyView {
	return CompanyView{ID: record.PK, Name: record.Name, Description: record.Description, Website: redactedMetadataURL(stringPointer(record.Website)), Currency: record.Currency, Active: record.Active, IsSupplier: record.IsSupplier, IsManufacturer: record.IsManufacturer, IsCustomer: record.IsCustomer, Notes: record.Notes}
}
func companyRecovery(record inventree.CompanyDetail) CompanyRecoveryView {
	return CompanyRecoveryView{ID: record.PK, Name: record.Name, Currency: record.Currency, Active: record.Active, IsSupplier: record.IsSupplier, IsManufacturer: record.IsManufacturer, IsCustomer: record.IsCustomer}
}
func supplierPartView(record inventree.SupplierPartDetail) SupplierPartView {
	return SupplierPartView{ID: record.PK, PartID: record.Part, SupplierID: record.Supplier, SKU: record.SKU, Description: record.Description, Link: redactedMetadataURL(record.Link), Active: record.Active, Primary: record.Primary, ManufacturerPartID: record.ManufacturerPart, Packaging: record.Packaging, PackQuantity: record.PackQuantity, Note: record.Note}
}
func supplierPartRecovery(record inventree.SupplierPartDetail) SupplierPartRecoveryView {
	return SupplierPartRecoveryView{ID: record.PK, PartID: record.Part, SupplierID: record.Supplier, SKU: record.SKU, Active: record.Active, Primary: record.Primary, ManufacturerPartID: record.ManufacturerPart}
}
func manufacturerPartView(record inventree.ManufacturerPartDetail) ManufacturerPartView {
	return ManufacturerPartView{ID: record.PK, PartID: record.Part, ManufacturerID: record.Manufacturer, MPN: record.MPN, Description: record.Description, Link: redactedMetadataURL(record.Link)}
}
func manufacturerPartRecovery(record inventree.ManufacturerPartDetail) ManufacturerPartRecoveryView {
	return ManufacturerPartRecoveryView{ID: record.PK, PartID: record.Part, ManufacturerID: record.Manufacturer, MPN: record.MPN}
}

func adminPage(limit, offset int) (int, error) {
	if offset < 0 {
		return 0, errors.New("offset must be non-negative")
	}
	if limit == 0 {
		return 20, nil
	}
	if limit < 1 || limit > 100 {
		return 0, errors.New("limit must be between 1 and 100")
	}
	return limit, nil
}
func adminWindow[T any](records []T, offset, limit int) []T {
	if offset >= len(records) {
		return nil
	}
	end := min(offset+limit, len(records))
	return records[offset:end]
}
func normalized(value string) string  { return strings.ToLower(strings.TrimSpace(value)) }
func conflict(value, clear bool) bool { return value && clear }
func setNullableString(fields inventree.PatchFields, key string, value *string, clear bool) {
	if value != nil {
		fields[key] = inventree.Set(*value)
	} else if clear {
		fields[key] = inventree.Null()
	}
}
func ptrSupplierRecovery(record inventree.SupplierPartDetail) *SupplierPartRecoveryView {
	value := supplierPartRecovery(record)
	return &value
}
func ptrManufacturerRecovery(record inventree.ManufacturerPartDetail) *ManufacturerPartRecoveryView {
	value := manufacturerPartRecovery(record)
	return &value
}
func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func safeCompanyAdminError(action string, err error) error {
	var apiErr *inventree.APIError
	if errors.As(err, &apiErr) {
		allowed := []string{"name", "description", "website", "currency", "active", "is_supplier", "is_manufacturer", "notes", "part", "supplier", "SKU", "manufacturer", "MPN", "link", "primary", "manufacturer_part", "packaging", "pack_quantity", "note"}
		var details []string
		for _, key := range allowed {
			if messages := apiErr.FieldErrors[key]; len(messages) > 0 {
				details = append(details, key+": rejected by InvenTree")
			}
		}
		if len(details) > 0 {
			return fmt.Errorf("%s rejected by InvenTree (%d): %s", action, apiErr.StatusCode, strings.Join(details, ", "))
		}
		return fmt.Errorf("%s failed with InvenTree status %d", action, apiErr.StatusCode)
	}
	return fmt.Errorf("%s failed; inspect InvenTree availability and current stable-ID state before retrying", action)
}

func ambiguousAdminMutation(err error) bool {
	var apiErr *inventree.APIError
	return !errors.As(err, &apiErr) || apiErr.StatusCode >= http.StatusInternalServerError
}

func clarificationCompanyRole(before CompanyRecoveryView, role string) (*mcp.CallToolResult, CompanyMutationOutput[CompanyView, CompanyRecoveryView], error) {
	return TextResult(StatusClarificationRequired), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusClarificationRequired, Before: &before, RecoveryPlan: "Review the current company and retry with confirm:true to remove the " + role + " role."}, nil
}
func clarificationCompanyRoleUsed(before CompanyRecoveryView, role string) (*mcp.CallToolResult, CompanyMutationOutput[CompanyView, CompanyRecoveryView], error) {
	return TextResult(StatusClarificationRequired), CompanyMutationOutput[CompanyView, CompanyRecoveryView]{Status: StatusClarificationRequired, Before: &before, RecoveryPlan: "The " + role + " role cannot be removed while corresponding sourcing links exist. Reassign those links outside this operation or keep the role."}, nil
}
