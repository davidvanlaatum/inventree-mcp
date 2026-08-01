package inventree

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurchaseOrderLineItemAcceptsStringAndNumericPurchasePrices(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, input := range []string{`{"purchase_price":"1.25"}`, `{"purchase_price":1.25}`} {
		var line PurchaseOrderLineItem
		r.NoError(json.Unmarshal([]byte(input), &line))
		r.NotNil(line.PurchasePrice)
		a.Equal(DecimalString("1.25"), *line.PurchasePrice)
	}
}
