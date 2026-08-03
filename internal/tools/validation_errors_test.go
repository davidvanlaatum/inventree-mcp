package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeToolErrorIncludesOnlyCanonicalAllowlistedValidationDetails(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	err := safeToolError(&inventree.APIError{
		StatusCode: http.StatusBadRequest,
		Kind:       inventree.ErrorKindValidation,
		Detail:     "secret raw response body",
		FieldErrors: map[string][]string{
			"MPN":       {"This field may not be blank."},
			"website":   {"Invalid URL https://user:password@example.test/?token=secret"},
			"tax_id":    {"private tax data"},
			"api_token": {"token-value"},
		},
	})

	a.Equal("InvenTree validation failed with status 400: MPN: This field may not be blank.; website: Enter a valid URL.", err.Error())
	a.NotContains(err.Error(), "secret")
	a.NotContains(err.Error(), "password")
	a.NotContains(err.Error(), "tax")
	a.NotContains(err.Error(), "token")
}

func TestSafeToolErrorSuppressesNonValidationResponseDetails(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	err := safeToolError(&inventree.APIError{StatusCode: http.StatusInternalServerError, Kind: inventree.ErrorKindServer, Detail: "private upstream body"})
	a.Equal("InvenTree request failed with status 500", err.Error())
}

func TestSafeToolErrorSuppressesTransportURL(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	err := safeToolError(fmt.Errorf("search failed: %w", &url.Error{
		Op:  http.MethodGet,
		URL: "https://operator:password@example.test/api/part/?token=secret&search=private",
		Err: errors.New("connection refused"),
	}))

	a.Equal("upstream request failed", err.Error())
	a.NotContains(err.Error(), "secret")
	a.NotContains(err.Error(), "private")
	a.NotContains(err.Error(), "password")
}

func TestSafeToolErrorPreservesWrappedContextSentinels(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		err := safeToolError(&url.Error{
			Op:  http.MethodGet,
			URL: "https://operator:password@example.test/api/part/?token=secret",
			Err: sentinel,
		})
		r.ErrorIs(err, sentinel)
		r.Equal(sentinel.Error(), err.Error())
	}
}

func TestSafeValidationFailureCapsAndCoalescesFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	duplicateFields := map[string][]string{
		"MPN":   {"This field may not be blank."},
		" mpn ": {"This field is required."},
		"mpn":   {"private submitted value"},
	}
	coalesced, ok := safeValidationFailure(&inventree.APIError{
		StatusCode:  http.StatusBadRequest,
		Kind:        inventree.ErrorKindValidation,
		FieldErrors: duplicateFields,
	})
	a.True(ok)
	a.Equal([]ValidationFieldError{{Field: "MPN", Messages: []string{"This field is required.", "This field may not be blank.", "Rejected by InvenTree."}}}, coalesced.Fields)

	fieldErrors := map[string][]string{}
	for _, field := range []string{
		"active", "assembly", "attachment", "batch", "category", "comment", "component", "creation_date",
		"currency", "data", "default_location", "description", "destination", "filename", "image", "link",
		"manufacturer", "name", "notes", "part",
	} {
		fieldErrors[field] = []string{"invalid private value"}
	}

	failure, ok := safeValidationFailure(&inventree.APIError{
		StatusCode:  http.StatusBadRequest,
		Kind:        inventree.ErrorKindValidation,
		FieldErrors: fieldErrors,
	})

	a.True(ok)
	a.Len(failure.Fields, maxValidationFields)
}
