package tools

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
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

func TestCheckedToolManifestMatchesGeneratedMetadata(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var checked ToolManifestDocument
	r.NoError(json.Unmarshal(docs.ToolManifestJSON(), &checked))

	generated := GenerateToolManifest()
	a.Equal(generated, checked)

	toolReference := docs.ToolReferenceMarkdown()
	milestoneRows := markdownRowsByFirstCell(section(toolReference, "## Milestone 1 Tools", "## Future Tools"))
	skeletonRows := markdownRowsByFirstCell(section(toolReference, "## Skeleton Tools", "## Registered Prompts"))
	for _, entry := range checked.Tools {
		a.Contains(toolReference, "`"+entry.Name+"`")
		a.Contains(toolReference, "`"+entry.MutationClass+"`")
		for _, scope := range entry.Scopes {
			a.Contains(toolReference, "`"+scope+"`")
		}
		row := milestoneRows[entry.Name]
		classCell := 2
		scopesCell := 3
		uploadSourcesCell := 4
		annotationsCell := 5
		httpRegistrationCell := 6
		if entry.Name == HealthVersionToolName {
			row = skeletonRows[entry.Name]
			classCell = 3
			scopesCell = 5
			uploadSourcesCell = 6
			annotationsCell = 4
			httpRegistrationCell = 7
		}
		r.NotEmpty(row, entry.Name)
		r.Greater(len(row), httpRegistrationCell, entry.Name)
		a.Equal(entry.MutationClass, normalizeClassCell(row[classCell]), entry.Name)
		a.ElementsMatch(entry.Scopes, markdownCodeValues(row[scopesCell]), entry.Name)
		a.ElementsMatch(uploadSourceLabels(entry.UploadSources), markdownListValues(row[uploadSourcesCell]), entry.Name)
		a.ElementsMatch(annotationLabels(entry.Annotations), markdownCodeValues(row[annotationsCell]), entry.Name)
		a.Equal([]string{entry.HTTPRegistration}, markdownCodeValues(row[httpRegistrationCell]), entry.Name)
	}
	for _, prompt := range checked.Prompts {
		a.Contains(toolReference, "`"+prompt.Name+"`")
		a.Contains(toolReference, "`"+prompt.MilestoneStatus+"`")
	}
}

func section(markdown string, start string, end string) string {
	startIndex := strings.Index(markdown, start)
	if startIndex < 0 {
		return ""
	}
	remaining := markdown[startIndex:]
	endIndex := strings.Index(remaining[len(start):], end)
	if endIndex < 0 {
		return remaining
	}
	return remaining[:len(start)+endIndex]
}

func markdownRowsByFirstCell(markdown string) map[string][]string {
	rows := make(map[string][]string)
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		trimmed := make([]string, 0, len(cells)-2)
		for _, cell := range cells[1 : len(cells)-1] {
			trimmed = append(trimmed, strings.TrimSpace(cell))
		}
		name := strings.Trim(trimmed[0], "`")
		if name == "" || name == "Tool" {
			continue
		}
		rows[name] = trimmed
	}
	return rows
}

func normalizeClassCell(cell string) string {
	cell = strings.TrimSpace(strings.Split(cell, ",")[0])
	cell = strings.Trim(cell, "`")
	cell = strings.ToLower(cell)
	return strings.ReplaceAll(cell, "-", "_")
}

func markdownCodeValues(cell string) []string {
	parts := strings.Split(cell, "`")
	values := make([]string, 0, len(parts)/2)
	for i := 1; i < len(parts); i += 2 {
		values = append(values, parts[i])
	}
	if len(values) == 0 && strings.HasPrefix(strings.TrimSpace(cell), "None") {
		return nil
	}
	return values
}

func markdownListValues(cell string) []string {
	if strings.TrimSpace(cell) == "None" {
		return nil
	}
	parts := strings.Split(cell, ";")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, strings.TrimSpace(part))
	}
	return values
}

func uploadSourceLabels(uploadSources []string) []string {
	labels := make([]string, 0, len(uploadSources))
	for _, uploadSource := range uploadSources {
		switch uploadSource {
		case "inline_base64":
			labels = append(labels, "Inline bytes")
		case "stdio_local_path":
			labels = append(labels, "STDIO allowlisted local path")
		case "http_url_fetch":
			labels = append(labels, "HTTP(S) URL only")
		case "http_url_link":
			labels = append(labels, "HTTP(S) link only, no fetch")
		case "existing_attachment_image":
			labels = append(labels, "Existing attachment/image ID")
		default:
			labels = append(labels, uploadSource)
		}
	}
	return labels
}

func annotationLabels(annotations AnnotationClass) []string {
	return []string{
		"readOnlyHint:" + boolString(annotations.ReadOnly),
		"destructiveHint:" + boolString(annotations.Destructive),
		"idempotentHint:" + boolString(annotations.Idempotent),
		"openWorldHint:" + boolString(annotations.OpenWorld),
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func jsonFieldName(tag string) string {
	for i, char := range tag {
		if char == ',' {
			return tag[:i]
		}
	}
	return tag
}
