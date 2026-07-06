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
		reflect.TypeOf(PartParametersInput{}),
		reflect.TypeOf(StockItemsInput{}),
		reflect.TypeOf(DownloadInput{}),
		reflect.TypeOf(DownloadOutput{}),
		reflect.TypeOf(PurchasePreviewInput{}),
		reflect.TypeOf(PurchasePreviewLineInput{}),
		reflect.TypeOf(PurchasePreviewOutput{}),
		reflect.TypeOf(PurchasePreviewLineOutput{}),
		reflect.TypeOf(SetPartParametersInput{}),
		reflect.TypeOf(ParameterSetInput{}),
		reflect.TypeOf(UpsertPartWorkflowInput{}),
		reflect.TypeOf(PartUpsertWorkflowOutput{}),
		reflect.TypeOf(PartUpsertWorkflowAction{}),
		reflect.TypeOf(InitialStockWorkflowInput{}),
		reflect.TypeOf(InitialStockWorkflowOutput{}),
		reflect.TypeOf(InitialStockWorkflowAction{}),
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

func TestToolReferenceDocumentsRegisteredLookupTools(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data, err := os.ReadFile("../../docs/tool-reference.md")
	r.NoError(err)
	docs := string(data)

	a.Contains(docs, "## Registered Lookup Tools")
	a.Contains(docs, "`"+ScopeInventreeRead+"`")
	a.Contains(docs, "`readOnlyHint:true`")
	a.Contains(docs, "`destructiveHint:false`")
	a.Contains(docs, "`idempotentHint:true`")
	a.Contains(docs, "`openWorldHint:false`")
	for _, name := range lookupToolNames {
		a.Contains(docs, "`"+name+"`")
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		a.Equal("read_only", auth.MutationClass)
		a.Equal([]string{ScopeInventreeRead}, auth.Scopes)
	}
}

func TestToolReferenceDocumentsRegisteredWriteTools(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data, err := os.ReadFile("../../docs/tool-reference.md")
	r.NoError(err)
	docs := string(data)

	a.Contains(docs, "## Registered Write Tools")
	a.Contains(docs, "`"+ScopeInventreeWrite+"`")
	a.Contains(docs, "`readOnlyHint:false`")
	a.Contains(docs, "`destructiveHint:false`")
	a.Contains(docs, "`idempotentHint:false`")
	a.Contains(docs, "`openWorldHint:false`")
	a.Contains(docs, "HTTP mode does not register them until `M1C-S04`")
	for _, name := range writeToolNames {
		a.Contains(docs, "`"+name+"`")
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		if name == CreateStockItemToolName || name == InitialStockWorkflowToolName {
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
			a.Contains(docs, "`"+ScopeInventreeOperational+"`")
		} else {
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite}, auth.Scopes)
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
