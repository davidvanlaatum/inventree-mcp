package tools

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePricingClient struct {
	internal     map[int]inventree.PartInternalPriceBreak
	sale         map[int]inventree.PartSalePriceBreak
	supplier     map[int]inventree.SupplierPriceBreak
	parts        map[int]inventree.Part
	pricing      map[int]inventree.PartPricing
	nextID       int
	createErr    error
	updateErr    error
	deleteErr    error
	refreshErr   error
	forceHasMore bool
}

func newFakePricingClient() *fakePricingClient {
	return &fakePricingClient{
		internal: map[int]inventree.PartInternalPriceBreak{},
		sale:     map[int]inventree.PartSalePriceBreak{},
		supplier: map[int]inventree.SupplierPriceBreak{},
		parts:    map[int]inventree.Part{},
		pricing:  map[int]inventree.PartPricing{},
		nextID:   100,
	}
}

func (f *fakePricingClient) allocID() int {
	id := f.nextID
	f.nextID++
	return id
}

func (f *fakePricingClient) SearchPartInternalPriceBreaksPage(_ context.Context, query inventree.PartInternalPriceBreakQuery) (inventree.PartInternalPriceBreakPage, error) {
	var all []inventree.PartInternalPriceBreak
	for _, record := range f.internal {
		if query.Part > 0 && record.Part != query.Part {
			continue
		}
		all = append(all, record)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PK < all[j].PK })
	start := min(query.Offset, len(all))
	end := len(all)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return inventree.PartInternalPriceBreakPage{Count: len(all), Results: all[start:end], HasMore: f.forceHasMore || end < len(all)}, nil
}

func (f *fakePricingClient) GetPartInternalPriceBreak(_ context.Context, id int) (inventree.PartInternalPriceBreak, error) {
	record, ok := f.internal[id]
	if !ok {
		return inventree.PartInternalPriceBreak{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePricingClient) CreatePartInternalPriceBreak(_ context.Context, input inventree.PartInternalPriceBreakCreate) (inventree.PartInternalPriceBreak, error) {
	if f.createErr != nil {
		return inventree.PartInternalPriceBreak{}, f.createErr
	}
	id := f.allocID()
	record := inventree.PartInternalPriceBreak{PK: id, Part: input.Part, Quantity: input.Quantity, Price: inventree.DecimalString(input.Price), PriceCurrency: input.PriceCurrency}
	f.internal[id] = record
	return record, nil
}

func (f *fakePricingClient) UpdatePartInternalPriceBreak(_ context.Context, id int, fields inventree.PatchFields) (inventree.PartInternalPriceBreak, error) {
	if f.updateErr != nil {
		return inventree.PartInternalPriceBreak{}, f.updateErr
	}
	record := f.internal[id]
	if value, ok := fields["price"]; ok {
		record.Price = inventree.DecimalString(value.Value().(string))
	}
	if value, ok := fields["price_currency"]; ok {
		record.PriceCurrency = value.Value().(string)
	}
	f.internal[id] = record
	return record, nil
}

func (f *fakePricingClient) DeletePartInternalPriceBreak(_ context.Context, id int) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.internal, id)
	return nil
}

func (f *fakePricingClient) SearchPartSalePriceBreaksPage(_ context.Context, query inventree.PartSalePriceBreakQuery) (inventree.PartSalePriceBreakPage, error) {
	var all []inventree.PartSalePriceBreak
	for _, record := range f.sale {
		if query.Part > 0 && record.Part != query.Part {
			continue
		}
		all = append(all, record)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PK < all[j].PK })
	start := min(query.Offset, len(all))
	end := len(all)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return inventree.PartSalePriceBreakPage{Count: len(all), Results: all[start:end], HasMore: f.forceHasMore || end < len(all)}, nil
}

func (f *fakePricingClient) GetPartSalePriceBreak(_ context.Context, id int) (inventree.PartSalePriceBreak, error) {
	record, ok := f.sale[id]
	if !ok {
		return inventree.PartSalePriceBreak{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePricingClient) CreatePartSalePriceBreak(_ context.Context, input inventree.PartSalePriceBreakCreate) (inventree.PartSalePriceBreak, error) {
	if f.createErr != nil {
		return inventree.PartSalePriceBreak{}, f.createErr
	}
	id := f.allocID()
	record := inventree.PartSalePriceBreak{PK: id, Part: input.Part, Quantity: input.Quantity, Price: inventree.DecimalString(input.Price), PriceCurrency: input.PriceCurrency}
	f.sale[id] = record
	return record, nil
}

func (f *fakePricingClient) UpdatePartSalePriceBreak(_ context.Context, id int, fields inventree.PatchFields) (inventree.PartSalePriceBreak, error) {
	if f.updateErr != nil {
		return inventree.PartSalePriceBreak{}, f.updateErr
	}
	record := f.sale[id]
	if value, ok := fields["price"]; ok {
		record.Price = inventree.DecimalString(value.Value().(string))
	}
	if value, ok := fields["price_currency"]; ok {
		record.PriceCurrency = value.Value().(string)
	}
	f.sale[id] = record
	return record, nil
}

func (f *fakePricingClient) DeletePartSalePriceBreak(_ context.Context, id int) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.sale, id)
	return nil
}

func (f *fakePricingClient) SearchSupplierPriceBreaksPage(_ context.Context, query inventree.SupplierPriceBreakQuery) (inventree.SupplierPriceBreakPage, error) {
	var all []inventree.SupplierPriceBreak
	for _, record := range f.supplier {
		if query.SupplierPart > 0 && record.SupplierPart != query.SupplierPart {
			continue
		}
		all = append(all, record)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PK < all[j].PK })
	start := min(query.Offset, len(all))
	end := len(all)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return inventree.SupplierPriceBreakPage{Count: len(all), Results: all[start:end], HasMore: f.forceHasMore || end < len(all)}, nil
}

func (f *fakePricingClient) GetSupplierPriceBreak(_ context.Context, id int) (inventree.SupplierPriceBreak, error) {
	record, ok := f.supplier[id]
	if !ok {
		return inventree.SupplierPriceBreak{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePricingClient) CreateSupplierPriceBreak(_ context.Context, input inventree.SupplierPriceBreakCreate) (inventree.SupplierPriceBreak, error) {
	if f.createErr != nil {
		return inventree.SupplierPriceBreak{}, f.createErr
	}
	id := f.allocID()
	record := inventree.SupplierPriceBreak{PK: id, SupplierPart: input.SupplierPart, Quantity: input.Quantity, Price: inventree.DecimalString(input.Price), PriceCurrency: input.PriceCurrency, Supplier: 1}
	f.supplier[id] = record
	return record, nil
}

func (f *fakePricingClient) UpdateSupplierPriceBreak(_ context.Context, id int, fields inventree.PatchFields) (inventree.SupplierPriceBreak, error) {
	if f.updateErr != nil {
		return inventree.SupplierPriceBreak{}, f.updateErr
	}
	record := f.supplier[id]
	if value, ok := fields["price"]; ok {
		record.Price = inventree.DecimalString(value.Value().(string))
	}
	if value, ok := fields["price_currency"]; ok {
		record.PriceCurrency = value.Value().(string)
	}
	f.supplier[id] = record
	return record, nil
}

func (f *fakePricingClient) DeleteSupplierPriceBreak(_ context.Context, id int) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.supplier, id)
	return nil
}

func (f *fakePricingClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	record, ok := f.parts[id]
	if !ok {
		return inventree.Part{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePricingClient) GetPartPricing(_ context.Context, partID int) (inventree.PartPricing, error) {
	record, ok := f.pricing[partID]
	if !ok {
		return inventree.PartPricing{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePricingClient) UpdatePartPricing(_ context.Context, partID int, fields inventree.PatchFields) (inventree.PartPricing, error) {
	if f.updateErr != nil {
		return inventree.PartPricing{}, f.updateErr
	}
	record := f.pricing[partID]
	if value, ok := fields["override_min"]; ok {
		if value.Value() == nil {
			record.OverrideMin = nil
		} else {
			decimal := inventree.DecimalString(value.Value().(string))
			record.OverrideMin = &decimal
		}
	}
	if value, ok := fields["override_min_currency"]; ok {
		record.OverrideMinCurrency = value.Value().(string)
	}
	if value, ok := fields["override_max"]; ok {
		if value.Value() == nil {
			record.OverrideMax = nil
		} else {
			decimal := inventree.DecimalString(value.Value().(string))
			record.OverrideMax = &decimal
		}
	}
	if value, ok := fields["override_max_currency"]; ok {
		record.OverrideMaxCurrency = value.Value().(string)
	}
	f.pricing[partID] = record
	return record, nil
}

func (f *fakePricingClient) RefreshPartPricing(_ context.Context, partID int) (inventree.PartPricing, error) {
	if f.refreshErr != nil {
		return inventree.PartPricing{}, f.refreshErr
	}
	record := f.pricing[partID]
	return record, nil
}

func pricingDeps(fake *fakePricingClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestSearchInternalPriceBreaksRequiresPartID(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := searchInternalPriceBreaks(pricingDeps(fake))

	_, output, err := handler(ctx, nil, SearchInternalPriceBreaksInput{})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
}

func TestSearchInternalPriceBreaksReturnsRowsOrderedAndPaginated(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.internal[1] = inventree.PartInternalPriceBreak{PK: 1, Part: 5, Quantity: 1, Price: "10.00", PriceCurrency: "USD"}
	fake.internal[2] = inventree.PartInternalPriceBreak{PK: 2, Part: 5, Quantity: 5, Price: "8.00", PriceCurrency: "USD"}
	fake.internal[3] = inventree.PartInternalPriceBreak{PK: 3, Part: 99, Quantity: 1, Price: "1.00", PriceCurrency: "USD"}
	handler := searchInternalPriceBreaks(pricingDeps(fake))

	_, output, err := handler(ctx, nil, SearchInternalPriceBreaksInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.Len(t, output.Results, 2)
	a.Equal(1, output.Results[0].PK)
	a.Equal(2, output.Results[1].PK)

	_, paged, err := handler(ctx, nil, SearchInternalPriceBreaksInput{PartID: 5, Limit: 1, Offset: 1})
	a.NoError(err)
	a.Equal(StatusOK, paged.Status)
	require.Len(t, paged.Results, 1)
	a.Equal(2, paged.Results[0].PK)
}

func TestSearchInternalPriceBreaksFailsClosedOnBudgetExceeded(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.internal[1] = inventree.PartInternalPriceBreak{PK: 1, Part: 5, Quantity: 1, Price: "10.00", PriceCurrency: "USD"}
	fake.forceHasMore = true
	handler := searchInternalPriceBreaks(pricingDeps(fake))

	_, _, err := handler(ctx, nil, SearchInternalPriceBreaksInput{PartID: 5})
	require.Error(t, err)
}

func TestSearchSalePriceBreaksReturnsRows(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.sale[1] = inventree.PartSalePriceBreak{PK: 1, Part: 5, Quantity: 1, Price: "20.00", PriceCurrency: "USD"}
	handler := searchSalePriceBreaks(pricingDeps(fake))

	_, output, err := handler(ctx, nil, SearchSalePriceBreaksInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.Len(t, output.Results, 1)
	a.Equal(1, output.Results[0].PK)
	a.InDelta(1, output.Results[0].Quantity, 0.0001)
}

func TestSearchSupplierPriceBreaksReturnsRows(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.supplier[1] = inventree.SupplierPriceBreak{PK: 1, SupplierPart: 7, Quantity: 1, Price: "3.00", PriceCurrency: "USD", Supplier: 4}
	handler := searchSupplierPriceBreaks(pricingDeps(fake))

	_, output, err := handler(ctx, nil, SearchSupplierPriceBreaksInput{SupplierPartID: 7})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.Len(t, output.Results, 1)
	a.Equal(1, output.Results[0].PK)
	a.Equal(4, output.Results[0].SupplierID)
}

func TestCreateInternalPriceBreakDetectsDuplicateQuantity(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.internal[1] = inventree.PartInternalPriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "8.00", PriceCurrency: "USD"}
	handler := createInternalPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateInternalPriceBreakInput{PartID: 5, Quantity: 10, Price: "9.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	require.Len(t, output.Candidates, 1)
	a.Equal(1, output.Candidates[0].PK)
}

func TestCreateInternalPriceBreakSucceeds(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := createInternalPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateInternalPriceBreakInput{PartID: 5, Quantity: 10, Price: "9.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record)
	a.InDelta(10, output.Record.Quantity, 0.0001)
	a.Equal("9.00", output.Record.Price)
}

func TestCreateInternalPriceBreakRejectsInvalidFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := createInternalPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateInternalPriceBreakInput{PartID: 5, Quantity: 0, Price: "9.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusValidationFailed, output.Status)
}

func TestCreateInternalPriceBreakMapsUpstreamValidationFailure(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.createErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{"price": {"Ensure this value is greater than or equal to 0."}}}
	handler := createInternalPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateInternalPriceBreakInput{PartID: 5, Quantity: 10, Price: "-1.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusValidationFailed, output.Status)
	require.NotNil(t, output.Validation)
}

func TestUpdateInternalPriceBreakPatchesPrice(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.internal[1] = inventree.PartInternalPriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "8.00", PriceCurrency: "USD"}
	handler := updateInternalPriceBreak(pricingDeps(fake))

	newPrice := "12.00"
	_, output, err := handler(ctx, nil, UpdateInternalPriceBreakInput{ID: 1, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record)
	a.Equal("12.00", output.Record.Price)
}

func TestUpdateInternalPriceBreakNotFound(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := updateInternalPriceBreak(pricingDeps(fake))

	newPrice := "12.00"
	_, output, err := handler(ctx, nil, UpdateInternalPriceBreakInput{ID: 999, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestDeleteInternalPriceBreakRequiresConfirm(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.internal[1] = inventree.PartInternalPriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "8.00", PriceCurrency: "USD"}
	handler := deleteInternalPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, DeleteInternalPriceBreakInput{ID: 1})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	_, stillThere := fake.internal[1]
	a.True(stillThere)

	_, output, err = handler(ctx, nil, DeleteInternalPriceBreakInput{ID: 1, Confirm: true})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	_, stillThere = fake.internal[1]
	a.False(stillThere)
}

func TestCreateSalePriceBreakRequiresSalablePart(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.parts[5] = inventree.Part{PK: 5, Salable: false}
	handler := createSalePriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateSalePriceBreakInput{PartID: 5, Quantity: 10, Price: "9.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusValidationFailed, output.Status)
	a.Empty(fake.sale)
}

func TestCreateSalePriceBreakSucceedsOnceSalable(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.parts[5] = inventree.Part{PK: 5, Salable: true}
	handler := createSalePriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateSalePriceBreakInput{PartID: 5, Quantity: 10, Price: "9.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record)
}

func TestCreateSalePriceBreakDetectsDuplicateQuantity(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.parts[5] = inventree.Part{PK: 5, Salable: true}
	fake.sale[1] = inventree.PartSalePriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "18.00", PriceCurrency: "USD"}
	handler := createSalePriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateSalePriceBreakInput{PartID: 5, Quantity: 10, Price: "19.00", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	require.Len(t, output.Candidates, 1)
	a.Equal(1, output.Candidates[0].PK)
}

func TestUpdateSalePriceBreakPatchesPrice(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.sale[1] = inventree.PartSalePriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "18.00", PriceCurrency: "USD"}
	handler := updateSalePriceBreak(pricingDeps(fake))

	newPrice := "19.00"
	_, output, err := handler(ctx, nil, UpdateSalePriceBreakInput{ID: 1, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record)
	a.Equal("19.00", output.Record.Price)
}

func TestUpdateSalePriceBreakNotFound(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := updateSalePriceBreak(pricingDeps(fake))

	newPrice := "19.00"
	_, output, err := handler(ctx, nil, UpdateSalePriceBreakInput{ID: 999, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestDeleteSalePriceBreakRequiresConfirm(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.sale[1] = inventree.PartSalePriceBreak{PK: 1, Part: 5, Quantity: 10, Price: "18.00", PriceCurrency: "USD"}
	handler := deleteSalePriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, DeleteSalePriceBreakInput{ID: 1})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	_, stillThere := fake.sale[1]
	a.True(stillThere)

	_, output, err = handler(ctx, nil, DeleteSalePriceBreakInput{ID: 1, Confirm: true})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	_, stillThere = fake.sale[1]
	a.False(stillThere)
}

func TestCreateSupplierPriceBreakDetectsDuplicateQuantity(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.supplier[1] = inventree.SupplierPriceBreak{PK: 1, SupplierPart: 7, Quantity: 5, Price: "3.00", PriceCurrency: "USD"}
	handler := createSupplierPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, CreateSupplierPriceBreakInput{SupplierPartID: 7, Quantity: 5, Price: "3.50", PriceCurrency: "USD"})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
}

func TestUpdateSupplierPriceBreakPatchesPrice(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.supplier[1] = inventree.SupplierPriceBreak{PK: 1, SupplierPart: 7, Quantity: 5, Price: "3.00", PriceCurrency: "USD"}
	handler := updateSupplierPriceBreak(pricingDeps(fake))

	newPrice := "3.25"
	_, output, err := handler(ctx, nil, UpdateSupplierPriceBreakInput{ID: 1, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record)
	a.Equal("3.25", output.Record.Price)
}

func TestUpdateSupplierPriceBreakNotFound(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := updateSupplierPriceBreak(pricingDeps(fake))

	newPrice := "3.25"
	_, output, err := handler(ctx, nil, UpdateSupplierPriceBreakInput{ID: 999, Price: &newPrice})
	a.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestDeleteSupplierPriceBreakRequiresConfirm(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.supplier[1] = inventree.SupplierPriceBreak{PK: 1, SupplierPart: 7, Quantity: 5, Price: "3.00", PriceCurrency: "USD"}
	handler := deleteSupplierPriceBreak(pricingDeps(fake))

	_, output, err := handler(ctx, nil, DeleteSupplierPriceBreakInput{ID: 1})
	a.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	_, stillThere := fake.supplier[1]
	a.True(stillThere)

	_, output, err = handler(ctx, nil, DeleteSupplierPriceBreakInput{ID: 1, Confirm: true})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	_, stillThere = fake.supplier[1]
	a.False(stillThere)
}

func TestGetPartPricingReturnsRecord(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.pricing[5] = inventree.PartPricing{Currency: "USD", ScheduledForUpdate: false}
	handler := getPartPricing(pricingDeps(fake))

	_, output, err := handler(ctx, nil, GetPartPricingInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("USD", output.Record.Currency)
}

func TestUpdatePartPricingOverrideSetsAndClearsFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.pricing[5] = inventree.PartPricing{Currency: "USD"}
	handler := updatePartPricingOverride(pricingDeps(fake))

	minValue := "3.00"
	minCurrency := "USD"
	_, output, err := handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, OverrideMin: &minValue, OverrideMinCurrency: &minCurrency})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	require.NotNil(t, output.Record.OverrideMin)
	a.Equal("3.00", string(*output.Record.OverrideMin))

	_, output, err = handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, ClearOverrideMin: true})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Nil(output.Record.OverrideMin)
}

func TestUpdatePartPricingOverrideRejectsConflictingMinFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := updatePartPricingOverride(pricingDeps(fake))

	minValue := "3.00"
	_, _, err := handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, OverrideMin: &minValue, ClearOverrideMin: true})
	require.Error(t, err)
}

func TestUpdatePartPricingOverrideRejectsConflictingMaxFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := updatePartPricingOverride(pricingDeps(fake))

	maxValue := "300.00"
	_, _, err := handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, OverrideMax: &maxValue, ClearOverrideMax: true})
	require.Error(t, err)
}

func TestUpdatePartPricingOverrideRejectsCurrencyWithoutValue(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.pricing[5] = inventree.PartPricing{Currency: "USD"}
	handler := updatePartPricingOverride(pricingDeps(fake))

	minCurrency := "USD"
	_, _, err := handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, OverrideMinCurrency: &minCurrency})
	require.Error(t, err)

	maxCurrency := "USD"
	_, _, err = handler(ctx, nil, UpdatePartPricingOverrideInput{PartID: 5, OverrideMaxCurrency: &maxCurrency})
	require.Error(t, err)
}

func TestRefreshPartPricingRejectsNonPositivePartID(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	handler := refreshPartPricing(pricingDeps(fake))

	_, output, err := handler(ctx, nil, RefreshPartPricingInput{PartID: 0})
	a.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestRefreshPartPricingReturnsNotFoundWhenPartMissing(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.refreshErr = &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	handler := refreshPartPricing(pricingDeps(fake))

	_, output, err := handler(ctx, nil, RefreshPartPricingInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusNotFound, output.Status)
}

func TestRefreshPartPricingReturnsSettledOnce(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.pricing[5] = inventree.PartPricing{Currency: "USD", ScheduledForUpdate: false}
	handler := refreshPartPricing(pricingDeps(fake))

	_, output, err := handler(ctx, nil, RefreshPartPricingInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Settled)
}

// Deliberately not t.Parallel(): this test mutates the package-level
// partPricingRefreshTimeout var (see its doc comment in pricing_tools.go),
// which every other parallel test in this file that calls refreshPartPricing
// also reads. Go guarantees every non-parallel top-level test in a package
// completes -- including t.Cleanup -- before any t.Parallel() test resumes,
// so keeping this one non-parallel is what makes the shared-var mutation
// safe rather than a data race.
func TestRefreshPartPricingReturnsPendingWhenNeverSettled(t *testing.T) {
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePricingClient()
	fake.pricing[5] = inventree.PartPricing{Currency: "USD", ScheduledForUpdate: true}
	handler := refreshPartPricing(pricingDeps(fake))

	original := partPricingRefreshTimeout
	partPricingRefreshTimeout = 50 * time.Millisecond
	t.Cleanup(func() { partPricingRefreshTimeout = original })

	_, output, err := handler(ctx, nil, RefreshPartPricingInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusPending, output.Status)
	a.False(output.Settled)
}

func TestQuantityMatchesToleratesFloatNoise(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.True(quantityMatches(10.0, 10.0))
	a.False(quantityMatches(10.0, 10.5))
}
