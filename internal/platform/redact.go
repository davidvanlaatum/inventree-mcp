package platform

import (
	"log/slog"
	"strings"
)

const redactedValue = "[REDACTED]"

var sensitiveLogKeys = map[string]struct{}{
	"access_token":    {},
	"api_token":       {},
	"authorization":   {},
	"code":            {},
	"cookie":          {},
	"inventree_token": {},
	"password":        {},
	"refresh_token":   {},
	"secret":          {},
	"set-cookie":      {},
	"state":           {},
	"token":           {},
}

func RedactSecret(value string) string {
	if value == "" {
		return ""
	}
	return redactedValue
}

func RedactedAttr(key string) slog.Attr {
	return slog.String(key, redactedValue)
}

func RedactLogAttr(_ []string, attr slog.Attr) slog.Attr {
	if _, ok := sensitiveLogKeys[strings.ToLower(attr.Key)]; ok {
		return RedactedAttr(attr.Key)
	}
	return attr
}
