package tools

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolReferenceDocumentsLookupFrameworkSchema(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	toolReference := docs.ToolReferenceMarkdown()

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
		"`" + StatusNoImage + "`",
		"`" + strconv.Itoa(DefaultLookupLimit) + "`",
		"`" + strconv.Itoa(MaxLookupLimit) + "`",
	} {
		a.Contains(toolReference, required)
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
		reflect.TypeOf(UploadAttachmentInput{}),
		reflect.TypeOf(UploadAttachmentFromURLInput{}),
		reflect.TypeOf(CreateLinkAttachmentInput{}),
		reflect.TypeOf(UpdateAttachmentMetadataInput{}),
		reflect.TypeOf(DeleteAttachmentInput{}),
		reflect.TypeOf(SetPrimaryImageInput{}),
		reflect.TypeOf(AttachmentWriteOutput{}),
		reflect.TypeOf(ClarificationResponse{}),
		reflect.TypeOf(ClarificationCandidate{}),
	} {
		for _, field := range reflect.VisibleFields(schemaType) {
			jsonName := jsonFieldName(field.Tag.Get("json"))
			if jsonName == "" || jsonName == "-" {
				continue
			}
			a.Contains(toolReference, "`"+jsonName+"`")
		}
	}
}

func TestToolReferenceDocumentsRegisteredLookupTools(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	toolReference := docs.ToolReferenceMarkdown()

	a.Contains(toolReference, "## Registered Lookup Tools")
	a.Contains(toolReference, "`"+ScopeInventreeRead+"`")
	a.Contains(toolReference, "`readOnlyHint:true`")
	a.Contains(toolReference, "`destructiveHint:false`")
	a.Contains(toolReference, "`idempotentHint:true`")
	a.Contains(toolReference, "`openWorldHint:false`")
	for _, name := range lookupToolNames {
		a.Contains(toolReference, "`"+name+"`")
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

	toolReference := docs.ToolReferenceMarkdown()

	a.Contains(toolReference, "## Registered Write Tools")
	a.Contains(toolReference, "`"+ScopeInventreeWrite+"`")
	a.Contains(toolReference, "`"+ScopeInventreeUpload+"`")
	a.Contains(toolReference, "`"+ScopeInventreeDestructive+"`")
	a.Contains(toolReference, "`readOnlyHint:false`")
	a.Contains(toolReference, "`destructiveHint:false`")
	a.Contains(toolReference, "`idempotentHint:false`")
	a.Contains(toolReference, "`openWorldHint:false`")
	a.Contains(toolReference, "`openWorldHint:true`")
	a.Contains(toolReference, "`destructiveHint:true`")
	a.Contains(toolReference, "HTTP mode does not register them until `M1C-S04`")
	for _, name := range writeToolNames {
		a.Contains(toolReference, "`"+name+"`")
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		switch name {
		case CreateStockItemToolName, InitialStockWorkflowToolName:
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
			a.Contains(toolReference, "`"+ScopeInventreeOperational+"`")
		case UploadAttachmentToolName, UploadAttachmentFromURLToolName, CreateLinkAttachmentToolName, UpdateAttachmentMetadataToolName, SetPrimaryImageToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeUpload}, auth.Scopes)
		case DeleteAttachmentToolName:
			a.Equal("destructive", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeUpload, ScopeInventreeDestructive}, auth.Scopes)
		default:
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
