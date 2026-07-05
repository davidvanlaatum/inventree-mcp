package tools

import (
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolReferenceDocumentsLookupFrameworkSchema(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data, err := os.ReadFile("../../docs/tool-reference.md")
	r.NoError(err)
	docs := string(data)

	for _, required := range []string{
		"## Lookup Tool Framework",
		"`search`",
		"`limit`",
		"`offset`",
		"`model_type`",
		"`model_id`",
		"`status`",
		"`clarification_required`",
		"`question`",
		"`field`",
		"`reason`",
		"`retry`",
		"`hard_error`",
		"`candidates`",
		"`retry_values`",
		"`" + StatusOK + "`",
		"`" + StatusNotFound + "`",
		"`" + StatusClarificationRequired + "`",
		"`" + strconv.Itoa(DefaultLookupLimit) + "`",
		"`" + strconv.Itoa(MaxLookupLimit) + "`",
	} {
		a.Contains(docs, required)
	}

	for _, schemaType := range []reflect.Type{
		reflect.TypeOf(SearchInput{}),
		reflect.TypeOf(IDInput{}),
		reflect.TypeOf(ObjectLookupInput{}),
		reflect.TypeOf(ClarificationResponse{}),
		reflect.TypeOf(ClarificationCandidate{}),
	} {
		for _, field := range reflect.VisibleFields(schemaType) {
			jsonName := jsonFieldName(field.Tag.Get("json"))
			if jsonName == "" || jsonName == "-" {
				continue
			}
			a.Contains(docs, "`"+jsonName+"`")
		}
	}
}

func jsonFieldName(tag string) string {
	for i, char := range tag {
		if char == ',' {
			return tag[:i]
		}
	}
	return tag
}
