package tools

import (
	"errors"
	"net/url"
	"strings"
)

// validateExternalURL validates an operator-supplied or upstream external
// link without including the rejected value in its error. Query parameters
// and fragments are functional URL components and are preserved verbatim.
func validateExternalURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("external URL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("external URL scheme must be http or https")
	}
	if parsed.User != nil {
		return "", errors.New("external URL must not include userinfo or credentials")
	}
	return parsed.String(), nil
}

func validateExternalURLPointer(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	validated, err := validateExternalURL(*raw)
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

// projectExternalURL returns a complete valid HTTP(S) URL for an authorized
// successful projection. Invalid values, including credentials, are omitted
// instead of being repaired into a different URL.
func projectExternalURL(raw *string) string {
	if raw == nil {
		return ""
	}
	validated, err := validateExternalURL(*raw)
	if err != nil {
		return ""
	}
	return validated
}
