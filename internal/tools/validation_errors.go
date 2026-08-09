package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
)

const StatusValidationFailed = "validation_failed"

const maxValidationFields = 16

type ValidationFailure struct {
	StatusCode int                    `json:"status_code"`
	Fields     []ValidationFieldError `json:"fields,omitempty"`
}

type ValidationFieldError struct {
	Field    string   `json:"field"`
	Messages []string `json:"messages"`
}

var validationFieldAllowlist = map[string]string{
	"active": "active", "assembly": "assembly", "attachment": "attachment", "batch": "batch",
	"category": "category", "comment": "comment", "component": "component", "creation_date": "creation_date",
	"currency": "currency", "data": "data", "default_location": "default_location", "description": "description",
	"destination": "destination", "filename": "filename", "image": "image", "ipn": "IPN",
	"is_manufacturer": "is_manufacturer", "is_supplier": "is_supplier", "line": "line", "link": "link",
	"manufacturer": "manufacturer", "manufacturer_part": "manufacturer_part", "model_id": "model_id",
	"model_type": "model_type", "mpn": "MPN", "name": "name", "non_field_errors": "non_field_errors",
	"note": "note", "notes": "notes", "order": "order", "pack_quantity": "pack_quantity",
	"packaging": "packaging", "part": "part", "primary": "primary", "purchase_price": "purchase_price",
	"purchase_price_currency": "purchase_price_currency", "purchaseable": "purchaseable", "quantity": "quantity",
	"reference": "reference", "serial": "serial", "sku": "SKU", "start_date": "start_date",
	"status": "status", "supplier": "supplier", "supplier_reference": "supplier_reference", "tags": "tags",
	"price": "price", "price_currency": "price_currency",
	"target_date": "target_date", "template": "template", "trackable": "trackable", "units": "units",
	"virtual": "virtual", "website": "website",
}

func safeValidationFailure(err error) (*ValidationFailure, bool) {
	var apiErr *inventree.APIError
	if !errors.As(err, &apiErr) || (apiErr.Kind != inventree.ErrorKindValidation && apiErr.StatusCode != http.StatusBadRequest && apiErr.StatusCode != http.StatusUnprocessableEntity) {
		return nil, false
	}

	rawKeys := make([]string, 0, len(apiErr.FieldErrors))
	for key := range apiErr.FieldErrors {
		rawKeys = append(rawKeys, key)
	}
	sort.Slice(rawKeys, func(i, j int) bool {
		left, right := strings.ToLower(rawKeys[i]), strings.ToLower(rawKeys[j])
		if left == right {
			return rawKeys[i] < rawKeys[j]
		}
		return left < right
	})

	byCanonicalField := map[string][]string{}
	for _, key := range rawKeys {
		canonical, ok := validationFieldAllowlist[strings.ToLower(strings.TrimSpace(key))]
		if !ok {
			continue
		}
		messages := safeValidationMessages(apiErr.FieldErrors[key])
		if len(messages) == 0 {
			messages = []string{"Rejected by InvenTree."}
		}
		byCanonicalField[canonical] = mergeValidationMessages(byCanonicalField[canonical], messages)
	}

	keys := make([]string, 0, len(byCanonicalField))
	for key := range byCanonicalField {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
	if len(keys) > maxValidationFields {
		keys = keys[:maxValidationFields]
	}

	failure := &ValidationFailure{StatusCode: apiErr.StatusCode}
	for _, key := range keys {
		failure.Fields = append(failure.Fields, ValidationFieldError{Field: key, Messages: byCanonicalField[key]})
	}
	return failure, true
}

func mergeValidationMessages(existing, additional []string) []string {
	const maxMessages = 4
	seen := make(map[string]struct{}, len(existing)+len(additional))
	merged := make([]string, 0, min(len(existing)+len(additional), maxMessages))
	for _, messages := range [][]string{existing, additional} {
		for _, message := range messages {
			if _, ok := seen[message]; ok {
				continue
			}
			seen[message] = struct{}{}
			merged = append(merged, message)
			if len(merged) == maxMessages {
				return merged
			}
		}
	}
	return merged
}

func safeValidationMessages(raw []string) []string {
	const maxMessages = 4
	messages := make([]string, 0, min(len(raw), maxMessages))
	seen := map[string]struct{}{}
	for _, message := range raw {
		lower := strings.ToLower(message)
		safe := "Rejected by InvenTree."
		switch {
		case strings.Contains(lower, "blank"):
			safe = "This field may not be blank."
		case strings.Contains(lower, "required"):
			safe = "This field is required."
		case strings.Contains(lower, "null"):
			safe = "This field may not be null."
		case strings.Contains(lower, "valid url") || strings.Contains(lower, "valid uri"):
			safe = "Enter a valid URL."
		case strings.Contains(lower, "valid date"):
			safe = "Enter a valid date."
		case strings.Contains(lower, "valid choice"):
			safe = "Not a valid choice."
		case strings.Contains(lower, "greater than") || strings.Contains(lower, "less than") || strings.Contains(lower, "maximum") || strings.Contains(lower, "minimum"):
			safe = "Value is outside the allowed range."
		}
		if _, ok := seen[safe]; ok {
			continue
		}
		seen[safe] = struct{}{}
		messages = append(messages, safe)
		if len(messages) == maxMessages {
			break
		}
	}
	return messages
}

func safeToolError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return errors.New("upstream request failed")
	}
	var apiErr *inventree.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if validation, ok := safeValidationFailure(err); ok {
		parts := make([]string, 0, len(validation.Fields))
		for _, field := range validation.Fields {
			parts = append(parts, field.Field+": "+strings.Join(field.Messages, " "))
		}
		if len(parts) > 0 {
			return fmt.Errorf("InvenTree validation failed with status %d: %s", validation.StatusCode, strings.Join(parts, "; "))
		}
		return fmt.Errorf("InvenTree validation failed with status %d", validation.StatusCode)
	}
	return fmt.Errorf("InvenTree request failed with status %d", apiErr.StatusCode)
}

// definiteMutationRejection classifies a status code as a definite upstream
// rejection -- one where the mutation is known not to have applied, so no
// read-back verification is needed -- versus an ambiguous failure where the
// mutation may have applied despite the error response. 408 (timeout), 425
// (too early), and 429 (rate limited) are excluded from "definite" because a
// retried or delayed request can still have succeeded server-side. Shared
// by every guarded destructive/operational tool that must tell a definite
// rejection apart from a lost or ambiguous response (stock adjustment,
// depletion, transfer, and part deletion); it is not stock-specific despite
// originating there.
func definiteMutationRejection(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != 408 && statusCode != 425 && statusCode != 429
}
