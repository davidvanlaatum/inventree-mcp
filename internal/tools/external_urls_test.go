package tools

import (
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalURLPolicyPreservesFunctionalComponents(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	const complete = "https://supplier.example.test:8443/catalog/item?sku=ABC%20123&view=full#datasheet"
	validated, err := validateExternalURL("  " + complete + "  ")
	r.NoError(err)
	a.Equal(complete, validated)
	a.Equal(complete, projectExternalURL(dvgoutils.Ptr(complete)))
}

func TestExternalURLPolicyRejectsUnsafeOrMalformedValuesWithoutEchoingThem(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://user:secret@example.test/private?token=secret#fragment",
		"ftp://example.test/file",
		"/relative/path?token=secret",
		"https:///missing-host?token=secret",
		"https://example.test/%zz?token=secret",
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			validated, err := validateExternalURL(raw)
			require.Error(t, err)
			assert.Empty(t, validated)
			assert.NotContains(t, err.Error(), raw)
			assert.NotContains(t, err.Error(), "secret")
			assert.Empty(t, projectExternalURL(&raw))
		})
	}
}
