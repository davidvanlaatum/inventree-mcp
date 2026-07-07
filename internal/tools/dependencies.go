package tools

import (
	"context"
	"errors"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/spf13/afero"
)

var ErrLookupClientUnavailable = errors.New("InvenTree lookup client unavailable")

type Dependencies struct {
	ClientFromContext func(context.Context) (any, error)
	EnableWriteTools  bool
	UploadMode        upload.Mode
	UploadFS          afero.Fs
	UploadAllowRoots  []string
	UploadMaxBytes    int64
	UploadTimeout     time.Duration
	URLFetcher        upload.URLFetcher
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
