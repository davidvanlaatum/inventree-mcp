package tools

import (
	"reflect"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
)

func projectWebLinks(resolver *weblinks.Resolver, toolName string, output any) {
	if resolver == nil || output == nil {
		return
	}
	projectWebLinkValue(resolver, toolName, reflect.ValueOf(output))
}

// projectWebLinkValue walks the current MCP output DTO shapes. Those shapes are
// acyclic and keep linkable records in structs, pointers, slices, or arrays;
// maps contain metadata scalars only and are intentionally not traversed.
func projectWebLinkValue(resolver *weblinks.Resolver, toolName string, value reflect.Value) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			projectWebLinkValue(resolver, toolName, value.Elem())
		}
	case reflect.Struct:
		if value.CanAddr() && value.Addr().CanInterface() {
			decorateWebLink(resolver, toolName, value.Addr().Interface())
		}
		for index := range value.NumField() {
			projectWebLinkValue(resolver, toolName, value.Field(index))
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			projectWebLinkValue(resolver, toolName, value.Index(index))
		}
	}
}

func decorateWebLink(resolver *weblinks.Resolver, toolName string, target any) {
	switch value := target.(type) {
	case *inventree.Part:
		value.WebURL = resolver.URL(weblinks.Part, value.PK)
	case *PartDetailView:
		if value.complete {
			value.WebURL = resolver.URL(weblinks.Part, value.PK)
		}
	case *inventree.Category:
		value.WebURL = resolver.URL(weblinks.PartCategory, value.PK)
	case *inventree.Company:
		value.WebURL = resolver.URL(companyRouteKind(toolName), value.PK)
	case *inventree.StockLocation:
		value.WebURL = resolver.URL(weblinks.StockLocation, value.PK)
	case *inventree.StockItem:
		value.WebURL = resolver.URL(weblinks.StockItem, value.PK)
	case *inventree.SupplierPart:
		value.WebURL = resolver.URL(weblinks.SupplierPart, value.PK)
	case *inventree.SupplierPartDetail:
		value.WebURL = resolver.URL(weblinks.SupplierPart, value.PK)
	case *inventree.ManufacturerPart:
		value.WebURL = resolver.URL(weblinks.ManufacturerPart, value.PK)
	case *inventree.ManufacturerPartDetail:
		value.WebURL = resolver.URL(weblinks.ManufacturerPart, value.PK)
	case *inventree.PurchaseOrder:
		value.WebURL = resolver.URL(weblinks.PurchaseOrder, value.PK)
	case *inventree.PurchaseOrderLineItem:
		value.ParentWebURL = resolver.URL(weblinks.PurchaseOrder, value.Order)
	case *inventree.PurchaseOrderExtraLine:
		value.ParentWebURL = resolver.URL(weblinks.PurchaseOrder, value.Order)
	case *inventree.Parameter:
		value.ParentWebURL = modelParentWebURL(resolver, value.ModelType, value.ModelID)
	case *inventree.CategoryParameterTemplate:
		value.ParentWebURL = resolver.URL(weblinks.PartCategory, value.Category)
	case *inventree.Attachment:
		value.ParentWebURL = modelParentWebURL(resolver, value.ModelType, value.ModelID)
	case *ClarificationCandidate:
		decorateCandidateWebLink(resolver, toolName, value)
	case *AttachmentMetadata:
		value.ParentWebURL = modelParentWebURL(resolver, value.ModelType, value.ModelID)
	case *CompanyView:
		value.WebURL = resolver.URL(companyRouteKind(toolName), value.ID)
	case *CompanyRecoveryView:
		value.WebURL = resolver.URL(companyRouteKind(toolName), value.ID)
	case *SupplierPartView:
		value.WebURL = resolver.URL(weblinks.SupplierPart, value.ID)
	case *SupplierPartRecoveryView:
		value.WebURL = resolver.URL(weblinks.SupplierPart, value.ID)
	case *ManufacturerPartView:
		value.WebURL = resolver.URL(weblinks.ManufacturerPart, value.ID)
	case *ManufacturerPartRecoveryView:
		value.WebURL = resolver.URL(weblinks.ManufacturerPart, value.ID)
	case *PartParameterSearchResult:
		value.ParentWebURL = resolver.URL(weblinks.Part, value.PartID)
	case *CategoryParameterDefaultRecord:
		value.ParentWebURL = resolver.URL(weblinks.PartCategory, value.CategoryID)
	case *CompanyImageState:
		value.WebURL = resolver.URL(weblinks.Company, value.CompanyID)
	case *StockStateSnapshot:
		value.WebURL = resolver.URL(weblinks.StockItem, value.StockItemID)
	case *StockTransferLocation:
		value.WebURL = resolver.URL(weblinks.StockLocation, value.ID)
	case *StockLocationPlanState:
		value.WebURL = resolver.URL(weblinks.StockLocation, value.ID)
	case *StockLocationPlanContext:
		value.WebURL = resolver.URL(weblinks.StockLocation, value.ID)
	case *StockMetadataState:
		value.WebURL = resolver.URL(weblinks.StockItem, value.ID)
	case *BulkParameterAction:
		value.ParentWebURL = resolver.URL(weblinks.Part, value.PartID)
	case *ParameterTemplateMergeAction:
		if value.PartID > 0 {
			value.ParentWebURL = resolver.URL(weblinks.Part, value.PartID)
		} else {
			value.ParentWebURL = modelParentWebURL(resolver, value.ModelType, value.ModelID)
		}
	case *ReceivePurchaseOrderOutput:
		if value.Order != nil {
			for index := range value.Plan {
				value.Plan[index].ParentWebURL = resolver.URL(weblinks.PurchaseOrder, value.Order.PK)
			}
		}
	}
}

func companyRouteKind(toolName string) weblinks.Kind {
	switch toolName {
	case SearchSuppliersToolName:
		return weblinks.Supplier
	case SearchManufacturersToolName:
		return weblinks.Manufacturer
	default:
		return weblinks.Company
	}
}

func modelParentWebURL(resolver *weblinks.Resolver, modelType string, modelID int) string {
	switch modelType {
	case "part", "part.part":
		return resolver.URL(weblinks.Part, modelID)
	case "partcategory", "part.partcategory":
		return resolver.URL(weblinks.PartCategory, modelID)
	case "stockitem", "stock.stockitem":
		return resolver.URL(weblinks.StockItem, modelID)
	case "stocklocation", "stock.stocklocation":
		return resolver.URL(weblinks.StockLocation, modelID)
	case "company", "company.company":
		return resolver.URL(weblinks.Company, modelID)
	case "supplierpart", "company.supplierpart":
		return resolver.URL(weblinks.SupplierPart, modelID)
	case "manufacturerpart", "company.manufacturerpart":
		return resolver.URL(weblinks.ManufacturerPart, modelID)
	case "purchaseorder", "order.purchaseorder":
		return resolver.URL(weblinks.PurchaseOrder, modelID)
	default:
		return ""
	}
}

func decorateCandidateWebLink(resolver *weblinks.Resolver, toolName string, candidate *ClarificationCandidate) {
	candidate.WebURL = ""
	candidate.ParentWebURL = ""
	if candidate.APIURL != "" {
		sanitized, err := weblinks.ValidateAPIPath(candidate.APIURL)
		if err != nil {
			candidate.APIURL = ""
		} else {
			candidate.APIURL = sanitized
		}
	}
	if kind, id, ok := weblinks.KindForAPIPath(candidate.APIURL); ok {
		if kind == weblinks.Company {
			kind = companyRouteKind(toolName)
		}
		candidate.WebURL = resolver.URL(kind, id)
	}
	if orderID, ok := positiveIntField(candidate.Fields, "order"); ok {
		candidate.ParentWebURL = resolver.URL(weblinks.PurchaseOrder, orderID)
		return
	}
	modelType, _ := candidate.Fields["model_type"].(string)
	if modelID, ok := positiveIntField(candidate.Fields, "model_id"); ok {
		candidate.ParentWebURL = modelParentWebURL(resolver, modelType, modelID)
		return
	}
	if partID, ok := positiveIntField(candidate.Fields, "part_id"); ok && candidate.WebURL == "" {
		candidate.ParentWebURL = resolver.URL(weblinks.Part, partID)
		return
	}
	if categoryID, ok := positiveIntField(candidate.Fields, "category_id"); ok && candidate.WebURL == "" {
		candidate.ParentWebURL = resolver.URL(weblinks.PartCategory, categoryID)
	}
}

func positiveIntField(fields map[string]any, key string) (int, bool) {
	if fields == nil {
		return 0, false
	}
	switch value := fields[key].(type) {
	case int:
		return value, value > 0
	case int64:
		return int(value), value > 0
	case float64:
		if value > 0 && value == float64(int(value)) {
			return int(value), true
		}
		return 0, false
	default:
		return 0, false
	}
}
