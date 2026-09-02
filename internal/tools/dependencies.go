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
	ClientFromContext   func(context.Context) (any, error)
	EnableWriteTools    bool
	AuthorizationMode   AuthorizationMode
	ResourceMetadataURL string
	UploadMode          upload.Mode
	UploadFS            afero.Fs
	UploadAllowRoots    []string
	UploadMaxBytes      int64
	UploadTimeout       time.Duration
	URLFetcher          upload.URLFetcher
	WebLinks            *weblinks.Resolver
	// BulkMaxItems and BulkConcurrency configure every bulk_update_*/
	// bulk_set_stock_status tool's item-count limit and worker concurrency.
	// A value <= 0 (including the zero value left by an uninitialized
	// Dependencies literal, such as in a test fixture) is not enforced
	// directly — every call site reads these through effectiveBulkMaxItems/
	// effectiveBulkConcurrency (bulk_progress.go), which fall back to
	// defaultBulkMaxItems/defaultBulkConcurrency instead of treating every
	// batch as oversized. Do not read these fields directly in new code.
	BulkMaxItems                         int
	BulkConcurrency                      int
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
	stockTransferBulkPlanStore           *batch.Store[stockTransferBulkPlan]
	purchaseOrderBulkPlanStore           *batch.Store[purchaseOrderBulkPlan]
	purchaseOrderLineBulkPlanStore       *batch.Store[purchaseOrderLineBulkPlan]
	purchaseOrderExtraLineBulkPlanStore  *batch.Store[purchaseOrderExtraLineBulkPlan]
	attachmentBulkPlanStore              *batch.Store[attachmentBulkPlan]
	objectParameterBulkPlanStore         *batch.Store[objectParameterBulkPlan]
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
