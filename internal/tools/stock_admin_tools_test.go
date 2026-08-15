package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateStockLocationUsesExactParentDuplicateMatrixAndReferences(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockAdminFake()
	fake.locations[10] = inventree.StockLocation{PK: 10, Name: "Warehouse"}
	fake.companies[20] = inventree.Company{PK: 20, Name: "Owner"}
	fake.locationTypes[30] = inventree.StockLocationType{PK: 30, Name: "Shelf"}
	fake.pages[0] = inventree.StockLocationPage{Count: 101, Results: []inventree.StockLocation{{PK: 1, Name: "Other", Parent: dvgoutils.Ptr(10)}}, HasMore: true}
	fake.pages[100] = inventree.StockLocationPage{Count: 101}
	fake.createResult = inventree.StockLocation{PK: 40, Name: "Bin", Parent: dvgoutils.Ptr(10), Owner: dvgoutils.Ptr(20), LocationType: dvgoutils.Ptr(30), Structural: false, External: true}

	_, out, err := createStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "  Bin ", ParentID: dvgoutils.Ptr(10), OwnerID: dvgoutils.Ptr(20), LocationTypeID: dvgoutils.Ptr(30), Structural: dvgoutils.Ptr(false), External: dvgoutils.Ptr(true)})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.NotNil(out.Record)
	a.Equal(40, out.Record.PK)
	a.Equal([]int{0, 100, 0}, fake.pageOffsets)
	a.Equal("Bin", fake.lastCreate.Name)
	a.Equal(10, *fake.lastCreate.Parent)
	a.Equal(20, *fake.lastCreate.Owner)
	a.Equal(30, *fake.lastCreate.LocationType)
}

func TestCreateStockLocationRefusesLaterPageDuplicateAndFailsClosed(t *testing.T) {
	t.Parallel()
	t.Run("later page duplicate", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.pages[0] = inventree.StockLocationPage{HasMore: true}
		fake.pages[100] = inventree.StockLocationPage{Results: []inventree.StockLocation{{PK: 2, Name: " bIn "}}}
		_, out, err := createStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "Bin"})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.createCalls)
	})
	t.Run("scan bound", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		for offset := 0; offset < stockLocationScanLimit; offset += stockLocationPageSize {
			fake.pages[offset] = inventree.StockLocationPage{HasMore: true}
		}
		_, out, err := createStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "Bin"})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Contains(t, out.Clarification.Reason, "safety limit")
	})
}

func TestCreateStockLocationRecoversPostPersistResponseLoss(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockAdminFake()
	fake.createResult = inventree.StockLocation{PK: 40, Name: "Bin", Description: "stored"}
	fake.createErr = errors.New("connection reset after persist")
	_, out, err := createStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "Bin", Description: dvgoutils.Ptr("stored")})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Recovered)
	assert.Equal(t, 40, out.Record.PK)
}

func TestUpdateStockLocationKeepsOperationalFieldsOutOfOrdinaryPatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockAdminFake()
	fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin", Description: "old", Parent: dvgoutils.Ptr(10), Owner: dvgoutils.Ptr(20), CustomIcon: dvgoutils.Ptr("box"), LocationType: dvgoutils.Ptr(30), Structural: true, External: true}

	_, out, err := updateStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationInput{ID: 40, Name: dvgoutils.Ptr(" Bin A "), Description: dvgoutils.Ptr(""), ClearOwner: true, ClearCustomIcon: true, ClearLocationType: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(fake.lastPatchHas("name"))
	a.True(fake.lastPatchHas("description"))
	a.True(fake.lastPatchNull("owner"))
	a.True(fake.lastPatchNull("custom_icon"))
	a.True(fake.lastPatchNull("location_type"))
	a.False(fake.lastPatchHas("parent"))
	a.False(fake.lastPatchHas("structural"))
	a.False(fake.lastPatchHas("external"))
	a.True(out.Record.Structural)
	a.True(out.Record.External)
}

func TestRestructureStockLocationRequiresCurrentPlanAndRefusesCycles(t *testing.T) {
	t.Parallel()
	t.Run("review and execute", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.locations[10] = inventree.StockLocation{PK: 10, Name: "Root"}
		fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin", Structural: false, External: false}
		input := RestructureStockLocationInput{ID: 40, ParentID: dvgoutils.Ptr(10), Structural: dvgoutils.Ptr(true), External: dvgoutils.Ptr(true), DryRun: true}
		_, plan, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		require.NotEmpty(t, plan.PlanHash)
		require.NotNil(t, plan.Plan.TargetParent)
		assert.Equal(t, 10, plan.Plan.TargetParent.ID)
		assert.Equal(t, 10, *plan.Plan.After.ParentID)
		assert.Zero(t, fake.updateLocationCalls)

		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, out, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		assert.Equal(t, StatusOK, out.Status)
		assert.Equal(t, 1, fake.updateLocationCalls)
		assert.Equal(t, 10, *out.Record.Parent)
		assert.True(t, out.Record.Structural)
		assert.True(t, out.Record.External)
	})
	t.Run("stale plan", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin"}
		_, out, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: 40, Structural: dvgoutils.Ptr(true), Confirm: true, PlanHash: "stale"})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.updateLocationCalls)
	})
	t.Run("target parent context drift", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.locations[10] = inventree.StockLocation{PK: 10, Name: "Root", PathString: "Root", Path: []inventree.TreePath{{PK: 10, Name: "Root"}}}
		fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin"}
		input := RestructureStockLocationInput{ID: 40, ParentID: dvgoutils.Ptr(10), DryRun: true}
		_, plan, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		fake.locations[10] = inventree.StockLocation{PK: 10, Name: "Root", PathString: "Root", Path: []inventree.TreePath{{PK: 10, Name: "Root"}}, Structural: true}
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, out, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.updateLocationCalls)
	})
	t.Run("descendant parent", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Parent"}
		fake.locations[50] = inventree.StockLocation{PK: 50, Name: "Child", Parent: dvgoutils.Ptr(40)}
		_, out, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: 40, ParentID: dvgoutils.Ptr(50), DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.updateLocationCalls)
	})
}

func TestUpdateStockItemMetadataBindsCompleteStateAndApprovedFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockAdminFake()
	fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Location: dvgoutils.Ptr(40), Quantity: 8, Serial: dvgoutils.Ptr("S-1"), Status: 10, DeleteOnDeplete: true, Batch: dvgoutils.Ptr("old"), Packaging: dvgoutils.Ptr("reel"), Notes: dvgoutils.Ptr("old note")}
	completeLink := "https://example.test/item?account=42#details"
	input := UpdateStockItemMetadataInput{ID: 50, Batch: dvgoutils.Ptr("B-2"), ExpiryDate: dvgoutils.Ptr("2027-01-02"), ClearPackaging: true, Notes: dvgoutils.Ptr("checked"), Link: &completeLink, DryRun: true}

	_, plan, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)
	a.Equal(float64(8), plan.Plan.Before.Quantity)
	a.Equal("S-1", *plan.Plan.Before.Serial)
	a.Equal("B-2", *plan.Plan.After.Batch)
	a.Nil(plan.Plan.After.Packaging)
	a.Zero(fake.updateStockCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, fake.updateStockCalls)
	r.NotNil(out.Record)
	a.Equal(completeLink, out.Record.Link)
	a.True(fake.lastPatchHas("batch"))
	a.True(fake.lastPatchHas("expiry_date"))
	a.True(fake.lastPatchNull("packaging"))
	a.True(fake.lastPatchHas("notes"))
	a.True(fake.lastPatchHas("link"))
	for _, forbidden := range []string{"location", "quantity", "status", "serial", "owner", "supplier_part", "purchase_price", "delete_on_deplete", "belongs_to", "purchase_order", "build"} {
		a.False(fake.lastPatchHas(forbidden), forbidden)
	}
}

func TestUpdateStockItemMetadataRejectsUnsafeLinkAndRecoversUnknownResult(t *testing.T) {
	t.Parallel()
	t.Run("unsafe link", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1}
		_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockItemMetadataInput{ID: 50, Link: dvgoutils.Ptr("https://user:pass@example.test/private"), DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("post persist response loss", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1, Batch: dvgoutils.Ptr("old")}
		input := UpdateStockItemMetadataInput{ID: 50, Batch: dvgoutils.Ptr("new"), DryRun: true}
		_, plan, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		fake.updateStockErr = errors.New("response lost")
		_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		assert.Equal(t, StatusOK, out.Status)
		assert.True(t, out.Recovered)
	})
}

func TestSanitizedStockItemPreservesValidCompleteLinkAndOmitsCredentials(t *testing.T) {
	t.Parallel()
	item := sanitizedStockItem(inventree.StockItem{PK: 50, Link: "https://user:pass@example.test/path?token=secret#fragment"})
	assert.Empty(t, item.Link)
	plan := projectStockMetadataPlan(StockMetadataPlan{
		Before: StockMetadataState{Link: "https://user:pass@example.test/before?token=secret#fragment"},
		After:  StockMetadataState{Link: "https://example.test/after?token=secret#fragment"},
	})
	assert.Empty(t, plan.Before.Link)
	assert.Equal(t, "https://example.test/after?token=secret#fragment", plan.After.Link)
}

func TestStockAdministrationExactLookupHandlers(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockAdminFake()
	fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin"}
	fake.locationTypes[3] = inventree.StockLocationType{PK: 3, Name: "Shelf"}
	fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 2, Link: "https://example.test/item?secret=value"}

	_, location, err := getStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 40})
	r.NoError(err)
	a.Equal(StatusOK, location.Status)
	a.Equal("Bin", location.Record.Name)
	_, locationType, err := getStockLocationType(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 3})
	r.NoError(err)
	a.Equal("Shelf", locationType.Record.Name)
	_, types, err := searchStockLocationTypes(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "shelf"})
	r.NoError(err)
	a.Equal(StatusOK, types.Status)
	r.Len(types.Results, 1)
	_, stock, err := getStockItem(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 50})
	r.NoError(err)
	a.Equal("https://example.test/item?secret=value", stock.Record.Link)
}

func TestStockAdministrationPlanAndReadbackFailuresDoNotWriteBlindly(t *testing.T) {
	t.Parallel()
	t.Run("metadata needs reviewed plan", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1}
		_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockItemMetadataInput{ID: 50, Batch: dvgoutils.Ptr("new")})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Equal(t, "plan_hash", out.Clarification.Retry)
		assert.Zero(t, fake.updateStockCalls)
	})
	t.Run("protected state invalidates metadata plan", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1, StatusCustomKey: dvgoutils.Ptr(10), Link: "https://example.test/item?token=one"}
		input := UpdateStockItemMetadataInput{ID: 50, Batch: dvgoutils.Ptr("new"), DryRun: true}
		_, plan, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 2, StatusCustomKey: dvgoutils.Ptr(11), Link: "https://example.test/item?token=two"}
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.updateStockCalls)
		assert.Empty(t, out.Plan.Before.Link)
	})
	t.Run("raw link state invalidates metadata plan", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1, Link: "https://example.test/item?token=one"}
		input := UpdateStockItemMetadataInput{ID: 50, Batch: dvgoutils.Ptr("new"), DryRun: true}
		_, plan, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1, Link: "https://example.test/item?token=two"}
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.updateStockCalls)
		assert.NotEmpty(t, plan.Plan.Before.Link)
		assert.Empty(t, out.Plan.Before.Link)
	})
	t.Run("location readback divergence", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.locations[40] = inventree.StockLocation{PK: 40, Name: "Bin", Description: "old"}
		fake.suppressLocationPatch = true
		_, out, err := updateStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationInput{ID: 40, Description: dvgoutils.Ptr("new")})
		require.NoError(t, err)
		assert.Equal(t, StatusPartialFailure, out.Status)
		assert.Contains(t, out.RecoveryPlan, "read-back")
	})
	t.Run("invalid metadata shapes", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		fake.stockItems[50] = inventree.StockItem{PK: 50, Part: 5, Quantity: 1, Batch: dvgoutils.Ptr("old")}
		for _, input := range []UpdateStockItemMetadataInput{
			{ID: 50},
			{ID: 50, Batch: dvgoutils.Ptr("new"), ClearBatch: true},
			{ID: 50, ExpiryDate: dvgoutils.Ptr("tomorrow")},
			{ID: 50, Batch: dvgoutils.Ptr("old")},
		} {
			_, out, err := updateStockItemMetadata(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			assert.Equal(t, StatusClarificationRequired, out.Status)
		}
		assert.Zero(t, fake.updateStockCalls)
	})
}

func TestStockAdministrationPatchValidationEdges(t *testing.T) {
	t.Parallel()
	t.Run("ordinary location patch", func(t *testing.T) {
		before := inventree.StockLocation{Name: "Bin"}
		_, _, err := ordinaryLocationPatch(UpdateStockLocationInput{}, before)
		require.ErrorContains(t, err, "at least one")
		_, _, err = ordinaryLocationPatch(UpdateStockLocationInput{Name: dvgoutils.Ptr("   ")}, before)
		require.ErrorContains(t, err, "nonblank")
		_, _, err = ordinaryLocationPatch(UpdateStockLocationInput{OwnerID: dvgoutils.Ptr(1), ClearOwner: true}, before)
		require.ErrorContains(t, err, "conflict")
	})
	t.Run("create fields preserve explicit values", func(t *testing.T) {
		fields := locationCreateFields(CreateStockLocationInput{
			Description:    dvgoutils.Ptr(""),
			ParentID:       dvgoutils.Ptr(1),
			OwnerID:        dvgoutils.Ptr(2),
			CustomIcon:     dvgoutils.Ptr("box"),
			Structural:     dvgoutils.Ptr(false),
			External:       dvgoutils.Ptr(false),
			LocationTypeID: dvgoutils.Ptr(3),
		}, "Bin")
		assert.Len(t, fields, 8)
	})
	t.Run("parent constraints", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		_, err := validateStockLocationParent(ctx, fake, 1, 0)
		require.ErrorContains(t, err, "positive")
		_, err = validateStockLocationParent(ctx, fake, 1, 1)
		require.ErrorIs(t, err, errStockLocationSelfParent)
	})
	t.Run("reference identities", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		err := validateLocationReferences(ctx, fake, dvgoutils.Ptr(0), nil, nil)
		require.ErrorContains(t, err, "positive")
		fake.locations[1] = inventree.StockLocation{PK: 9}
		err = validateLocationReferences(ctx, fake, dvgoutils.Ptr(1), nil, nil)
		require.ErrorIs(t, err, errStockAdminInvalidReference)
		fake.locations[1] = inventree.StockLocation{PK: 1}
		fake.companies[2] = inventree.Company{PK: 9}
		err = validateLocationReferences(ctx, fake, dvgoutils.Ptr(1), dvgoutils.Ptr(2), nil)
		require.ErrorIs(t, err, errStockAdminInvalidReference)
		fake.companies[2] = inventree.Company{PK: 2}
		fake.locationTypes[3] = inventree.StockLocationType{PK: 9}
		err = validateLocationReferences(ctx, fake, dvgoutils.Ptr(1), dvgoutils.Ptr(2), dvgoutils.Ptr(3))
		require.ErrorIs(t, err, errStockAdminInvalidReference)
	})
	t.Run("invalid references return clarification", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockAdminFake()
		_, created, err := createStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "Bin", OwnerID: dvgoutils.Ptr(0)})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, created.Status)
		fake.locations[1] = inventree.StockLocation{PK: 1, Name: "Bin"}
		_, restructured, err := restructureStockLocation(stockAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: 1, ParentID: dvgoutils.Ptr(0), DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, restructured.Status)
	})
	t.Run("safe error omits upstream detail", func(t *testing.T) {
		err := safeStockAdminError("stock-location lookup")
		assert.EqualError(t, err, "stock-location lookup failed; inspect InvenTree availability and permissions before retrying")
	})
}

type stockAdminFake struct {
	locations             map[int]inventree.StockLocation
	stockItems            map[int]inventree.StockItem
	companies             map[int]inventree.Company
	locationTypes         map[int]inventree.StockLocationType
	pages                 map[int]inventree.StockLocationPage
	pageOffsets           []int
	createResult          inventree.StockLocation
	createErr             error
	createCalls           int
	updateLocationCalls   int
	updateStockCalls      int
	updateStockErr        error
	suppressLocationPatch bool
	lastCreate            inventree.StockLocationCreate
	lastPatch             inventree.PatchFields
}

func newStockAdminFake() *stockAdminFake {
	return &stockAdminFake{locations: map[int]inventree.StockLocation{}, stockItems: map[int]inventree.StockItem{}, companies: map[int]inventree.Company{}, locationTypes: map[int]inventree.StockLocationType{}, pages: map[int]inventree.StockLocationPage{}}
}

func stockAdminDeps(fake *stockAdminFake) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func (f *stockAdminFake) GetCompany(_ context.Context, id int) (inventree.Company, error) {
	value, ok := f.companies[id]
	if !ok {
		return inventree.Company{}, errors.New("not found")
	}
	return value, nil
}

func (f *stockAdminFake) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	value, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, errors.New("not found")
	}
	return value, nil
}

func (f *stockAdminFake) SearchStockLocationsPage(_ context.Context, query inventree.StockLocationQuery) (inventree.StockLocationPage, error) {
	f.pageOffsets = append(f.pageOffsets, query.Offset)
	if f.createCalls > 0 && f.createResult.PK != 0 {
		return inventree.StockLocationPage{Count: 1, Results: []inventree.StockLocation{f.createResult}}, nil
	}
	if page, ok := f.pages[query.Offset]; ok {
		return page, nil
	}
	results := make([]inventree.StockLocation, 0)
	for _, location := range f.locations {
		if sameOptionalInt(location.Parent, query.Parent) {
			results = append(results, location)
		}
	}
	return inventree.StockLocationPage{Count: len(results), Results: results}, nil
}

func (f *stockAdminFake) GetStockLocationType(_ context.Context, id int) (inventree.StockLocationType, error) {
	value, ok := f.locationTypes[id]
	if !ok {
		return inventree.StockLocationType{}, errors.New("not found")
	}
	return value, nil
}

func (f *stockAdminFake) SearchStockLocationTypes(_ context.Context, query inventree.SearchQuery) ([]inventree.StockLocationType, error) {
	results := make([]inventree.StockLocationType, 0)
	for _, value := range f.locationTypes {
		if query.Search == "" || strings.Contains(strings.ToLower(value.Name), strings.ToLower(query.Search)) {
			results = append(results, value)
		}
	}
	return results, nil
}

func (f *stockAdminFake) CreateStockLocation(_ context.Context, input inventree.StockLocationCreate) (inventree.StockLocation, error) {
	f.createCalls++
	f.lastCreate = input
	f.locations[f.createResult.PK] = f.createResult
	return f.createResult, f.createErr
}

func (f *stockAdminFake) UpdateStockLocation(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockLocation, error) {
	f.updateLocationCalls++
	f.lastPatch = fields
	value := f.locations[id]
	if !f.suppressLocationPatch {
		applyLocationPatch(&value, fields)
	}
	f.locations[id] = value
	return value, nil
}

func (f *stockAdminFake) GetStockItem(_ context.Context, id int) (inventree.StockItem, error) {
	value, ok := f.stockItems[id]
	if !ok {
		return inventree.StockItem{}, errors.New("not found")
	}
	return value, nil
}

func (f *stockAdminFake) UpdateStockItem(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockItem, error) {
	f.updateStockCalls++
	f.lastPatch = fields
	value := f.stockItems[id]
	applyStockPatch(&value, fields)
	f.stockItems[id] = value
	return value, f.updateStockErr
}

func (f *stockAdminFake) lastPatchHas(key string) bool { _, ok := f.lastPatch[key]; return ok }

func (f *stockAdminFake) lastPatchNull(key string) bool {
	value, ok := f.lastPatch[key]
	return ok && value.Value() == nil
}

func applyLocationPatch(value *inventree.StockLocation, fields inventree.PatchFields) {
	for key, patch := range fields {
		switch key {
		case "name":
			value.Name = patch.Value().(string)
		case "description":
			value.Description = patch.Value().(string)
		case "parent":
			value.Parent = stockTestIntPointer(patch.Value())
		case "owner":
			value.Owner = stockTestIntPointer(patch.Value())
		case "custom_icon":
			value.CustomIcon = stockTestStringPointer(patch.Value())
		case "location_type":
			value.LocationType = stockTestIntPointer(patch.Value())
		case "structural":
			value.Structural = patch.Value().(bool)
		case "external":
			value.External = patch.Value().(bool)
		}
	}
}

func applyStockPatch(value *inventree.StockItem, fields inventree.PatchFields) {
	for key, patch := range fields {
		switch key {
		case "batch":
			value.Batch = stockTestStringPointer(patch.Value())
		case "expiry_date":
			value.ExpiryDate = stockTestStringPointer(patch.Value())
		case "packaging":
			value.Packaging = stockTestStringPointer(patch.Value())
		case "notes":
			value.Notes = stockTestStringPointer(patch.Value())
		case "link":
			value.Link = patch.Value().(string)
		}
	}
}

func stockTestIntPointer(value any) *int {
	if value == nil {
		return nil
	}
	result := value.(int)
	return &result
}

func stockTestStringPointer(value any) *string {
	if value == nil {
		return nil
	}
	result := value.(string)
	return &result
}
