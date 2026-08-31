package tools

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func attrsByKey(attrs []slog.Attr) map[string]any {
	byKey := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		byKey[attr.Key] = attr.Value.Any()
	}
	return byKey
}

func TestSafeInvocationFieldsReturnsNilForUnmappedTool(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(safeInvocationFields("unmapped_tool", []byte(`{"id":1}`)))
}

func TestSafeInvocationFieldsReturnsNilForEmptyArguments(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(safeInvocationFields(GetPartToolName, nil))
}

func TestIDInputFieldsExtractsPositiveID(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	attrs := idInputFields([]byte(`{"id":7}`))
	a.Len(attrs, 1)
	a.Equal("id", attrs[0].Key)
	a.Equal(int64(7), attrs[0].Value.Int64())
}

func TestIDInputFieldsOmitsNonPositiveID(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(idInputFields([]byte(`{"id":0}`)))
	a.Nil(idInputFields([]byte(`{"id":-1}`)))
	a.Nil(idInputFields([]byte(`not json`)))
}

func TestSearchInputFieldsOmitsZeroValuesAndNeverIncludesSearchText(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Empty(searchInputFields([]byte(`{"search":"secret"}`)))
	byKey := attrsByKey(searchInputFields([]byte(`{"search":"secret","limit":50,"offset":20}`)))
	a.EqualValues(50, byKey["limit"])
	a.EqualValues(20, byKey["offset"])
	_, hasSearch := byKey["search"]
	a.False(hasSearch)
}

func TestObjectLookupInputFieldsExtractsBoundedFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	byKey := attrsByKey(objectLookupInputFields([]byte(`{"model_type":"part","model_id":9,"search":"secret","limit":30,"offset":15}`)))
	a.Equal("part", byKey["model_type"])
	a.EqualValues(9, byKey["model_id"])
	a.EqualValues(30, byKey["limit"])
	a.EqualValues(15, byKey["offset"])
	_, hasSearch := byKey["search"]
	a.False(hasSearch)
}

func TestObjectLookupInputFieldsOmitsEmptyOrZeroValues(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Empty(objectLookupInputFields([]byte(`{}`)))
	a.Nil(objectLookupInputFields([]byte(`not json`)))
}

func TestBoundedShortStringTruncatesOversizedValues(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	short := "part"
	a.Equal(short, boundedShortString(short))
	long := strings.Repeat("x", maxLoggedShortStringLength+10)
	truncated := boundedShortString(long)
	a.Len(truncated, maxLoggedShortStringLength)
}

func TestBulkItemsInputFieldsCountsItemsWithoutContent(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	byKey := attrsByKey(bulkItemsInputFields([]byte(`{"items":[{"id":1,"secret":"do not log"},{"id":2}],"dry_run":true,"confirm":false}`)))
	a.EqualValues(2, byKey["item_count"])
	a.Equal(true, byKey["dry_run"])
	a.Equal(false, byKey["confirm"])
	_, hasItems := byKey["items"]
	a.False(hasItems)
}

func TestBulkItemsInputFieldsReturnsNilForMalformedArguments(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(bulkItemsInputFields([]byte(`not json`)))
}

func TestBulkPropagatePartParametersFieldsCountsPartIDs(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	byKey := attrsByKey(bulkPropagatePartParametersFields([]byte(`{"part_ids":[1,2,3],"dry_run":true,"confirm":false}`)))
	a.EqualValues(3, byKey["item_count"])
	a.Equal(true, byKey["dry_run"])
}

func TestBulkPropagatePartParametersFieldsReturnsNilForMalformedArguments(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(bulkPropagatePartParametersFields([]byte(`not json`)))
}

func TestRenderComponentImageFieldsExtractsFamilyOnly(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	attrs := renderComponentImageFields([]byte(`{"family":"resistor","axial":{"length_mm":10}}`))
	a.Len(attrs, 1)
	a.Equal("family", attrs[0].Key)
	a.Equal("resistor", attrs[0].Value.String())
}

func TestRenderComponentImageFieldsOmitsEmptyFamily(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Nil(renderComponentImageFields([]byte(`{}`)))
	a.Nil(renderComponentImageFields([]byte(`not json`)))
}
