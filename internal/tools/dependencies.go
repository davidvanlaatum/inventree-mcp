package tools

import (
	"context"
	"errors"
)

var ErrLookupClientUnavailable = errors.New("InvenTree lookup client unavailable")

type Dependencies struct {
	ClientFromContext func(context.Context) (any, error)
}

func (d Dependencies) Client(ctx context.Context) (any, error) {
	if d.ClientFromContext == nil {
		return nil, ErrLookupClientUnavailable
	}
	client, err := d.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrLookupClientUnavailable
	}
	return client, nil
}
