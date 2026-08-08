package tools

import (
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	attachmentModelTypeClause = "part, stockitem, company, manufacturerpart, supplierpart, or purchaseorder"
	parameterModelTypeClause  = "build.build, company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, order.returnorder, order.salesorder, order.salesordershipment, order.transferorder, part.part, part.partcategory, or stock.stocklocation"
)

func TestRegisteredModelTypeSchemasDocumentEndpointSpecificVocabularies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	session, closeSession := plannedChangesSession(t, ctx, &fakeMilestoneLookupClient{})
	defer closeSession()

	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	descriptions := make(map[string]string)
	modelTypeSchemas := make(map[string]map[string]any)
	for _, tool := range listed.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			continue
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		modelType, ok := properties["model_type"].(map[string]any)
		if !ok {
			continue
		}
		description, _ := modelType["description"].(string)
		descriptions[tool.Name] = description
		modelTypeSchemas[tool.Name] = modelType
	}

	attachmentValues := []string{"part", "stockitem", "company", "manufacturerpart", "supplierpart", "purchaseorder"}
	a.Len(inScopeAttachmentModelTypes, len(attachmentValues))
	for _, value := range attachmentValues {
		a.True(inScopeAttachmentModelTypes[value], "runtime attachment allowlist must contain %s", value)
		a.False(validParameterTemplateModelType(value), "parameter allowlist must reject attachment model type %s", value)
	}
	for _, name := range []string{ListAttachmentsToolName, UploadAttachmentToolName, UploadAttachmentFromURLToolName, CreateLinkAttachmentToolName} {
		description, ok := descriptions[name]
		r.True(ok, "missing model_type schema for %s", name)
		a.Contains(description, attachmentModelTypeClause, "%s must document the complete attachment model-type clause", name)
		a.Contains(description, "short, unqualified", name)
		a.Contains(description, "Do not use parameter model types", name)
		a.NotContains(modelTypeSchemas[name], "enum", "%s must preserve handler-level validation behavior", name)
	}

	parameterValues := []string{
		"build.build", "company.company", "company.manufacturerpart", "company.supplierpart",
		"order.purchaseorder", "order.returnorder", "order.salesorder", "order.salesordershipment",
		"order.transferorder", "part.part", "part.partcategory", "stock.stocklocation",
	}
	for _, value := range parameterValues {
		a.True(validParameterTemplateModelType(value), "runtime parameter allowlist must contain %s", value)
		a.False(inScopeAttachmentModelTypes[value], "attachment allowlist must reject parameter model type %s", value)
	}
	a.True(validParameterTemplateModelType(""), "empty parameter model type must remain unrestricted")
	for _, name := range []string{CreateParameterTemplateToolName, UpdateParameterTemplateToolName} {
		description, ok := descriptions[name]
		r.True(ok, "missing model_type schema for %s", name)
		a.Contains(description, parameterModelTypeClause, "%s must document the complete parameter model-type clause", name)
		a.Contains(description, "qualified app.model", name)
		a.Contains(description, "empty string for no restriction", name)
		a.Contains(description, "Do not use attachment model types", name)
		a.NotContains(modelTypeSchemas[name], "enum", "%s must preserve handler-level validation behavior", name)
	}
}
