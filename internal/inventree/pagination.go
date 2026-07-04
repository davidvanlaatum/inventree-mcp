package inventree

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type Page[T any] struct {
	Count    int     `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  []T     `json:"results"`
}

func ListAll[T any](ctx context.Context, client *Client, path string, query url.Values) ([]T, error) {
	if client == nil {
		return nil, errors.New("InvenTree client is required")
	}

	var all []T
	nextPath := path
	nextQuery := cloneValues(query)
	for {
		var page Page[T]
		req, err := client.NewRequest(ctx, http.MethodGet, nextPath, nextQuery, nil)
		if err != nil {
			return nil, err
		}
		if err := client.DoJSON(req, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		if page.Next == nil || *page.Next == "" {
			return all, nil
		}
		nextURL, err := url.Parse(*page.Next)
		if err != nil {
			return nil, err
		}
		nextPath = nextURL.Path
		nextQuery = nextURL.Query()
	}
}

func cloneValues(values url.Values) url.Values {
	if values == nil {
		return nil
	}
	clone := make(url.Values, len(values))
	for key, items := range values {
		clone[key] = append([]string(nil), items...)
	}
	return clone
}
