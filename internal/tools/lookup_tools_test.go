package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupToolAuthorizationsUseReadOnlyScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, name := range lookupToolNames {
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		a.Equal("read_only", auth.MutationClass)
		wantScopes := []string{ScopeInventreeRead}
		if name == PollStocktakeGenerationToolName {
			wantScopes = []string{ScopeInventreeRead, ScopeInventreeOperational}
		}
		a.Equal(wantScopes, auth.Scopes)
		a.Equal(ReadOnlyAnnotations, auth.Annotations)
	}
}

func TestSearchPartsReturnsClarificationForAmbiguousResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{
			{PK: 10, Name: "10k resistor"},
			{PK: 11, Name: "10k resistor precision"},
		},
	}
	handler := searchParts(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "10k"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part_id", output.Clarification.Retry)
	a.Equal("part", output.Clarification.Field)
	a.Equal("10k", output.Clarification.RetryValues["search"])
	r.Len(output.Clarification.Candidates, 2)
	a.Equal("10", output.Clarification.Candidates[0].ID)
	a.Equal("10k resistor", output.Clarification.Candidates[0].Label)
	a.Equal("/api/part/10/", output.Clarification.Candidates[0].APIURL)
	a.Equal(inventree.SearchQuery{Search: "10k", Limit: 20}, fake.lastSearchPartsQuery)
}

func TestGetPartReturnsStructuredNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		getPartErr: &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound},
	}
	handler := getPart(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 404})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusNotFound, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNotFound, output.Status)
	a.Nil(output.Record)
	wire, marshalErr := json.Marshal(output)
	r.NoError(marshalErr)
	var encoded map[string]any
	r.NoError(json.Unmarshal(wire, &encoded))
	_, hasRecord := encoded["record"]
	a.False(hasRecord)
}

func TestGetPartNotFoundOmitsRecordThroughTypedMCPBoundary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "part-not-found-test-server", Version: "v0.0.0"}, nil)
		Register(server, Dependencies{ClientFromContext: func(context.Context) (any, error) {
			return &fakeMilestoneLookupClient{getPartErr: &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}}, nil
		}})
		serverDone <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "part-not-found-test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: GetPartToolName, Arguments: map[string]any{"id": 404}})
	r.NoError(err)
	r.NotNil(result)
	a.False(result.IsError)
	structured := result.StructuredContent.(map[string]any)
	a.Equal(StatusNotFound, structured["status"])
	_, hasRecord := structured["record"]
	a.False(hasRecord)
}

func TestGetPartReturnsCompleteApprovedDetailAndSanitizesExternalLink(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	complete := "https://example.test/parts/10?view=full#notes"
	pricing := inventree.DecimalString("1.250000")
	stock := 4.5
	fake := &fakeMilestoneLookupClient{partDetail: inventree.PartDetail{
		PK: 10, Name: "resistor", IPN: "R-10K", Link: &complete, Notes: dvgoutils.Ptr("markdown"),
		PricingMin: &pricing, InStock: &stock, CreationUser: dvgoutils.Ptr(7), Consumable: true,
		RevisionOf: dvgoutils.Ptr(8), RevisionCount: dvgoutils.Ptr(2), VariantOf: dvgoutils.Ptr(9),
	}}

	resolver, err := weblinks.New("https://inventory.example.test", "INVENTREE_WEB_URL", true)
	r.NoError(err)
	deps := depsForFake(fake)
	deps.WebLinks = resolver
	_, output, err := getPart(deps)(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("R-10K", output.Record.IPN)
	a.Equal(complete, output.Record.Link)
	a.Equal(dvgoutils.Ptr("markdown"), output.Record.Notes)
	a.Equal(&pricing, output.Record.PricingMin)
	a.Equal(&stock, output.Record.InStock)
	a.Equal(dvgoutils.Ptr(7), output.Record.CreationUser)
	a.True(output.Record.Consumable)
	a.Equal(dvgoutils.Ptr(8), output.Record.RevisionOf)
	a.Equal(dvgoutils.Ptr(2), output.Record.RevisionCount)
	a.Equal(dvgoutils.Ptr(9), output.Record.VariantOf)
	a.Equal("https://inventory.example.test/part/10/", output.Record.WebURL)

	credentialed := "https://user:pass@example.test/secret"
	fake.partDetail.Link = &credentialed
	_, output, err = getPart(deps)(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})
	r.NoError(err)
	a.Empty(output.Record.Link)
	encoded, err := json.Marshal(output.Record)
	r.NoError(err)
	a.NotContains(string(encoded), "user:pass")
	a.NotContains(string(encoded), "secret")
}

func TestGetPartRejectsMismatchedExactIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{partDetail: inventree.PartDetail{PK: 11}}

	_, _, err := getPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})

	require.ErrorContains(t, err, "mismatched part identity")
}

func TestGetPartCategoryRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{partCategory: inventree.Category{PK: 99, Name: "wrong"}}

	_, _, err := getPartCategory(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})
	require.ErrorContains(t, err, "mismatched part-category identity")
}

func TestGetPartCategorySanitizesUnexpectedUpstreamError(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{partCategoryErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, Detail: "sensitive upstream detail"}}

	_, _, err := getPartCategory(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sensitive")
	assert.Contains(t, err.Error(), "part-category lookup failed")
}

func TestSearchCompaniesReturnsNotFoundForNoResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	handler := searchCompanies(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "missing"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusNotFound, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNotFound, output.Status)
	a.Empty(output.Results)
}

func TestAttachmentMetadataToolsGateScopeAndRedactURLs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fileURL := "/media/file.pdf?signature=secret#fragment"
	thumbURL := "https://inventory.example.test/media/thumb.png?signature=secret"
	linkURL := "https://example.test/datasheet.pdf?token=secret#fragment"
	fake := &fakeMilestoneLookupClient{
		attachments: []inventree.Attachment{{
			PK:         90,
			ModelType:  "part",
			ModelID:    10,
			Attachment: &fileURL,
			Thumbnail:  &thumbURL,
			Link:       &linkURL,
			Filename:   "datasheet.pdf",
		}},
		attachment: inventree.Attachment{
			PK:         90,
			ModelType:  "part",
			ModelID:    10,
			Attachment: &fileURL,
			Thumbnail:  &thumbURL,
			Link:       &linkURL,
			Filename:   "datasheet.pdf",
		},
	}

	listHandler := listAttachments(depsForFake(fake))
	result, listOutput, err := listHandler(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "part", ModelID: 10})
	r.NoError(err)
	r.NotNil(result)
	r.Len(listOutput.Results, 1)
	a.Equal("/media/file.pdf", listOutput.Results[0].AttachmentURL)
	a.Equal("https://inventory.example.test/media/thumb.png", listOutput.Results[0].ThumbnailURL)
	a.Equal(linkURL, listOutput.Results[0].LinkURL)

	getHandler := getAttachmentMetadata(depsForFake(fake))
	_, recordOutput, err := getHandler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 90})
	r.NoError(err)
	a.Equal("/media/file.pdf", recordOutput.Record.AttachmentURL)
	a.Equal(linkURL, recordOutput.Record.LinkURL)

	_, _, err = listHandler(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "salesorder", ModelID: 10})
	r.ErrorContains(err, `model type "salesorder" is out of scope`)

	fake.attachment.ModelType = "salesorder"
	_, _, err = getHandler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 91})
	r.ErrorContains(err, `model type "salesorder" is out of scope`)
}

func TestSearchStockItemsUsesStableFilters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 2, PurchaseOrder: dvgoutils.Ptr(120), PurchaseOrderReference: dvgoutils.Ptr("PO-1")}},
	}
	handler := searchStockItems(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, StockItemsInput{PartID: 10, LocationID: 40, PurchaseOrderID: 120, Limit: 250})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, output.Count)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, PurchaseOrderID: 120, Limit: 100}, fake.lastSearchStockItemsQuery)
	r.NotNil(output.Results[0].PurchaseOrder)
	a.Equal(120, *output.Results[0].PurchaseOrder)
}

func TestSearchStockSerialsUsesPartScopedFilters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	serial := "42"
	fake := &fakeMilestoneLookupClient{stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 1, Serial: &serial}}}
	gte, lte := 1, 100
	serialized := true

	result, output, err := searchStockSerials(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, StockSerialsInput{PartID: 10, Serial: "42", SerialGTE: &gte, SerialLTE: &lte, Serialized: &serialized})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, output.Count)
	a.Equal(inventree.StockItemQuery{PartID: 10, Serial: "42", SerialGTE: &gte, SerialLTE: &lte, Serialized: &serialized, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	r.NotNil(output.Results[0].Serial)
	a.Equal("42", *output.Results[0].Serial)
}

func TestSearchStockSerialsRequiresPositivePartID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := searchStockSerials(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, StockSerialsInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)
}

func TestGetPartNextSerialReturnsLatestAndNext(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	latest := "9"
	fake := &fakeMilestoneLookupClient{
		part:              inventree.Part{PK: 10, Trackable: true},
		partSerialNumbers: inventree.PartSerialNumbers{Latest: &latest, Next: "10"},
	}

	_, output, err := getPartNextSerial(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PartNextSerialInput{PartID: 10})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(10, output.PartID)
	r.NotNil(output.Latest)
	a.Equal("9", *output.Latest)
	a.Equal("10", output.Next)
	a.Equal(10, fake.lastGetPartSerialNumbersID)
}

func TestGetPartNextSerialRejectsNonTrackablePart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 10, Trackable: false}}

	_, output, err := getPartNextSerial(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PartNextSerialInput{PartID: 10})
	r.NoError(err)
	a.Equal(StatusNotTrackable, output.Status)
	a.Zero(fake.lastGetPartSerialNumbersID)
}

func TestGetPartNextSerialReportsNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{getPartErr: &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}}

	_, output, err := getPartNextSerial(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PartNextSerialInput{PartID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestPreviewPurchaseOrderUsesSupplierPartIDWithoutWrites(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		supplierPart: inventree.SupplierPart{PK: 40, Part: 10, Supplier: 30, SKU: "ACME-10K"},
	}
	price := 1.25

	_, output, err := previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		SupplierID: 30,
		Lines: []PurchasePreviewLineInput{{
			SupplierPartID: 40,
			Quantity:       4,
			UnitPrice:      &price,
			Currency:       "AUD",
			Notes:          "prototype stock",
		}},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(30, output.SupplierID)
	r.Len(output.Lines, 1)
	a.Equal(40, output.Lines[0].SupplierPartID)
	a.Equal(10, output.Lines[0].PartID)
	a.Equal(5.0, *output.Lines[0].LineTotal)
	a.Equal(40, fake.lastGetSupplierPartID)
	a.False(fake.createdSupplierPart)
	a.False(fake.createdStockItem)
}

func TestPreviewPurchaseOrderSearchesAndClarifiesSupplierParts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		supplierParts: []inventree.SupplierPart{
			{PK: 40, Part: 10, Supplier: 30, SKU: "ACME-10K-A"},
			{PK: 41, Part: 10, Supplier: 30, SKU: "ACME-10K-B"},
		},
	}

	_, output, err := previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		SupplierID: 30,
		Lines:      []PurchasePreviewLineInput{{PartID: 10, Quantity: 4}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("supplier_part", output.Clarification.Field)
	a.Equal("supplier_part_id", output.Clarification.Retry)
	a.Len(output.Clarification.Candidates, 2)
	a.Equal(inventree.SupplierPartQuery{Part: 10, Supplier: 30}, fake.lastSearchSupplierPartsQuery)
	a.False(fake.createdSupplierPart)
}

func TestPreviewPurchaseOrderValidatesAmbiguousInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	price := 1.25

	_, output, err := previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("lines", output.Clarification.Field)

	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{Lines: []PurchasePreviewLineInput{{SupplierPartID: 40}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("quantity", output.Clarification.Field)

	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{Lines: []PurchasePreviewLineInput{{PartID: 10, Quantity: 1}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("supplier", output.Clarification.Field)

	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{SupplierID: 30, Lines: []PurchasePreviewLineInput{{PartID: 10, Quantity: 1, UnitPrice: &price}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("currency", output.Clarification.Field)
}

func TestPreviewPurchaseOrderRejectsMismatchedStableSupplierPartIDs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeMilestoneLookupClient{
		supplierParts: []inventree.SupplierPart{
			{PK: 40, Part: 10, Supplier: 30, SKU: "ACME-10K"},
			{PK: 41, Part: 11, Supplier: 31, SKU: "OTHER-11K"},
		},
	}
	_, output, err := previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		Lines: []PurchasePreviewLineInput{
			{SupplierPartID: 40, Quantity: 1},
			{SupplierPartID: 41, Quantity: 1},
		},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("supplier", output.Clarification.Field)
	a.Equal(30, output.SupplierID)

	fake = &fakeMilestoneLookupClient{
		supplierPart: inventree.SupplierPart{PK: 40, Part: 10, Supplier: 30, SKU: "ACME-10K"},
	}
	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		SupplierID: 30,
		Lines:      []PurchasePreviewLineInput{{SupplierPartID: 40, PartID: 11, Quantity: 1}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		SupplierID: 30,
		Lines:      []PurchasePreviewLineInput{{SupplierPartID: 40, PartID: -1, Quantity: 1}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = previewPurchaseOrder(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
		SupplierID: 30,
		Lines:      []PurchasePreviewLineInput{{SupplierPartID: 40, SupplierSKU: "wrong", Quantity: 1}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("supplier_sku", output.Clarification.Field)
}

func TestLookupHandlersPassStructuredQueriesToClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(context.Context, *require.Assertions, *fakeMilestoneLookupClient)
	}{
		{
			name: "categories",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.categories = []inventree.Category{{PK: 20, Name: "passives"}}
				_, _, err := searchPartCategories(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "pass", Limit: 101, Offset: 2})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "pass", Limit: 100, Offset: 2}, fake.lastSearchPartCategoriesQuery)
			},
		},
		{
			name: "parameter templates",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.parameterTemplates = []inventree.ParameterTemplate{{PK: 70, Name: "Resistance"}}
				_, _, err := searchParameterTemplates(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "resistance", Limit: 5, Offset: 1})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "resistance", Limit: 5, Offset: 1}, fake.lastSearchParameterTemplatesQuery)
			},
		},
		{
			name: "part parameters",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.parameters = []inventree.Parameter{{PK: 60, ModelID: 10}}
				_, _, err := getPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PartParametersInput{PartID: 10, Limit: 0, Offset: 3})
				r.NoError(err)
				r.Equal(inventree.PartParameterQuery{PartID: 10, Limit: 20, Offset: 3}, fake.lastSearchPartParametersQuery)
			},
		},
		{
			name: "suppliers",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.suppliers = []inventree.Company{{PK: 30, Name: "supplier", IsSupplier: true}}
				_, _, err := searchSuppliers(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "sup", Limit: 6})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "sup", Limit: 6}, fake.lastSearchSuppliersQuery)
			},
		},
		{
			name: "manufacturers",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.manufacturers = []inventree.Company{{PK: 31, Name: "maker", IsManufacturer: true}}
				_, _, err := searchManufacturers(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "make", Offset: 4})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "make", Limit: 20, Offset: 4}, fake.lastSearchManufacturersQuery)
			},
		},
		{
			name: "stock locations",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.stockLocations = []inventree.StockLocation{{PK: 40, Name: "bin"}}
				_, _, err := searchStockLocations(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "bin", Limit: 12})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "bin", Limit: 12}, fake.lastSearchStockLocationsQuery)
			},
		},
		{
			name: "attachments",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.attachments = []inventree.Attachment{{PK: 90, ModelType: "part", ModelID: 10, Filename: "datasheet.pdf"}}
				_, _, err := listAttachments(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "part", ModelID: 10, Search: "data", Limit: 3, Offset: 2})
				r.NoError(err)
				r.Equal(inventree.AttachmentQuery{ModelType: "part", ModelID: 10, Search: "data", Limit: 3, Offset: 2}, fake.lastListAttachmentsQuery)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			tt.run(ctx, r, &fakeMilestoneLookupClient{})
		})
	}
}

func TestDownloadAttachmentReturnsTextOrBase64WithDigest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		downloadedAttachment: inventree.DownloadedAttachment{
			Attachment:  inventree.Attachment{PK: 90, Filename: "datasheet.txt"},
			Content:     []byte("hello"),
			ContentType: "text/plain; charset=utf-8",
			SourceURL:   "https://inventory.example.test/media/datasheet.txt",
		},
	}
	handler := downloadAttachment(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: 90, MaxBytes: 100})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal("hello", output.Text)
	a.Empty(output.Base64)
	a.Equal("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", output.SHA256)
	a.Equal(int64(100), fake.lastAttachmentMaxBytes)
}

func TestDownloadPartImageReturnsBase64ForBinaryContent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		downloadedPartImage: inventree.DownloadedPartImage{
			Part:        inventree.Part{PK: 10, Name: "resistor"},
			Content:     []byte{0x89, 0x50, 0x4e, 0x47},
			Filename:    "resistor.png",
			ContentType: "image/png",
			SourceURL:   "https://inventory.example.test/media/resistor.png",
		},
	}
	handler := downloadPartImage(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{ID: 10})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Empty(output.Text)
	a.Equal("resistor.png", output.Filename)
	a.Equal("iVBORw==", output.Base64)
	a.Equal(defaultDownloadMaxBytes, fake.lastPartImageMaxBytes)
}

func TestDownloadPartImageReturnsNoImageStatus(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		downloadPartImageErr: inventree.ErrPartImageMissing,
	}
	handler := downloadPartImage(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{ID: 10})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusNoImage, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNoImage, output.Status)
	a.Equal(10, output.ID)
	a.Equal(string(inventree.AttachmentContentOriginal), output.Mode)
}

func depsForFake(fake *fakeMilestoneLookupClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return fake, nil
		},
	}
}

type fakeMilestoneLookupClient struct {
	parts                              []inventree.Part
	categories                         []inventree.Category
	companies                          []inventree.Company
	suppliers                          []inventree.Company
	manufacturers                      []inventree.Company
	manufacturerSearchResults          [][]inventree.Company
	manufacturerSearchCalls            int
	stockLocations                     []inventree.StockLocation
	stockItems                         []inventree.StockItem
	parameters                         []inventree.Parameter
	parameterTemplates                 []inventree.ParameterTemplate
	categoryParameterTemplates         []inventree.CategoryParameterTemplate
	inheritedCategoryIDs               []int
	categoryParameterScanLimitExceeded bool
	attachments                        []inventree.Attachment
	supplierParts                      []inventree.SupplierPart
	manufacturerParts                  []inventree.ManufacturerPart
	supplierPart                       inventree.SupplierPart
	attachment                         inventree.Attachment
	downloadedAttachment               inventree.DownloadedAttachment
	downloadedPartImage                inventree.DownloadedPartImage
	downloadPartImageErr               error
	getPartErr                         error
	getPartDetailErr                   error
	getPartDetailAfterFirstErr         error
	partDetailAfterFirst               *inventree.PartDetail
	getPartDetailCalls                 int
	getCompanyErr                      error
	companyDetail                      *inventree.CompanyDetail
	companyDetailErr                   error
	supplierPartDetail                 *inventree.SupplierPartDetail
	supplierPartDetailErr              error
	manufacturerPartDetail             *inventree.ManufacturerPartDetail
	manufacturerPartDetailErr          error
	part                               inventree.Part
	partDetail                         inventree.PartDetail
	partCategory                       inventree.Category
	partCategoryErr                    error
	createdPart                        bool
	createdPartParameter               bool
	createPartParameterCount           int
	createdCompany                     bool
	createdSupplierPart                bool
	createdManufacturerPart            bool
	createCompanyCalls                 int
	createSupplierPartCalls            int
	createManufacturerPartCalls        int
	createdStockItem                   bool
	uploadedAttachment                 bool
	createdLinkAttachment              bool
	deletedAttachment                  bool
	setPartPrimaryImage                bool
	createPartErr                      error
	createPartResult                   *inventree.Part
	updatePartErr                      error
	createCompanyErr                   error
	searchManufacturersErr             error
	createSupplierPartErr              error
	createManufacturerPartErr          error

	lastSearchPartsQuery                      inventree.SearchQuery
	lastSearchPartCategoriesQuery             inventree.SearchQuery
	lastSearchPartParametersQuery             inventree.PartParameterQuery
	lastSearchParameterTemplatesQuery         inventree.SearchQuery
	lastGetParameterTemplateID                int
	lastSearchCategoryParameterTemplatesQuery inventree.CategoryParameterTemplateQuery
	lastSearchCompaniesQuery                  inventree.SearchQuery
	lastSearchSuppliersQuery                  inventree.SearchQuery
	lastSearchManufacturersQuery              inventree.SearchQuery
	lastSearchStockLocationsQuery             inventree.SearchQuery
	lastSearchStockItemsQuery                 inventree.StockItemQuery
	lastGetStockLocationID                    int
	lastGetCompanyID                          int
	lastListAttachmentsQuery                  inventree.AttachmentQuery
	lastSearchSupplierPartsQuery              inventree.SupplierPartQuery
	lastSearchManufacturerPartsQuery          inventree.ManufacturerPartQuery
	lastGetSupplierPartID                     int
	lastCreatePart                            inventree.PartCreate
	lastCreatePartParameter                   inventree.ParameterCreate
	lastCreateCompany                         inventree.CompanyCreate
	lastCreateSupplierPart                    inventree.SupplierPartCreate
	lastCreateManufacturerPart                inventree.ManufacturerPartCreate
	lastCreateStockItem                       inventree.StockItemCreate
	lastAttachmentCreate                      inventree.AttachmentCreate
	lastUpdatePartFields                      inventree.PatchFields
	lastUpdatePartParameterFields             inventree.PatchFields
	lastUpdateAttachmentFields                inventree.PatchFields
	lastDeleteAttachmentID                    int
	lastSetPartPrimaryImagePartID             int
	lastSetPartPrimaryImageInput              inventree.PartPrimaryImageCreate
	updatePartParameterCount                  int
	lastAttachmentMaxBytes                    int64
	lastPartImageMaxBytes                     int64
	partSerialNumbers                         inventree.PartSerialNumbers
	getPartSerialNumbersErr                   error
	lastGetPartSerialNumbersID                int
}

func (f *fakeMilestoneLookupClient) SearchParts(_ context.Context, query inventree.SearchQuery) ([]inventree.Part, error) {
	f.lastSearchPartsQuery = query
	return f.parts, nil
}

func (f *fakeMilestoneLookupClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	if f.getPartErr != nil {
		return inventree.Part{}, f.getPartErr
	}
	if f.part.PK != 0 {
		return f.part, nil
	}
	return inventree.Part{PK: id, Name: "part"}, nil
}

func (f *fakeMilestoneLookupClient) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	f.getPartDetailCalls++
	if f.getPartDetailCalls > 1 {
		if f.getPartDetailAfterFirstErr != nil {
			return inventree.PartDetail{}, f.getPartDetailAfterFirstErr
		}
		if f.partDetailAfterFirst != nil {
			return *f.partDetailAfterFirst, nil
		}
	}
	if f.getPartDetailErr != nil {
		return inventree.PartDetail{}, f.getPartDetailErr
	}
	if f.getPartErr != nil {
		return inventree.PartDetail{}, f.getPartErr
	}
	if f.partDetail.PK != 0 {
		return f.partDetail, nil
	}
	if f.part.PK != 0 {
		return inventree.PartDetail{PK: f.part.PK, Name: f.part.Name, MinimumStock: 0, MaximumStock: 0}, nil
	}
	return inventree.PartDetail{PK: id, Name: "part"}, nil
}

func (f *fakeMilestoneLookupClient) GetCompany(_ context.Context, id int) (inventree.Company, error) {
	f.lastGetCompanyID = id
	if f.getCompanyErr != nil {
		return inventree.Company{}, f.getCompanyErr
	}
	for _, records := range [][]inventree.Company{f.companies, f.suppliers, f.manufacturers} {
		for _, company := range records {
			if company.PK == id {
				return company, nil
			}
		}
	}
	return inventree.Company{PK: id, Name: "company", IsSupplier: true, IsManufacturer: true}, nil
}

func (f *fakeMilestoneLookupClient) SearchPartCategories(_ context.Context, query inventree.SearchQuery) ([]inventree.Category, error) {
	f.lastSearchPartCategoriesQuery = query
	return f.categories, nil
}

func (f *fakeMilestoneLookupClient) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	if f.partCategoryErr != nil {
		return inventree.Category{}, f.partCategoryErr
	}
	if f.partCategory.PK != 0 {
		return f.partCategory, nil
	}
	for _, category := range f.categories {
		if category.PK == id {
			return category, nil
		}
	}
	return inventree.Category{PK: id, Name: "category"}, nil
}

func (f *fakeMilestoneLookupClient) SearchPartParameters(_ context.Context, query inventree.PartParameterQuery) ([]inventree.Parameter, error) {
	f.lastSearchPartParametersQuery = query
	return f.parameters, nil
}

func (f *fakeMilestoneLookupClient) SearchParameterTemplates(_ context.Context, query inventree.SearchQuery) ([]inventree.ParameterTemplate, error) {
	f.lastSearchParameterTemplatesQuery = query
	return f.parameterTemplates, nil
}

func (f *fakeMilestoneLookupClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	f.lastGetParameterTemplateID = id
	for _, template := range f.parameterTemplates {
		if template.PK == id {
			return template, nil
		}
	}
	return inventree.ParameterTemplate{PK: id, Name: "template", Enabled: true}, nil
}

func (f *fakeMilestoneLookupClient) SearchCategoryParameterTemplatesPage(_ context.Context, query inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error) {
	f.lastSearchCategoryParameterTemplatesQuery = query
	if f.categoryParameterScanLimitExceeded {
		next := "next"
		return inventree.Page[inventree.CategoryParameterTemplate]{Next: &next}, nil
	}
	records := make([]inventree.CategoryParameterTemplate, 0, len(f.categoryParameterTemplates))
	for _, link := range f.categoryParameterTemplates {
		if link.Category == query.CategoryID {
			records = append(records, link)
			continue
		}
		if query.FetchParent == nil || !*query.FetchParent {
			continue
		}
		for _, ancestorID := range f.inheritedCategoryIDs {
			if link.Category == ancestorID {
				records = append(records, link)
				break
			}
		}
	}
	return pageSlice(records, query.Offset, query.Limit), nil
}

func (f *fakeMilestoneLookupClient) SearchCompanies(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchCompaniesQuery = query
	return f.companies, nil
}

func (f *fakeMilestoneLookupClient) CreateCompany(_ context.Context, input inventree.CompanyCreate) (inventree.Company, error) {
	f.createCompanyCalls++
	f.createdCompany = true
	f.lastCreateCompany = input
	if f.createCompanyErr != nil {
		return inventree.Company{}, f.createCompanyErr
	}
	return inventree.Company{PK: 30, Name: input.Name, Currency: input.Currency, IsSupplier: input.IsSupplier, IsManufacturer: input.IsManufacturer}, nil
}

func (f *fakeMilestoneLookupClient) GetCompanyDetail(_ context.Context, id int) (inventree.CompanyDetail, error) {
	if f.companyDetailErr != nil {
		return inventree.CompanyDetail{}, f.companyDetailErr
	}
	if f.companyDetail != nil {
		return *f.companyDetail, nil
	}
	for _, company := range f.companies {
		if company.PK == id {
			return inventree.CompanyDetail{Company: company}, nil
		}
	}
	input := f.lastCreateCompany
	return inventree.CompanyDetail{Company: inventree.Company{PK: id, Name: input.Name, Description: input.Description, Currency: input.Currency, IsSupplier: input.IsSupplier, IsManufacturer: input.IsManufacturer}, Website: input.Website}, nil
}

func (f *fakeMilestoneLookupClient) SearchSuppliers(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchSuppliersQuery = query
	return f.suppliers, nil
}

func (f *fakeMilestoneLookupClient) SearchManufacturers(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchManufacturersQuery = query
	if f.searchManufacturersErr != nil {
		return nil, f.searchManufacturersErr
	}
	if len(f.manufacturerSearchResults) > 0 {
		index := min(f.manufacturerSearchCalls, len(f.manufacturerSearchResults)-1)
		f.manufacturerSearchCalls++
		return f.manufacturerSearchResults[index], nil
	}
	return f.manufacturers, nil
}

func (f *fakeMilestoneLookupClient) SearchStockLocations(_ context.Context, query inventree.SearchQuery) ([]inventree.StockLocation, error) {
	f.lastSearchStockLocationsQuery = query
	return f.stockLocations, nil
}

func (f *fakeMilestoneLookupClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	f.lastGetStockLocationID = id
	for _, location := range f.stockLocations {
		if location.PK == id {
			return location, nil
		}
	}
	return inventree.StockLocation{PK: id, Name: "location"}, nil
}

func (f *fakeMilestoneLookupClient) SearchStockItems(_ context.Context, query inventree.StockItemQuery) ([]inventree.StockItem, error) {
	f.lastSearchStockItemsQuery = query
	return f.stockItems, nil
}

func (f *fakeMilestoneLookupClient) GetPartSerialNumbers(_ context.Context, id int) (inventree.PartSerialNumbers, error) {
	f.lastGetPartSerialNumbersID = id
	if f.getPartSerialNumbersErr != nil {
		return inventree.PartSerialNumbers{}, f.getPartSerialNumbersErr
	}
	return f.partSerialNumbers, nil
}

func (f *fakeMilestoneLookupClient) ListAttachments(_ context.Context, query inventree.AttachmentQuery) ([]inventree.Attachment, error) {
	f.lastListAttachmentsQuery = query
	return f.attachments, nil
}

func (f *fakeMilestoneLookupClient) GetAttachmentMetadata(_ context.Context, id int) (inventree.Attachment, error) {
	if f.attachment.PK != 0 {
		return f.attachment, nil
	}
	return inventree.Attachment{PK: id, Filename: "attachment"}, nil
}

func (f *fakeMilestoneLookupClient) DownloadAttachment(_ context.Context, _ int, _ inventree.AttachmentContentMode, maxBytes int64) (inventree.DownloadedAttachment, error) {
	f.lastAttachmentMaxBytes = maxBytes
	return f.downloadedAttachment, nil
}

func (f *fakeMilestoneLookupClient) DownloadPartImage(_ context.Context, _ int, _ inventree.AttachmentContentMode, maxBytes int64) (inventree.DownloadedPartImage, error) {
	f.lastPartImageMaxBytes = maxBytes
	if f.downloadPartImageErr != nil {
		return inventree.DownloadedPartImage{}, f.downloadPartImageErr
	}
	return f.downloadedPartImage, nil
}

func (f *fakeMilestoneLookupClient) UploadAttachment(_ context.Context, input inventree.AttachmentCreate) (inventree.Attachment, error) {
	f.uploadedAttachment = true
	f.lastAttachmentCreate = input
	return inventree.Attachment{PK: 90, ModelType: input.ModelType, ModelID: input.ModelID, Filename: input.Filename, Comment: derefString(input.Comment), FileSize: ptrInt64(int64(len(input.Content)))}, nil
}

func (f *fakeMilestoneLookupClient) CreateLinkAttachment(_ context.Context, input inventree.AttachmentCreate) (inventree.Attachment, error) {
	f.createdLinkAttachment = true
	f.lastAttachmentCreate = input
	return inventree.Attachment{PK: 91, ModelType: input.ModelType, ModelID: input.ModelID, Filename: input.Filename, Link: &input.Link, Comment: derefString(input.Comment), IsLink: true}, nil
}

func (f *fakeMilestoneLookupClient) UpdateAttachmentMetadata(_ context.Context, id int, fields inventree.PatchFields) (inventree.Attachment, error) {
	f.lastUpdateAttachmentFields = fields
	return inventree.Attachment{PK: id, ModelType: "part", ModelID: 10, Filename: "updated"}, nil
}

func (f *fakeMilestoneLookupClient) DeleteAttachment(_ context.Context, id int) error {
	f.deletedAttachment = true
	f.lastDeleteAttachmentID = id
	return nil
}

func (f *fakeMilestoneLookupClient) SetPartPrimaryImage(_ context.Context, partID int, input inventree.PartPrimaryImageCreate) (inventree.Part, error) {
	f.setPartPrimaryImage = true
	f.lastSetPartPrimaryImagePartID = partID
	f.lastSetPartPrimaryImageInput = input
	imageURL := "/media/part_images/" + input.Filename
	return inventree.Part{PK: partID, Image: &imageURL}, nil
}

func (f *fakeMilestoneLookupClient) CreatePart(_ context.Context, input inventree.PartCreate) (inventree.Part, error) {
	f.createdPart = true
	f.lastCreatePart = input
	if f.createPartErr != nil {
		return inventree.Part{}, f.createPartErr
	}
	if f.createPartResult != nil {
		return *f.createPartResult, nil
	}
	return inventree.Part{PK: 10, Name: input.Name, Category: input.Category, Purchaseable: input.Purchaseable != nil && *input.Purchaseable}, nil
}

func (f *fakeMilestoneLookupClient) UpdatePart(_ context.Context, id int, fields inventree.PatchFields) (inventree.Part, error) {
	f.lastUpdatePartFields = fields
	if f.updatePartErr != nil {
		return inventree.Part{}, f.updatePartErr
	}
	return inventree.Part{PK: id}, nil
}

func (f *fakeMilestoneLookupClient) CreatePartParameter(_ context.Context, input inventree.ParameterCreate) (inventree.Parameter, error) {
	f.createdPartParameter = true
	f.createPartParameterCount++
	f.lastCreatePartParameter = input
	return inventree.Parameter{PK: 61, Template: input.Template, ModelType: input.ModelType, ModelID: input.ModelID, Data: input.Data}, nil
}

func (f *fakeMilestoneLookupClient) UpdatePartParameter(_ context.Context, id int, fields inventree.PatchFields) (inventree.Parameter, error) {
	f.updatePartParameterCount++
	f.lastUpdatePartParameterFields = fields
	return inventree.Parameter{PK: id, Data: fmt.Sprint(fields["data"])}, nil
}

func (f *fakeMilestoneLookupClient) SearchSupplierParts(_ context.Context, query inventree.SupplierPartQuery) ([]inventree.SupplierPart, error) {
	f.lastSearchSupplierPartsQuery = query
	return f.supplierParts, nil
}

func (f *fakeMilestoneLookupClient) GetSupplierPart(_ context.Context, id int) (inventree.SupplierPart, error) {
	f.lastGetSupplierPartID = id
	if f.supplierPart.PK != 0 {
		return f.supplierPart, nil
	}
	for _, record := range f.supplierParts {
		if record.PK == id {
			return record, nil
		}
	}
	return inventree.SupplierPart{PK: id, Part: 10, Supplier: 30, SKU: "SKU-1"}, nil
}

func (f *fakeMilestoneLookupClient) CreateSupplierPart(_ context.Context, input inventree.SupplierPartCreate) (inventree.SupplierPart, error) {
	f.createSupplierPartCalls++
	f.createdSupplierPart = true
	f.lastCreateSupplierPart = input
	if f.createSupplierPartErr != nil {
		return inventree.SupplierPart{}, f.createSupplierPartErr
	}
	description := ""
	if input.Description != nil {
		description = *input.Description
	}
	return inventree.SupplierPart{PK: 40, Part: input.Part, Supplier: input.Supplier, SKU: input.SKU, Description: description, Packaging: input.Packaging}, nil
}

func (f *fakeMilestoneLookupClient) GetSupplierPartDetail(_ context.Context, id int) (inventree.SupplierPartDetail, error) {
	if f.supplierPartDetailErr != nil {
		return inventree.SupplierPartDetail{}, f.supplierPartDetailErr
	}
	if f.supplierPartDetail != nil {
		return *f.supplierPartDetail, nil
	}
	for _, record := range f.supplierParts {
		if record.PK == id {
			description := record.Description
			return inventree.SupplierPartDetail{PK: record.PK, Part: record.Part, Supplier: record.Supplier, SKU: record.SKU, Description: &description, Active: record.Active, Primary: record.Primary, Packaging: record.Packaging, PackQuantityNative: record.PackQuantityNative}, nil
		}
	}
	input := f.lastCreateSupplierPart
	return inventree.SupplierPartDetail{PK: id, Part: input.Part, Supplier: input.Supplier, SKU: input.SKU, Description: input.Description, Link: input.Link, ManufacturerPart: input.ManufacturerPart, Packaging: input.Packaging, Note: input.Note, Notes: input.Notes, Available: derefFloat64(input.Available)}, nil
}

func (f *fakeMilestoneLookupClient) SearchManufacturerParts(_ context.Context, query inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error) {
	f.lastSearchManufacturerPartsQuery = query
	return f.manufacturerParts, nil
}

func (f *fakeMilestoneLookupClient) CreateManufacturerPart(_ context.Context, input inventree.ManufacturerPartCreate) (inventree.ManufacturerPart, error) {
	f.createManufacturerPartCalls++
	f.createdManufacturerPart = true
	f.lastCreateManufacturerPart = input
	if f.createManufacturerPartErr != nil {
		return inventree.ManufacturerPart{}, f.createManufacturerPartErr
	}
	mpn := ""
	if input.MPN != nil {
		mpn = *input.MPN
	}
	description := ""
	if input.Description != nil {
		description = *input.Description
	}
	return inventree.ManufacturerPart{PK: 50, Part: input.Part, Manufacturer: input.Manufacturer, MPN: mpn, Description: description}, nil
}

func (f *fakeMilestoneLookupClient) GetManufacturerPartDetail(_ context.Context, id int) (inventree.ManufacturerPartDetail, error) {
	if f.manufacturerPartDetailErr != nil {
		return inventree.ManufacturerPartDetail{}, f.manufacturerPartDetailErr
	}
	if f.manufacturerPartDetail != nil {
		return *f.manufacturerPartDetail, nil
	}
	for _, record := range f.manufacturerParts {
		if record.PK == id {
			mpn := record.MPN
			description := record.Description
			return inventree.ManufacturerPartDetail{PK: record.PK, Part: record.Part, Manufacturer: record.Manufacturer, MPN: &mpn, Description: &description}, nil
		}
	}
	input := f.lastCreateManufacturerPart
	return inventree.ManufacturerPartDetail{PK: id, Part: input.Part, Manufacturer: input.Manufacturer, MPN: input.MPN, Description: input.Description, Link: input.Link, Notes: input.Notes}, nil
}

func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func (f *fakeMilestoneLookupClient) CreateStockItem(_ context.Context, input inventree.StockItemCreate) (inventree.StockItem, error) {
	f.createdStockItem = true
	f.lastCreateStockItem = input
	return inventree.StockItem{PK: 50, Part: input.Part, Location: &input.Location, Quantity: input.Quantity, Status: 10, Batch: input.Batch, Serial: input.Serial, Notes: input.Notes}, nil
}

func ptrInt64(value int64) *int64 {
	return &value
}
