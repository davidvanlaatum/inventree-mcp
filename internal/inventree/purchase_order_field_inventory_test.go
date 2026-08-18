package inventree_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurchaseOrderFieldInventoriesClassifyPinnedSerializersExactly(t *testing.T) {
	t.Parallel()
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	require.NoError(t, err)

	tests := []struct {
		name      string
		schema    string
		inventory map[string]inventree.PurchaseOrderFieldClass
		model     any
	}{
		{name: "order", schema: "PurchaseOrder", inventory: inventree.PurchaseOrderFieldInventory, model: inventree.PurchaseOrderDetail{}},
		{name: "line", schema: "PurchaseOrderLineItem", inventory: inventree.PurchaseOrderLineFieldInventory, model: inventree.PurchaseOrderLineItemDetail{}},
		{name: "extra_line", schema: "PurchaseOrderExtraLine", inventory: inventree.PurchaseOrderExtraLineFieldInventory, model: inventree.PurchaseOrderExtraLine{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			serializer, ok := openapi.Components.Schemas[tc.schema]
			r.True(ok)
			a.Len(tc.inventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
			a.Equal(serializer.Properties, selectPurchaseOrderSchemaProperties(serializer.Properties, tc.inventory), "inventory must classify every and only pinned serializer field")

			allowed := map[inventree.PurchaseOrderFieldClass]bool{
				inventree.PurchaseOrderFieldExposed: true, inventree.PurchaseOrderFieldSeparateLookup: true,
				inventree.PurchaseOrderFieldDeferred: true, inventree.PurchaseOrderFieldWriteOnly: true,
				inventree.PurchaseOrderFieldExcluded: true,
			}
			exposed := map[string]bool{}
			for field, class := range tc.inventory {
				a.True(allowed[class], "field %s has unknown classification %q", field, class)
				if class == inventree.PurchaseOrderFieldExposed {
					exposed[field] = true
				}
			}
			a.Equal(exposed, purchaseOrderModelFields(tc.model), "exact detail model must expose every and only fields classified as exposed")
		})
	}
}

func TestPurchaseOrderDetailsPreserveNullableFieldsAndOmitUnapprovedData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var order inventree.PurchaseOrderDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":10,"reference":"PO-0001","supplier":30,"supplier_reference":"","description":"","status":10,"created_by":{"pk":7,"username":"admin"},"creation_date":null,"issue_date":null,"start_date":null,"target_date":null,"complete_date":null,"line_items":null,"completed_lines":0,"link":"","status_text":"Pending","status_custom_key":null,"notes":null,"overdue":false,"supplier_name":"Acme","total_price":null,"order_currency":null,"destination":null,"updated_at":null,"barcode_hash":"secret","project_code":null,"responsible":3,"contact":4,"address":null,"address_detail":null,"contact_detail":{"pk":4},"project_code_detail":null,"project_code_label":null,"responsible_detail":{"pk":3},"parameters":[],"tags":["deferred"],"supplier_detail":{"pk":30}}`), &order))
	a.Equal(7, order.CreatedBy.PK)
	a.Nil(order.CreationDate)
	a.Nil(order.LineItems)
	r.NotNil(order.CompletedLines)
	a.Equal(0, *order.CompletedLines)
	a.Empty(order.Link)
	a.Nil(order.Notes)
	a.Equal("Acme", order.SupplierName)
	r.NotNil(order.Responsible)
	a.Equal(3, *order.Responsible)
	r.NotNil(order.Contact)
	a.Equal(4, *order.Contact)
	a.Nil(order.Address)
	assertPurchaseOrderJSONOmits(t, order, "barcode_hash", "project_code", "address_detail", "contact_detail", "project_code_detail", "project_code_label", "responsible_detail", "parameters", "tags", "supplier_detail")

	encoded, err := json.Marshal(order)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	a.Equal(float64(7), keys["created_by"], "created_by must project to a flat creator user ID")

	var line inventree.PurchaseOrderLineItemDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":20,"order":10,"part":40,"internal_part":41,"internal_part_name":"Resistor","destination":null,"line":"","reference":"","notes":"","quantity":5,"received":0,"target_date":null,"purchase_price":null,"purchase_price_currency":"AUD","link":"","discount":0,"build_order":null,"overdue":null,"auto_pricing":false,"sku":null,"mpn":null,"ipn":null,"total_price":null,"barcode_hash":"secret","order_detail":{"pk":10},"project_code":null,"project_code_label":null,"project_code_detail":null,"build_order_detail":null,"destination_detail":null,"part_detail":{"pk":40},"supplier_part_detail":{"pk":41},"merge_items":true}`), &line))
	a.Equal("Resistor", line.InternalPartName)
	a.Nil(line.SKU)
	a.Nil(line.TotalPrice)
	assertPurchaseOrderJSONOmits(t, line, "order_detail", "project_code", "project_code_label", "project_code_detail", "build_order_detail", "destination_detail", "part_detail", "supplier_part_detail", "merge_items")

	var extraLine inventree.PurchaseOrderExtraLine
	r.NoError(json.Unmarshal([]byte(`{"pk":30,"order":10,"line":"","description":"Freight","discount":0,"link":"","notes":"","price":null,"price_currency":"AUD","quantity":1,"reference":"","target_date":null,"total_price":null,"order_detail":{"pk":10},"project_code":null,"project_code_label":null,"project_code_detail":null}`), &extraLine))
	a.Equal("Freight", extraLine.Description)
	a.Nil(extraLine.Price)
	a.Nil(extraLine.TotalPrice)
	assertPurchaseOrderJSONOmits(t, extraLine, "order_detail", "project_code", "project_code_label", "project_code_detail")
}

func purchaseOrderModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" {
			fields[name] = true
		}
	}
	return fields
}

func selectPurchaseOrderSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.PurchaseOrderFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertPurchaseOrderJSONOmits(t *testing.T, value any, fields ...string) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)
	encoded, err := json.Marshal(value)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	for _, field := range fields {
		a.NotContains(keys, field)
	}
}
