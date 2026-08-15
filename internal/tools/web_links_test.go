package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webLinkProjectionOutput struct {
	Part          inventree.Part                  `json:"part"`
	Line          inventree.PurchaseOrderLineItem `json:"line"`
	Attachment    AttachmentMetadata              `json:"attachment"`
	Parameter     PartParameterSearchResult       `json:"parameter"`
	Company       CompanyView                     `json:"company"`
	Clarification ClarificationResponse           `json:"clarification"`
}

func TestProjectWebLinksCoversDirectParentAndClarificationContracts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	resolver, err := weblinks.New("https://inventory.example.test/prefix", "INVENTREE_WEB_URL", true)
	r.NoError(err)

	out := webLinkProjectionOutput{
		Part:       inventree.Part{PK: 11},
		Line:       inventree.PurchaseOrderLineItem{PK: 12, Order: 21},
		Attachment: AttachmentMetadata{PK: 13, ModelType: "part", ModelID: 11},
		Parameter:  PartParameterSearchResult{ParameterID: 14, PartID: 11},
		Company:    CompanyView{ID: 15},
		Clarification: NewClarification("Which line?", "line", "ambiguous", "id", true, []ClarificationCandidate{
			{ID: "12", WebURL: "https://attacker.example/direct", ParentWebURL: "https://attacker.example/parent", APIURL: "/api/order/po-line/12/", Fields: map[string]any{"order": 21}},
			{ID: "unsafe", APIURL: "https://attacker.example/api/part/99/?token=secret"},
		}, nil),
	}

	projectWebLinks(resolver, SearchCompaniesToolName, &out)
	a.Equal("https://inventory.example.test/prefix/part/11/", out.Part.WebURL)
	a.Equal("https://inventory.example.test/prefix/purchasing/purchase-order/21/", out.Line.ParentWebURL)
	a.Empty(out.Line.WebURL)
	a.Equal(out.Part.WebURL, out.Attachment.ParentWebURL)
	a.Equal(out.Part.WebURL, out.Parameter.ParentWebURL)
	a.Equal("https://inventory.example.test/prefix/company/15/", out.Company.WebURL)
	r.Len(out.Clarification.Candidates, 2)
	candidate := out.Clarification.Candidates[0]
	a.Empty(candidate.WebURL)
	a.Equal("/api/order/po-line/12/", candidate.APIURL)
	a.Equal(out.Line.ParentWebURL, candidate.ParentWebURL)
	a.Empty(out.Clarification.Candidates[1].APIURL)
	a.Empty(out.Clarification.Candidates[1].WebURL)

	wire, err := json.Marshal(candidate)
	r.NoError(err)
	a.NotContains(string(wire), `"url"`)
	a.Contains(string(wire), `"api_url":"/api/order/po-line/12/"`)
	a.Contains(string(wire), `"parent_web_url":"https://inventory.example.test/prefix/purchasing/purchase-order/21/"`)
}

func TestProjectWebLinksCoversEveryDirectObjectKind(t *testing.T) {
	t.Parallel()
	resolver, err := weblinks.New("https://inventory.example.test", "INVENTREE_WEB_URL", true)
	require.NoError(t, err)
	tests := []struct {
		name     string
		toolName string
		target   any
		want     string
	}{
		{name: "part", target: &inventree.Part{PK: 1}, want: "https://inventory.example.test/part/1/"},
		{name: "category", target: &inventree.Category{PK: 2}, want: "https://inventory.example.test/part/category/2/"},
		{name: "company", target: &inventree.Company{PK: 3}, want: "https://inventory.example.test/company/3/"},
		{name: "supplier company view", toolName: SearchSuppliersToolName, target: &inventree.Company{PK: 4}, want: "https://inventory.example.test/purchasing/supplier/4/"},
		{name: "manufacturer company view", toolName: SearchManufacturersToolName, target: &inventree.Company{PK: 5}, want: "https://inventory.example.test/purchasing/manufacturer/5/"},
		{name: "supplier part", target: &inventree.SupplierPart{PK: 6}, want: "https://inventory.example.test/purchasing/supplier-part/6/"},
		{name: "manufacturer part", target: &inventree.ManufacturerPart{PK: 7}, want: "https://inventory.example.test/purchasing/manufacturer-part/7/"},
		{name: "stock location", target: &inventree.StockLocation{PK: 8}, want: "https://inventory.example.test/stock/location/8/"},
		{name: "stock item", target: &inventree.StockItem{PK: 9}, want: "https://inventory.example.test/stock/item/9/"},
		{name: "purchase order", target: &inventree.PurchaseOrder{PK: 10}, want: "https://inventory.example.test/purchasing/purchase-order/10/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectWebLinks(resolver, tc.toolName, tc.target)
			wire, marshalErr := json.Marshal(tc.target)
			require.NoError(t, marshalErr)
			var record map[string]any
			require.NoError(t, json.Unmarshal(wire, &record))
			assert.Equal(t, tc.want, record["web_url"])
			_, hasParent := record["parent_web_url"]
			assert.False(t, hasParent)
		})
	}
}

func TestProjectWebLinksCoversDetailRecoveryPlanAndSubordinateViews(t *testing.T) {
	t.Parallel()
	resolver, err := weblinks.New("https://inventory.example.test", "INVENTREE_WEB_URL", true)
	require.NoError(t, err)
	partModelType := "part.part"
	targets := []any{
		func() *PartDetailView { value := partDetailView(inventree.PartDetail{PK: 25}); return &value }(),
		&inventree.SupplierPartDetail{PK: 1},
		&inventree.ManufacturerPartDetail{PK: 2},
		&inventree.PurchaseOrderLineItem{PK: 3, Order: 30},
		&inventree.PurchaseOrderExtraLine{PK: 4, Order: 30},
		&inventree.Parameter{PK: 5, ModelType: partModelType, ModelID: 50},
		&inventree.CategoryParameterTemplate{PK: 6, Category: 60},
		&inventree.Attachment{PK: 7, ModelType: "stockitem", ModelID: 70},
		&AttachmentMetadata{PK: 8, ModelType: "company", ModelID: 80},
		&CompanyView{ID: 9},
		&CompanyRecoveryView{ID: 10},
		&SupplierPartView{ID: 11},
		&SupplierPartRecoveryView{ID: 12},
		&ManufacturerPartView{ID: 13},
		&ManufacturerPartRecoveryView{ID: 14},
		&PartParameterSearchResult{ParameterID: 15, PartID: 150},
		&CategoryParameterDefaultRecord{LinkID: 16, CategoryID: 160},
		&CompanyImageState{CompanyID: 17},
		&StockStateSnapshot{StockItemID: 18},
		&StockTransferLocation{ID: 19},
		&StockLocationPlanState{ID: 20},
		&StockLocationPlanContext{ID: 21},
		&StockMetadataState{ID: 22},
		&BulkParameterAction{PartID: 230},
		&ParameterTemplateMergeAction{PartID: 240},
	}
	for index, target := range targets {
		projectWebLinks(resolver, SearchCompaniesToolName, target)
		wire, marshalErr := json.Marshal(target)
		require.NoError(t, marshalErr, index)
		var record map[string]any
		require.NoError(t, json.Unmarshal(wire, &record), index)
		_, direct := record["web_url"]
		_, parent := record["parent_web_url"]
		assert.True(t, direct || parent, "target %d (%T) did not receive a link", index, target)
	}

	incomplete := &PartDetailView{PartDetail: inventree.PartDetail{PK: 99}}
	projectWebLinks(resolver, GetPartToolName, incomplete)
	a := assert.New(t)
	a.Empty(incomplete.WebURL, "partial-failure part records must remain URL-free")

	modelParents := []struct {
		modelType string
		kind      weblinks.Kind
	}{
		{modelType: "part", kind: weblinks.Part},
		{modelType: "part.partcategory", kind: weblinks.PartCategory},
		{modelType: "stock.stockitem", kind: weblinks.StockItem},
		{modelType: "stock.stocklocation", kind: weblinks.StockLocation},
		{modelType: "company.company", kind: weblinks.Company},
		{modelType: "supplierpart", kind: weblinks.SupplierPart},
		{modelType: "company.manufacturerpart", kind: weblinks.ManufacturerPart},
		{modelType: "order.purchaseorder", kind: weblinks.PurchaseOrder},
	}
	for _, tc := range modelParents {
		assert.Equal(t, resolver.URL(tc.kind, 25), modelParentWebURL(resolver, tc.modelType, 25))
	}
	assert.Empty(t, modelParentWebURL(resolver, "unsupported.model", 25))

	receiving := ReceivePurchaseOrderOutput{
		Order: &inventree.PurchaseOrder{PK: 26},
		Plan:  []ReceivePurchaseOrderPlanItem{{LineItemID: 27}},
	}
	projectWebLinks(resolver, ReceivePurchaseOrderToolName, &receiving)
	require.Len(t, receiving.Plan, 1)
	assert.Equal(t, resolver.URL(weblinks.PurchaseOrder, 26), receiving.Plan[0].ParentWebURL)
}

func TestLookupHandlerUsesOnlyProcessScopedResolverAuthority(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	resolver, err := weblinks.New("https://trusted.example.test/inventree", "INVENTREE_WEB_URL", true)
	r.NoError(err)

	handler := LookupHandler[partSearchClient, SearchInput, LookupOutput[inventree.Part]](Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return partSearchClientFunc(func(context.Context, inventree.SearchQuery) ([]inventree.Part, error) {
				return []inventree.Part{{PK: 7, Name: "part"}}, nil
			}), nil
		},
		WebLinks: resolver,
	}, SearchPartsToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client partSearchClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Part], error) {
		records, err := client.SearchParts(ctx, inventree.SearchQuery{Search: input.Search})
		return TextResult(StatusOK), LookupOutput[inventree.Part]{Status: StatusOK, Results: records}, err
	})

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "https://attacker.example/path"})
	r.NoError(err)
	r.Len(out.Results, 1)
	a.Equal("https://trusted.example.test/inventree/part/7/", out.Results[0].WebURL)
	a.NotContains(out.Results[0].WebURL, "attacker")
}

func TestWebLinksAppearAtMCPBoundaryWithoutDeprecatedURLKey(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := require.New(t)
	a := assert.New(t)
	resolver, err := weblinks.New("https://trusted.example.test/base", "INVENTREE_WEB_URL", true)
	r.NoError(err)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "web-link-test", Version: "v0.0.0"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "test_clarification", Description: "Returns direct and subordinate candidates."}, LookupHandler[any, map[string]any, ClarificationResponse](Dependencies{
			WebLinks: resolver,
			ClientFromContext: func(context.Context) (any, error) {
				return struct{}{}, nil
			},
		}, "test_clarification", func(context.Context, *mcp.CallToolRequest, any, map[string]any) (*mcp.CallToolResult, ClarificationResponse, error) {
			return TextResult(StatusClarificationRequired), NewClarification("Which record?", "id", "ambiguous", "id", true, []ClarificationCandidate{
				{ID: "7", Label: "part", APIURL: "/api/part/7/"},
				{ID: "12", Label: "line", APIURL: "/api/order/po-line/12/", Fields: map[string]any{"order": 21}},
			}, nil), nil
		}))
		Register(server, Dependencies{
			WebLinks: resolver,
			ClientFromContext: func(context.Context) (any, error) {
				return &fakeMilestoneLookupClient{parts: []inventree.Part{{PK: 7, Name: "part"}}}, nil
			},
		})
		serverDone <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "web-link-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SearchPartsToolName, Arguments: map[string]any{"search": "part"}})
	r.NoError(err)
	r.False(result.IsError)
	structured := result.StructuredContent.(map[string]any)
	records := structured["results"].([]any)
	r.Len(records, 1)
	record := records[0].(map[string]any)
	a.Equal("https://trusted.example.test/base/part/7/", record["web_url"])
	_, hasDeprecatedURL := record["url"]
	a.False(hasDeprecatedURL)

	clarificationResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "test_clarification", Arguments: map[string]any{}})
	r.NoError(err)
	r.False(clarificationResult.IsError)
	clarification := clarificationResult.StructuredContent.(map[string]any)
	candidates := clarification["candidates"].([]any)
	r.Len(candidates, 2)
	direct := candidates[0].(map[string]any)
	a.ElementsMatch([]string{"id", "label", "web_url", "api_url"}, mapKeys(direct))
	a.Equal("https://trusted.example.test/base/part/7/", direct["web_url"])
	a.Equal("/api/part/7/", direct["api_url"])
	subordinate := candidates[1].(map[string]any)
	a.ElementsMatch([]string{"id", "label", "api_url", "parent_web_url", "fields"}, mapKeys(subordinate))
	a.Equal("https://trusted.example.test/base/purchasing/purchase-order/21/", subordinate["parent_web_url"])
	a.Equal("/api/order/po-line/12/", subordinate["api_url"])
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestPositiveIntFieldAcceptsOnlyExactPositiveIntegers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value any
		want  int
		ok    bool
	}{
		{value: 1, want: 1, ok: true},
		{value: int64(2), want: 2, ok: true},
		{value: float64(3), want: 3, ok: true},
		{value: float64(3.5)},
		{value: 0},
		{value: "4"},
	}
	for _, tc := range tests {
		got, ok := positiveIntField(map[string]any{"id": tc.value}, "id")
		assert.Equal(t, tc.ok, ok)
		assert.Equal(t, tc.want, got)
	}
	_, ok := positiveIntField(nil, "id")
	assert.False(t, ok)
}

type partSearchClientFunc func(context.Context, inventree.SearchQuery) ([]inventree.Part, error)

func (fn partSearchClientFunc) SearchParts(ctx context.Context, query inventree.SearchQuery) ([]inventree.Part, error) {
	return fn(ctx, query)
}
