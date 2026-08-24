package tools

import (
	"context"
	"errors"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/spf13/afero"
)

var ErrLookupClientUnavailable = errors.New("InvenTree lookup client unavailable")

type Dependencies struct {
	ClientFromContext                    func(context.Context) (any, error)
	EnableWriteTools                     bool
	AuthorizationMode                    AuthorizationMode
	ResourceMetadataURL                  string
	UploadMode                           upload.Mode
	UploadFS                             afero.Fs
	UploadAllowRoots                     []string
	UploadMaxBytes                       int64
	UploadTimeout                        time.Duration
	URLFetcher                           upload.URLFetcher
	WebLinks                             *weblinks.Resolver
	stockPlanStore                       *stockPlanStore
	stockProvenancePlanStore             *stockProvenancePlanStore
	parameterPlanStore                   *parameterPlanStore
	partFamilyPlanStore                  *partFamilyPlanStore
	partRelationPlanStore                *partRelationPlanStore
	companyRolePlanStore                 *companyRolePlanStore
	ownerPlanStore                       *ownerPlanStore
	contactPlanStore                     *contactPlanStore
	addressPlanStore                     *addressPlanStore
	projectCodePlanStore                 *projectCodePlanStore
	stockLocationTypeDeletePlanStore     *stockLocationTypeDeletePlanStore
	stockLocationDeletePlanStore         *stockLocationDeletePlanStore
	objectParameterDeletePlanStore       *objectParameterDeletePlanStore
	parameterTemplateUniquenessPlanStore *parameterTemplateUniquenessPlanStore
	purchaseOrderLifecyclePlanStore      *purchaseOrderLifecyclePlanStore
	categoryDeletePlanStore              *categoryDeletePlanStore
	stocktakePlanStore                   *stocktakePlanStore
	stocktakeTaskStore                   *stocktakeTaskStore
	partBulkPlanStore                    *batch.Store[partBulkPlan]
	companyBulkPlanStore                 *batch.Store[companyBulkPlan]
	categoryBulkPlanStore                *batch.Store[categoryBulkPlan]
	supplierPartBulkPlanStore            *batch.Store[supplierPartBulkPlan]
	manufacturerPartBulkPlanStore        *batch.Store[manufacturerPartBulkPlan]
	stockMetadataBulkPlanStore           *batch.Store[stockMetadataBulkPlan]
	stockStatusBulkPlanStore             *batch.Store[stockStatusBulkPlan]
	purchaseOrderBulkPlanStore           *batch.Store[purchaseOrderBulkPlan]
	purchaseOrderLineBulkPlanStore       *batch.Store[purchaseOrderLineBulkPlan]
	purchaseOrderExtraLineBulkPlanStore  *batch.Store[purchaseOrderExtraLineBulkPlan]
}

func (d Dependencies) Client(ctx context.Context) (any, error) {
	if d.ClientFromContext == nil {
		return nil, ErrLookupClientUnavailable
	}
	client, err := d.ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrLookupClientUnavailable
	}
	return client, nil
}
