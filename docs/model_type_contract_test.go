package docs_test

import (
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/stretchr/testify/assert"
)

const (
	attachmentModelTypeList = "`part`, `stockitem`, `company`, `manufacturerpart`, `supplierpart`, or `purchaseorder`"
	parameterModelTypeList  = "`build.build`, `company.company`, `company.manufacturerpart`, `company.supplierpart`, `order.purchaseorder`, `order.returnorder`, `order.salesorder`, `order.salesordershipment`, `order.transferorder`, `part.part`, `part.partcategory`, or `stock.stocklocation`"
)

func TestModelTypeDocumentationKeepsEndpointVocabulariesSeparate(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	toolReference := docs.ToolReferenceMarkdown()
	a.Contains(toolReference, "Attachment endpoint model type")
	a.Contains(toolReference, "parameter endpoint restriction")
	a.Contains(toolReference, "qualified parameter values such as `part.part` and `order.purchaseorder` are invalid for attachment tools")
	a.Contains(toolReference, "short attachment values such as `part` and `purchaseorder` are invalid")
	a.Contains(toolReference, attachmentModelTypeList)
	a.Contains(toolReference, parameterModelTypeList)

	operatorRecipes := docs.OperatorRecipesMarkdown()
	a.Contains(operatorRecipes, "attachment tools use the attachment endpoint's short, unqualified values")
	a.Contains(operatorRecipes, "parameter templates use the parameter endpoint's qualified `app.model` values")
	a.Contains(operatorRecipes, attachmentModelTypeList)
	a.Contains(operatorRecipes, parameterModelTypeList)

	apiSchema := docs.APISchemaMarkdown()
	a.Contains(apiSchema, "attachment endpoint's `AttachmentModelTypeEnum` uses short, unqualified values")
	a.Contains(apiSchema, "parameter endpoint's `ModelTypeD42Enum`, which uses qualified Django-style `app.model` values")
}
