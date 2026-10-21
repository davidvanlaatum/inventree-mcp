package inventree

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Note excludes expanded user details and template/image administration.
type Note struct {
	PK          int     `json:"pk"`
	ModelType   string  `json:"model_type"`
	ModelID     int     `json:"model_id"`
	Template    bool    `json:"template"`
	Primary     bool    `json:"primary"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	Updated     *string `json:"updated"`
	UpdatedBy   *int    `json:"updated_by"`
}

type NoteQuery struct {
	ModelType string
	ModelID   int
	Search    string
	Limit     int
	Offset    int
}

type NoteCreate struct {
	ModelType   string `json:"model_type"`
	ModelID     int    `json:"model_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Primary     *bool  `json:"primary,omitempty"`
}

var ErrNoteOutOfScope = errors.New("note is not an ordinary note on a supported, readable object")

// ValidNoteModelType uses the notes endpoint's qualified model vocabulary.
func ValidNoteModelType(value string) bool {
	switch value {
	case "part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder":
		return true
	}
	return false
}

func (c *Client) readableNoteParent(ctx context.Context, modelType string, id int) error {
	if !ValidNoteModelType(modelType) || id <= 0 {
		return ErrNoteOutOfScope
	}
	var err error
	switch modelType {
	case "part.part":
		_, err = c.GetPart(ctx, id)
	case "company.company":
		_, err = c.GetCompany(ctx, id)
	case "company.supplierpart":
		_, err = c.GetSupplierPart(ctx, id)
	case "company.manufacturerpart":
		_, err = c.GetManufacturerPartDetail(ctx, id)
	case "stock.stockitem":
		_, err = c.GetStockItem(ctx, id)
	case "order.purchaseorder":
		_, err = c.GetPurchaseOrder(ctx, id)
	}
	return err
}

func (c *Client) SearchNotesPage(ctx context.Context, query NoteQuery) (Page[Note], error) {
	if !ValidNoteModelType(query.ModelType) || query.ModelID < 0 || query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || query.Offset > 5000 {
		return Page[Note]{}, errors.New("notes search requires a supported model_type, limit 1-100, and offset 0-5000")
	}
	if query.ModelID > 0 {
		if err := c.readableNoteParent(ctx, query.ModelType, query.ModelID); err != nil {
			return Page[Note]{}, err
		}
	}
	values := url.Values{"model_type": {query.ModelType}, "template": {"false"}, "limit": {strconv.Itoa(query.Limit)}, "offset": {strconv.Itoa(query.Offset)}, "ordering": {"-updated"}, "updated_by_detail": {"false"}}
	if query.ModelID > 0 {
		values.Set("model_id", strconv.Itoa(query.ModelID))
	}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	page, err := listPage[Note](ctx, c, "/api/note/", values)
	if err != nil {
		return Page[Note]{}, err
	}
	if len(page.Results) > query.Limit {
		return Page[Note]{}, errors.New("notes response exceeded the requested page size")
	}
	for _, note := range page.Results {
		if note.Template || note.ModelType != query.ModelType || note.ModelID <= 0 || (query.ModelID > 0 && note.ModelID != query.ModelID) {
			return Page[Note]{}, ErrNoteOutOfScope
		}
		if err := c.readableNoteParent(ctx, note.ModelType, note.ModelID); err != nil {
			return Page[Note]{}, err
		}
	}
	return page, nil
}

func (c *Client) GetNote(ctx context.Context, id int) (Note, error) {
	if id <= 0 {
		return Note{}, ErrNoteOutOfScope
	}
	var out Note
	req, err := c.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/note/%d/", id), url.Values{"updated_by_detail": {"false"}}, nil)
	if err != nil {
		return Note{}, err
	}
	err = c.DoJSON(req, &out)
	if err != nil {
		return Note{}, err
	}
	if out.PK != id || out.Template {
		return Note{}, ErrNoteOutOfScope
	}
	if err = c.readableNoteParent(ctx, out.ModelType, out.ModelID); err != nil {
		return Note{}, ErrNoteOutOfScope
	}
	return out, nil
}

func validateNoteText(title, description, content string) error {
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > 100 || utf8.RuneCountInString(description) > 250 || utf8.RuneCountInString(content) > 50000 {
		return errors.New("note requires a non-empty title up to 100 characters, description up to 250, and content up to 50000")
	}
	return nil
}

func (c *Client) CreateNote(ctx context.Context, input NoteCreate) (Note, error) {
	if err := validateNoteText(input.Title, input.Description, input.Content); err != nil {
		return Note{}, err
	}
	if err := c.readableNoteParent(ctx, input.ModelType, input.ModelID); err != nil {
		return Note{}, err
	}
	var out Note
	err := c.Post(ctx, "/api/note/", input, &out)
	if err != nil {
		return Note{}, err
	}
	if out.PK <= 0 || out.Template || out.ModelType != input.ModelType || out.ModelID != input.ModelID {
		return Note{}, ErrNoteOutOfScope
	}
	return out, nil
}

func (c *Client) UpdateNote(ctx context.Context, id int, fields PatchFields) (Note, error) {
	before, err := c.GetNote(ctx, id)
	if err != nil {
		return Note{}, err
	}
	if len(fields) == 0 {
		return Note{}, errors.New("at least one note field is required")
	}
	for key, value := range fields {
		switch key {
		case "title", "description", "content":
			text, ok := value.Value().(string)
			if !ok {
				return Note{}, errors.New("note text fields require explicit strings")
			}
			switch key {
			case "title":
				before.Title = text
			case "description":
				before.Description = text
			case "content":
				before.Content = text
			}
		case "primary":
			if _, ok := value.Value().(bool); !ok {
				return Note{}, errors.New("primary must be a boolean")
			}
		default:
			return Note{}, errors.New("note field is not editable")
		}
	}
	if err = validateNoteText(before.Title, before.Description, before.Content); err != nil {
		return Note{}, err
	}
	var out Note
	err = c.Patch(ctx, fmt.Sprintf("/api/note/%d/", id), fields, &out)
	if err != nil {
		return Note{}, err
	}
	if out.PK != id || out.Template || out.ModelType != before.ModelType || out.ModelID != before.ModelID {
		return Note{}, ErrNoteOutOfScope
	}
	return out, nil
}

func (c *Client) DeleteNote(ctx context.Context, id int) error {
	if _, err := c.GetNote(ctx, id); err != nil {
		return err
	}
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/note/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}
