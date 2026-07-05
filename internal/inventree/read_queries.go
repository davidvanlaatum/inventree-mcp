package inventree

import (
	"net/url"
	"strconv"
)

type SearchQuery struct {
	Search string
	Limit  int
	Offset int
}

type PartParameterQuery struct {
	PartID int
	Limit  int
	Offset int
}

type StockItemQuery struct {
	Search     string
	PartID     int
	LocationID int
	Limit      int
	Offset     int
}

type AttachmentQuery struct {
	ModelType string
	ModelID   int
	Search    string
	Limit     int
	Offset    int
}

func (q SearchQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q PartParameterQuery) values() url.Values {
	values := url.Values{}
	if q.PartID != 0 {
		values.Set("model_id", strconv.Itoa(q.PartID))
	}
	values.Set("model_type", parameterModelTypePart)
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q StockItemQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	if q.PartID != 0 {
		values.Set("part", strconv.Itoa(q.PartID))
	}
	if q.LocationID != 0 {
		values.Set("location", strconv.Itoa(q.LocationID))
	}
	return values
}

func (q AttachmentQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	values.Set("model_type", q.ModelType)
	if q.ModelID != 0 {
		values.Set("model_id", strconv.Itoa(q.ModelID))
	}
	return values
}

func setPagination(values url.Values, limit int, offset int) {
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
}
