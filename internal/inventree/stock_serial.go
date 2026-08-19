package inventree

import (
	"context"
	"fmt"
)

// PartSerialNumbers is the client projection of GET /api/part/{id}/serial-numbers/:
// the latest assigned serial number for the part (nil when the part has no
// serialized stock yet) and the next value InvenTree would compute for it.
type PartSerialNumbers struct {
	Latest *string `json:"latest"`
	Next   string  `json:"next"`
}

// GetPartSerialNumbers fetches latest/next serial-number information for one
// part from /api/part/{id}/serial-numbers/. Pinned InvenTree 1.5.1 does not
// reject this endpoint for a non-trackable part; it returns an ordinary 200
// with Latest:nil. Callers that want a clearer signal for a non-trackable
// part should verify Part.trackable themselves before calling this method.
func (c *Client) GetPartSerialNumbers(ctx context.Context, partID int) (PartSerialNumbers, error) {
	var out PartSerialNumbers
	err := c.get(ctx, fmt.Sprintf("/api/part/%d/serial-numbers/", partID), &out)
	return out, err
}
