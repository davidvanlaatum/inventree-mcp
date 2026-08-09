# User-Facing InvenTree Web Links

MCP object projections use a process-scoped, credential-free web base. Set `INVENTREE_WEB_URL` (or `--inventree-web-url`) when browser users reach InvenTree through a different public authority or path prefix than API clients. When omitted, every transport falls back to `INVENTREE_URL`. Both paths reject userinfo, query strings, fragments, unsupported schemes, invalid authorities, and non-canonical or escaped path prefixes. Production requires HTTPS; explicit development mode permits HTTP.

The effective configured base is authoritative. Request headers, forwarded host/scheme/prefix values, OAuth token envelopes, tool arguments, and clarification input cannot change link authority or route selection. A valid internal fallback may therefore disclose internal DNS/topology to authorized MCP callers, may be unreachable from their browsers, and may appear in operator-enabled debug traffic logs that capture response bodies. Configure `INVENTREE_WEB_URL` when that is not acceptable.

## Pinned Frontend Route Evidence

The route contract is pinned to InvenTree `1.4.3`, commit [`6b237de54e4cbfd7f51daff8403c17869898d965`](https://github.com/inventree/InvenTree/commit/6b237de54e4cbfd7f51daff8403c17869898d965), router blob [`ddeb3a21365761e999568c84d6417915817a9024`](https://github.com/inventree/InvenTree/blob/6b237de54e4cbfd7f51daff8403c17869898d965/src/frontend/src/router.tsx). The checked excerpt in `internal/weblinks/testdata/inventree-1.4.3-router-routes.txt` and `internal/weblinks` tests tie these declarations to the typed resolver.

| Object projection | Link field | Frontend route |
| --- | --- | --- |
| Part | `web_url` | `/part/{id}/` |
| Part category | `web_url` | `/part/category/{id}/` |
| Generic company | `web_url` | `/company/{id}/` |
| Supplier company view | `web_url` | `/purchasing/supplier/{id}/` |
| Manufacturer company view | `web_url` | `/purchasing/manufacturer/{id}/` |
| Supplier part | `web_url` | `/purchasing/supplier-part/{id}/` |
| Manufacturer part | `web_url` | `/purchasing/manufacturer-part/{id}/` |
| Stock location | `web_url` | `/stock/location/{id}/` |
| Stock item | `web_url` | `/stock/item/{id}/` |
| Purchase order | `web_url` | `/purchasing/purchase-order/{id}/` |

## Output Classification

The inventory below names the Go projections carried across the MCP boundary. Generic containers inherit the classification of their nested records; the container itself does not gain a link field.

| Projection type or output family | Classification | Link target |
| --- | --- | --- |
| `inventree.Part` | Direct | Part |
| `inventree.Category` | Direct | Part category |
| `inventree.Company`, `CompanyView`, `CompanyRecoveryView`, `CompanyImageState` | Direct | Generic company, except supplier/manufacturer searches select their role-specific company view |
| `inventree.SupplierPart`, `inventree.SupplierPartDetail`, `SupplierPartView`, `SupplierPartRecoveryView` | Direct | Supplier part |
| `inventree.ManufacturerPart`, `inventree.ManufacturerPartDetail`, `ManufacturerPartView`, `ManufacturerPartRecoveryView` | Direct | Manufacturer part |
| `inventree.StockLocation`, `StockTransferLocation`, `StockLocationPlanState`, `StockLocationPlanContext` | Direct | Stock location |
| `inventree.StockItem`, `StockStateSnapshot`, `StockMetadataState` | Direct | Stock item |
| `inventree.PurchaseOrder` | Direct | Purchase order |
| `LookupOutput[T]`, `RecordOutput[T]`, `WriteRecordOutput[T]`, `CompanyAdminSearchOutput[T]`, `CompanyAdminRecordOutput[T]`, `CompanyMutationOutput[T,R]` | Nested/mixed | Each contained record or recovery projection uses its own direct or parent classification |
| `CategoryHierarchyContext`, `CategoryMutationOutput`, `CategoryParameterDefaultsOutput`, `PartUpsertWorkflowOutput`, `InitialStockWorkflowOutput`, `AttachmentWriteOutput`, `PurchaseOrderExtraLineOutput`, `CategoryParameterDefaultOutput`, `DeletePartParameterOutput`, `ParameterTemplateOutput`, `MergeParameterTemplatesOutput`, `PurchaseOrderLineDeleteOutput`, `CompanyImageOutput`, `StockLocationPlan`, `StockMetadataPlan`, `StockLocationMutationOutput`, `StockMetadataMutationOutput`, `BulkParameterOutput`, `StockAdjustmentOutput` (and its public alias `StockTransferOutput`), `PurchaseOrderWorkflowOutput`, `ReceivePurchaseOrderOutput`, `IssuePurchaseOrderOutput`, `PartDeleteOutput`, `ClarificationResponse` | Nested/mixed | Stable nested object, state, recovery, plan, or clarification records use the classifications enumerated here |

Subordinate records without a stable dedicated router declaration omit `web_url` and use `parent_web_url` only for their immediate owner:

| Subordinate projection type | Classification | Immediate owner |
| --- | --- |
| `inventree.PurchaseOrderLineItem`, `inventree.PurchaseOrderExtraLine` | Parent | Purchase order exposed by `order` |
| `inventree.Attachment`, `AttachmentMetadata` | Parent | Supported object exposed by `model_type` and `model_id` |
| `inventree.Parameter`, `PartParameterSearchResult`, `BulkParameterAction`, `ParameterTemplateMergeAction` | Parent | Part exposed by `part_id`, or supported object exposed by `model_type` and `model_id` |
| `inventree.CategoryParameterTemplate`, `CategoryParameterDefaultRecord` | Parent | Part category exposed by `category` or `category_id` |
| `ReceivePurchaseOrderPlanItem` | Parent | Purchase order exposed by the containing `ReceivePurchaseOrderOutput.order` record |
| `ClarificationCandidate` | Direct, parent, or omitted | Its sanitized `api_url` identifies a stable direct object, or its existing `order`, `model_type`/`model_id`, `part_id`, or `category_id` fields identify an immediate owner |

The following projections intentionally omit both fields because they are non-object metadata, aggregates, validations, content transfers, or action/context records without a stable immediate-owner identity: `inventree.ParameterTemplate`, `inventree.StockLocationType`, `inventree.PartRelation`, `HealthVersionOutput`, `DownloadOutput`, `PurchasePreviewOutput`, `PurchasePreviewLineOutput`, `TemplateReferenceSummary`, `CategoryTemplateReference`, `ParameterAuditFinding`, `ParameterAuditOutput`, `BulkParameterFailure`, `BulkParameterPlan`, `ValidationFailure`, `ValidationFieldError`, `InitialStockWorkflowAction`, `PartUpsertWorkflowAction`, `PartUpsertWorkflowFailure`, `PlannedChange`, `PlannedChangeDependency`, `StockDepletionContext`, `StockTransferProvenance`, `StockTransferSafety`, `StockTransferContext`, `StockAdjustmentPlan`, `StockAdjustmentFailure`, `PurchaseOrderWorkflowAction`, `PurchaseOrderWorkflowFailure`, `ReceivePurchaseOrderItem`, and `PartDeleteBlockingReferences`. Input-only, registration, manifest, prompt, authorization, and internal confirmation/store structs are not MCP output records and are outside the projection inventory. The resolver never walks to a more distant ancestor.

Clarification candidates use the breaking explicit contract:

- `web_url`: absolute frontend URL when the candidate itself has a stable page;
- `parent_web_url`: absolute immediate-owner page for supported subordinate candidates;
- `api_url`: sanitized relative REST inspection path when one exists.

The former ambiguous `url` candidate field is removed. `api_url` is clarification-only and is not added universally to object records.
