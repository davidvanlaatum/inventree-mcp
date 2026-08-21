package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanStockLocationDeletePagesKeepsLocallyFilteredIDs(t *testing.T) {
	ids, err := scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		if offset == 0 {
			return 3, []int{20}, 2, true, nil
		}
		return 3, []int{30}, 1, false, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{20, 30}, ids)
}

func TestScanStockLocationDeletePagesFailsClosedOnIncompleteFinalPage(t *testing.T) {
	_, err := scanStockLocationDeletePages(func(int) (int, []int, int, bool, error) { return 2, []int{20}, 1, false, nil })
	assert.ErrorIs(t, err, errStockLocationDeleteScanLimit)
}

func TestStockLocationDeletePlanStoreBindsPlanAndConsumesOnce(t *testing.T) {
	now := time.Now()
	store := newStockLocationDeletePlanStore(func() time.Time { return now }, func() (string, error) { return "token", nil })
	ctx := context.Background()
	plan := StockLocationDeletePlan{Action: DeleteStockLocationToolName, Location: inventree.StockLocation{PK: 7}, References: StockLocationDeleteReferences{}}
	token, err := store.issue(ctx, plan)
	require.NoError(t, err)
	assert.True(t, store.consume(ctx, token, plan))
	assert.False(t, store.consume(ctx, token, plan))
}

func TestStockLocationDeletePlanStoreRejectsPrincipalAndExpiredTokens(t *testing.T) {
	now := time.Now()
	store := newStockLocationDeletePlanStore(func() time.Time { return now }, func() (string, error) { return "token", nil })
	plan := StockLocationDeletePlan{Action: DeleteStockLocationToolName, Location: inventree.StockLocation{PK: 7}}
	token, err := store.issue(context.Background(), plan)
	require.NoError(t, err)
	store.principal = func(context.Context) string { return "other-principal" }
	assert.False(t, store.consume(context.Background(), token, plan))
	store.principal = stockPlanPrincipal
	now = now.Add(stockLocationDeletePlanLifetime + time.Second)
	assert.False(t, store.consume(context.Background(), token, plan))
}

func TestDeleteStockLocationRefusesEveryDependencySurface(t *testing.T) {
	cases := []struct {
		name  string
		set   func(*stockLocationDeleteFake)
		check func(*testing.T, StockLocationDeleteOutput)
	}{
		{"stock item", func(f *stockLocationDeleteFake) { f.stockItems = []inventree.StockItem{{PK: 11}} }, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{11}, out.References.StockItemIDs)
		}},
		{"child location", func(f *stockLocationDeleteFake) { f.children = []inventree.StockLocation{{PK: 12}} }, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{12}, out.References.ChildLocationIDs)
		}},
		{"part default", func(f *stockLocationDeleteFake) { f.parts = []inventree.Part{{PK: 13, DefaultLocation: intPtr(7)}} }, func(t *testing.T, out StockLocationDeleteOutput) { assert.Equal(t, []int{13}, out.References.PartIDs) }},
		{"category default", func(f *stockLocationDeleteFake) {
			f.categories = []inventree.Category{{PK: 14, DefaultLocation: intPtr(7)}}
		}, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{14}, out.References.CategoryIDs)
		}},
		{"purchase order", func(f *stockLocationDeleteFake) {
			f.orders = []inventree.PurchaseOrder{{PK: 15, Destination: intPtr(7)}}
		}, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{15}, out.References.PurchaseOrderIDs)
		}},
		{"purchase order line", func(f *stockLocationDeleteFake) {
			f.lines = []inventree.PurchaseOrderLineItem{{PK: 16, Destination: intPtr(7)}}
		}, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{16}, out.References.PurchaseOrderLineIDs)
		}},
		{"parameter", func(f *stockLocationDeleteFake) { f.parameters = []inventree.Parameter{{PK: 17}} }, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{17}, out.References.ParameterValueIDs)
		}},
		{"build", func(f *stockLocationDeleteFake) { f.builds = []inventree.Build{{PK: 18, Destination: intPtr(7)}} }, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{18}, out.References.BuildIDs)
		}},
		{"build source", func(f *stockLocationDeleteFake) { f.builds = []inventree.Build{{PK: 181, TakeFrom: intPtr(7)}} }, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{181}, out.References.BuildIDs)
		}},
		{"transfer order", func(f *stockLocationDeleteFake) {
			f.transferOrders = []inventree.TransferOrder{{PK: 19, Destination: intPtr(7)}}
		}, func(t *testing.T, out StockLocationDeleteOutput) {
			assert.Equal(t, []int{19}, out.References.TransferOrderIDs)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7, Name: "Shelf"}}
			tc.set(fake)
			_, out, err := deleteStockLocation(stockLocationDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationInput{ID: 7})
			require.NoError(t, err)
			assert.Equal(t, StatusClarificationRequired, out.Status)
			tc.check(t, out)
			assert.Zero(t, fake.deleteCalls)
		})
	}
}

func TestDeleteStockLocationPreviewAndConfirmedDelete(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7, Name: "Shelf"}}
	deps := stockLocationDeleteDeps(fake)
	_, preview, err := deleteStockLocation(deps)(ctx, &mcp.CallToolRequest{}, DeleteStockLocationInput{ID: 7})
	require.NoError(t, err)
	require.NotEmpty(t, preview.PlanHash)
	_, out, err := deleteStockLocation(deps)(ctx, &mcp.CallToolRequest{}, DeleteStockLocationInput{ID: 7, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Verified)
	assert.Equal(t, 1, fake.deleteCalls)
}

func TestVerifyStockLocationDeletionRecoversFromLostMutationResponse(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7}, deleted: true}
	_, out, err := verifyStockLocationDeletion(ctx, fake, fake.location, StockLocationDeleteReferences{}, errors.New("transport closed after request"))
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Verified)
	assert.True(t, out.Recovered)
}

func TestVerifyStockLocationDeletionRefusesDefiniteUpstreamRejection(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7}}
	_, _, err := verifyStockLocationDeletion(ctx, fake, fake.location, StockLocationDeleteReferences{}, &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation})
	assert.Error(t, err)
}

func TestVerifyStockLocationDeletionReportsUnverifiableReadBack(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7}, readErr: errors.New("read failed")}
	_, out, err := verifyStockLocationDeletion(ctx, fake, fake.location, StockLocationDeleteReferences{}, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestVerifyStockLocationDeletionReportsSurvivingLocation(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7}}
	_, out, err := verifyStockLocationDeletion(ctx, fake, fake.location, StockLocationDeleteReferences{}, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestDeleteStockLocationRejectsStalePlan(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &stockLocationDeleteFake{location: inventree.StockLocation{PK: 7, Name: "Shelf"}}
	deps := stockLocationDeleteDeps(fake)
	_, preview, err := deleteStockLocation(deps)(ctx, &mcp.CallToolRequest{}, DeleteStockLocationInput{ID: 7})
	require.NoError(t, err)
	fake.location.Name = "Changed"
	_, out, err := deleteStockLocation(deps)(ctx, &mcp.CallToolRequest{}, DeleteStockLocationInput{ID: 7, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.deleteCalls)
}

func intPtr(value int) *int { return &value }

type stockLocationDeleteFake struct {
	location       inventree.StockLocation
	stockItems     []inventree.StockItem
	children       []inventree.StockLocation
	parts          []inventree.Part
	categories     []inventree.Category
	orders         []inventree.PurchaseOrder
	lines          []inventree.PurchaseOrderLineItem
	parameters     []inventree.Parameter
	builds         []inventree.Build
	transferOrders []inventree.TransferOrder
	deleted        bool
	deleteCalls    int
	readErr        error
}

func (f *stockLocationDeleteFake) GetStockLocation(context.Context, int) (inventree.StockLocation, error) {
	if f.deleted {
		return inventree.StockLocation{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	if f.readErr != nil {
		return inventree.StockLocation{}, f.readErr
	}
	return f.location, nil
}
func (f *stockLocationDeleteFake) SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error) {
	return inventree.StockItemPage{Count: len(f.stockItems), Results: f.stockItems}, nil
}
func (f *stockLocationDeleteFake) SearchStockLocationsPage(context.Context, inventree.StockLocationQuery) (inventree.StockLocationPage, error) {
	return inventree.StockLocationPage{Count: len(f.children), Results: f.children}, nil
}
func (f *stockLocationDeleteFake) SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error) {
	return inventree.PartPage{Count: len(f.parts), Results: f.parts}, nil
}
func (f *stockLocationDeleteFake) SearchPartCategoriesPage(context.Context, inventree.CategoryQuery) (inventree.CategoryPage, error) {
	return inventree.CategoryPage{Count: len(f.categories), Results: f.categories}, nil
}
func (f *stockLocationDeleteFake) SearchObjectParametersPage(context.Context, inventree.ObjectParameterQuery) (inventree.PartParameterPage, error) {
	return inventree.PartParameterPage{Count: len(f.parameters), Results: f.parameters}, nil
}
func (f *stockLocationDeleteFake) SearchPurchaseOrdersPage(context.Context, inventree.PurchaseOrderQuery) (inventree.PurchaseOrderPage, error) {
	return inventree.PurchaseOrderPage{Count: len(f.orders), Results: f.orders}, nil
}
func (f *stockLocationDeleteFake) SearchPurchaseOrderLinesPage(context.Context, inventree.PurchaseOrderLineQuery) (inventree.PurchaseOrderLinePage, error) {
	return inventree.PurchaseOrderLinePage{Count: len(f.lines), Results: f.lines}, nil
}
func (f *stockLocationDeleteFake) SearchBuildsPage(context.Context, inventree.BuildQuery) (inventree.BuildPage, error) {
	return inventree.BuildPage{Count: len(f.builds), Results: f.builds}, nil
}
func (f *stockLocationDeleteFake) SearchTransferOrdersPage(context.Context, inventree.TransferOrderQuery) (inventree.TransferOrderPage, error) {
	return inventree.TransferOrderPage{Count: len(f.transferOrders), Results: f.transferOrders}, nil
}
func (f *stockLocationDeleteFake) DeleteStockLocation(context.Context, int) error {
	f.deleteCalls++
	f.deleted = true
	return nil
}

func stockLocationDeleteDeps(fake *stockLocationDeleteFake) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, EnableWriteTools: true, stockLocationDeletePlanStore: newStockLocationDeletePlanStore(time.Now, func() (string, error) { return "token-" + time.Now().Format("150405.000000000"), nil })}
}

var _ StockLocationDeleteClient = (*stockLocationDeleteFake)(nil)
