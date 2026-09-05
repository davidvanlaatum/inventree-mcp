package inventree

// RequestFamily is the finite, closed classification of outbound InvenTree
// HTTP request shapes. Every included Client method maps to exactly one.
type RequestFamily string

const (
	RequestFamilyJSONAPI            RequestFamily = "json_api"
	RequestFamilyMultipartAPI       RequestFamily = "multipart_api"
	RequestFamilyAttachmentDownload RequestFamily = "attachment_download"
	RequestFamilyImageDownload      RequestFamily = "image_download"
	RequestFamilyDataOutputDownload RequestFamily = "data_output_download"
)

// clientRoute is one closed-registry entry: the exact HTTP method and path
// template (with {param} placeholders matching docs/endpoint-manifest.yaml's
// path style) a Client method issues, its request family, and the
// docs/endpoint-manifest.yaml `id` it corresponds to.
type clientRoute struct {
	Method     string
	Path       string
	Family     RequestFamily
	ManifestID string
}

// clientMethodRoutes is the closed registry: one entry per included exported
// Client method name. A drift test enforces this is exactly the set of
// exported Client methods that issue InvenTree HTTP requests (excluding the
// generic per-request helpers Patch/Post/NewRequest/DoJSON, which are not
// one-per-endpoint; GetCurrentUser/CreateCurrentUserToken, which are
// credential-bearing bootstrap/OAuth calls issued against a throwaway
// client outside normal tool dispatch; and ClearCompanyPrimaryImage, which
// delegates to UpdateCompany and issues no independent request).
//
// The four download methods (DownloadAttachment, DownloadPartImage,
// DownloadCompanyImage, DownloadDataOutput) are the only entries whose
// ManifestID does not resolve to a real docs/endpoint-manifest.yaml id: they
// hit InvenTree's opaque signed /media/... content URLs, which are not
// OpenAPI-documented endpoints at all. The drift test exempts exactly the
// three download families (attachment_download/image_download/
// data_output_download) from the manifest cross-check for this reason;
// their Path field ("/media/{path}") is documentation only and is never used
// for request matching — see resolveRoute in request_logging.go, which
// identifies these four via an explicit requestctx marker each method sets
// itself, since the destination URL carries no distinguishing information.
var clientMethodRoutes = map[string]clientRoute{
	// from read_methods.go
	"SearchParts":                          {Method: "GET", Path: "/api/part/", Family: RequestFamilyJSONAPI, ManifestID: "search_parts"},
	"SearchPartsPage":                      {Method: "GET", Path: "/api/part/", Family: RequestFamilyJSONAPI, ManifestID: "search_parts"},
	"SearchPartsByQuery":                   {Method: "GET", Path: "/api/part/", Family: RequestFamilyJSONAPI, ManifestID: "search_parts"},
	"GetPart":                              {Method: "GET", Path: "/api/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part"},
	"GetPartDetail":                        {Method: "GET", Path: "/api/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part"},
	"SearchPartCategories":                 {Method: "GET", Path: "/api/part/category/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_categories"},
	"SearchPartCategoriesPage":             {Method: "GET", Path: "/api/part/category/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_categories"},
	"GetPartCategory":                      {Method: "GET", Path: "/api/part/category/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_category"},
	"SearchCompanies":                      {Method: "GET", Path: "/api/company/", Family: RequestFamilyJSONAPI, ManifestID: "search_companies"},
	"SearchCompaniesPage":                  {Method: "GET", Path: "/api/company/", Family: RequestFamilyJSONAPI, ManifestID: "search_companies"},
	"GetCompanyDetail":                     {Method: "GET", Path: "/api/company/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_company"},
	"GetCompany":                           {Method: "GET", Path: "/api/company/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_company"},
	"SearchSuppliers":                      {Method: "GET", Path: "/api/company/", Family: RequestFamilyJSONAPI, ManifestID: "search_suppliers"},
	"SearchManufacturers":                  {Method: "GET", Path: "/api/company/", Family: RequestFamilyJSONAPI, ManifestID: "search_manufacturers"},
	"SearchStockLocations":                 {Method: "GET", Path: "/api/stock/location/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_locations"},
	"SearchStockLocationsPage":             {Method: "GET", Path: "/api/stock/location/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_locations"},
	"GetStockLocation":                     {Method: "GET", Path: "/api/stock/location/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_stock_location"},
	"SearchStockLocationTypes":             {Method: "GET", Path: "/api/stock/location-type/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_location_types"},
	"GetStockLocationType":                 {Method: "GET", Path: "/api/stock/location-type/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_stock_location_type"},
	"SearchStockLocationTypesPage":         {Method: "GET", Path: "/api/stock/location-type/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_location_types"},
	"SearchStockItems":                     {Method: "GET", Path: "/api/stock/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_items"},
	"SearchStockItemsPage":                 {Method: "GET", Path: "/api/stock/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_items"},
	"SearchSalesOrdersPage":                {Method: "GET", Path: "/api/order/so/", Family: RequestFamilyJSONAPI, ManifestID: "search_sales_orders"},
	"GetStockItem":                         {Method: "GET", Path: "/api/stock/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_stock_item"},
	"GetStockItemDetail":                   {Method: "GET", Path: "/api/stock/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_stock_item"},
	"SearchPartParameters":                 {Method: "GET", Path: "/api/parameter/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_parameters"},
	"SearchPartParametersPage":             {Method: "GET", Path: "/api/parameter/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_parameters"},
	"SearchTemplateParametersPage":         {Method: "GET", Path: "/api/parameter/", Family: RequestFamilyJSONAPI, ManifestID: "search_object_parameters"},
	"SearchObjectParametersPage":           {Method: "GET", Path: "/api/parameter/", Family: RequestFamilyJSONAPI, ManifestID: "search_object_parameters"},
	"SearchTagsPage":                       {Method: "GET", Path: "/api/tag/", Family: RequestFamilyJSONAPI, ManifestID: "search_tags"},
	"GenerateBarcode":                      {Method: "POST", Path: "/api/barcode/generate/", Family: RequestFamilyJSONAPI, ManifestID: "barcode_generate_create"},
	"ResolveBarcode":                       {Method: "POST", Path: "/api/barcode/", Family: RequestFamilyJSONAPI, ManifestID: "barcode_create"},
	"LinkBarcode":                          {Method: "POST", Path: "/api/barcode/link/", Family: RequestFamilyJSONAPI, ManifestID: "barcode_link_create"},
	"UnlinkBarcode":                        {Method: "POST", Path: "/api/barcode/unlink/", Family: RequestFamilyJSONAPI, ManifestID: "barcode_unlink_create"},
	"SearchBarcodeScanHistoryPage":         {Method: "GET", Path: "/api/barcode/history/", Family: RequestFamilyJSONAPI, ManifestID: "search_barcode_scan_history"},
	"GetPartParameter":                     {Method: "GET", Path: "/api/parameter/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_parameter"},
	"SearchParameterTemplates":             {Method: "GET", Path: "/api/parameter/template/", Family: RequestFamilyJSONAPI, ManifestID: "search_parameter_templates"},
	"SearchParameterTemplatesPage":         {Method: "GET", Path: "/api/parameter/template/", Family: RequestFamilyJSONAPI, ManifestID: "search_parameter_templates"},
	"GetParameterTemplate":                 {Method: "GET", Path: "/api/parameter/template/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_parameter_template"},
	"SearchCategoryParameterTemplatesPage": {Method: "GET", Path: "/api/part/category/parameters/", Family: RequestFamilyJSONAPI, ManifestID: "search_category_parameter_templates"},
	"GetCategoryParameterTemplate":         {Method: "GET", Path: "/api/part/category/parameters/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_category_parameter_template"},
	"ListAttachments":                      {Method: "GET", Path: "/api/attachment/", Family: RequestFamilyJSONAPI, ManifestID: "list_attachments"},
	"GetAttachmentMetadata":                {Method: "GET", Path: "/api/attachment/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_attachment_metadata"},
	"DownloadAttachment":                   {Method: "GET", Path: "/media/{path}", Family: RequestFamilyAttachmentDownload, ManifestID: "download_attachment_content"},
	"DownloadPartImage":                    {Method: "GET", Path: "/media/{path}", Family: RequestFamilyImageDownload, ManifestID: "download_part_image_content"},
	"DownloadCompanyImage":                 {Method: "GET", Path: "/media/{path}", Family: RequestFamilyImageDownload, ManifestID: "download_company_image_content"},
	"SearchSupplierParts":                  {Method: "GET", Path: "/api/company/part/", Family: RequestFamilyJSONAPI, ManifestID: "search_supplier_parts"},
	"SearchSupplierPartsPage":              {Method: "GET", Path: "/api/company/part/", Family: RequestFamilyJSONAPI, ManifestID: "search_supplier_parts"},
	"GetSupplierPartDetail":                {Method: "GET", Path: "/api/company/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_supplier_part"},
	"GetSupplierPart":                      {Method: "GET", Path: "/api/company/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_supplier_part"},
	"SearchManufacturerParts":              {Method: "GET", Path: "/api/company/part/manufacturer/", Family: RequestFamilyJSONAPI, ManifestID: "search_manufacturer_parts"},
	"SearchManufacturerPartsPage":          {Method: "GET", Path: "/api/company/part/manufacturer/", Family: RequestFamilyJSONAPI, ManifestID: "search_manufacturer_parts"},
	"GetManufacturerPartDetail":            {Method: "GET", Path: "/api/company/part/manufacturer/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_manufacturer_part"},
	"SearchPurchaseOrders":                 {Method: "GET", Path: "/api/order/po/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_orders"},
	"SearchPurchaseOrdersPage":             {Method: "GET", Path: "/api/order/po/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_orders"},
	"GetPurchaseOrder":                     {Method: "GET", Path: "/api/order/po/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_purchase_order"},
	"GetPurchaseOrderDetail":               {Method: "GET", Path: "/api/order/po/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_purchase_order"},
	"SearchPurchaseOrderLines":             {Method: "GET", Path: "/api/order/po-line/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_order_lines"},
	"SearchPurchaseOrderLinesPage":         {Method: "GET", Path: "/api/order/po-line/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_order_lines"},
	"GetPurchaseOrderLine":                 {Method: "GET", Path: "/api/order/po-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_purchase_order_line"},
	"GetPurchaseOrderLineDetail":           {Method: "GET", Path: "/api/order/po-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_purchase_order_line"},
	"SearchPurchaseOrderExtraLines":        {Method: "GET", Path: "/api/order/po-extra-line/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_order_extra_lines"},
	"SearchPurchaseOrderExtraLinesPage":    {Method: "GET", Path: "/api/order/po-extra-line/", Family: RequestFamilyJSONAPI, ManifestID: "search_purchase_order_extra_lines"},
	"GetPurchaseOrderExtraLine":            {Method: "GET", Path: "/api/order/po-extra-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_purchase_order_extra_line"},
	"SearchBomItems":                       {Method: "GET", Path: "/api/bom/", Family: RequestFamilyJSONAPI, ManifestID: "search_bom_items"},
	"SearchSalesOrderLines":                {Method: "GET", Path: "/api/order/so-line/", Family: RequestFamilyJSONAPI, ManifestID: "search_sales_order_lines"},
	"SearchBuilds":                         {Method: "GET", Path: "/api/build/", Family: RequestFamilyJSONAPI, ManifestID: "search_builds"},
	"SearchBuildsPage":                     {Method: "GET", Path: "/api/build/", Family: RequestFamilyJSONAPI, ManifestID: "search_builds"},
	"SearchTransferOrdersPage":             {Method: "GET", Path: "/api/order/transfer-order/", Family: RequestFamilyJSONAPI, ManifestID: "search_transfer_orders_for_stock_location_delete"},
	"SearchPartRelations":                  {Method: "GET", Path: "/api/part/related/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_relations"},
	"SearchPartRelationsPage":              {Method: "GET", Path: "/api/part/related/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_relations"},
	"GetPartRelation":                      {Method: "GET", Path: "/api/part/related/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_relation"},
	"SearchPartInternalPriceBreaksPage":    {Method: "GET", Path: "/api/part/internal-price/", Family: RequestFamilyJSONAPI, ManifestID: "search_internal_price_breaks"},
	"GetPartInternalPriceBreak":            {Method: "GET", Path: "/api/part/internal-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_internal_price_break"},
	"SearchPartSalePriceBreaksPage":        {Method: "GET", Path: "/api/part/sale-price/", Family: RequestFamilyJSONAPI, ManifestID: "search_sale_price_breaks"},
	"GetPartSalePriceBreak":                {Method: "GET", Path: "/api/part/sale-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_sale_price_break"},
	"SearchSupplierPriceBreaksPage":        {Method: "GET", Path: "/api/company/price-break/", Family: RequestFamilyJSONAPI, ManifestID: "search_supplier_price_breaks"},
	"GetSupplierPriceBreak":                {Method: "GET", Path: "/api/company/price-break/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_supplier_price_break"},
	"GetPartPricing":                       {Method: "GET", Path: "/api/part/{id}/pricing/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_pricing"},
	"SearchStockTrackingPage":              {Method: "GET", Path: "/api/stock/track/", Family: RequestFamilyJSONAPI, ManifestID: "search_stock_tracking"},
	"GetStockTrackingEntry":                {Method: "GET", Path: "/api/stock/track/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_stock_tracking_entry"},
	"SearchPartStocktakesPage":             {Method: "GET", Path: "/api/part/stocktake/", Family: RequestFamilyJSONAPI, ManifestID: "search_part_stocktakes"},
	"GetPartStocktake":                     {Method: "GET", Path: "/api/part/stocktake/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_stocktake"},
	"GeneratePartStocktake":                {Method: "POST", Path: "/api/part/stocktake/generate/", Family: RequestFamilyJSONAPI, ManifestID: "generate_part_stocktake"},
	"GetDataOutput":                        {Method: "GET", Path: "/api/data-output/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_data_output"},
	"DownloadDataOutput":                   {Method: "GET", Path: "/media/{path}", Family: RequestFamilyDataOutputDownload, ManifestID: "download_data_output_content"},
	"SearchOwnersPage":                     {Method: "GET", Path: "/api/user/owner/", Family: RequestFamilyJSONAPI, ManifestID: "search_owners"},
	"GetOwner":                             {Method: "GET", Path: "/api/user/owner/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_owner"},
	"SearchUsersPage":                      {Method: "GET", Path: "/api/user/", Family: RequestFamilyJSONAPI, ManifestID: "search_users"},
	"GetUser":                              {Method: "GET", Path: "/api/user/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_user"},
	"SearchContactsPage":                   {Method: "GET", Path: "/api/company/contact/", Family: RequestFamilyJSONAPI, ManifestID: "search_contacts"},
	"GetContact":                           {Method: "GET", Path: "/api/company/contact/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_contact"},
	"SearchAddressesPage":                  {Method: "GET", Path: "/api/company/address/", Family: RequestFamilyJSONAPI, ManifestID: "search_addresses"},
	"GetAddress":                           {Method: "GET", Path: "/api/company/address/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_address"},
	"SearchProjectCodesPage":               {Method: "GET", Path: "/api/project-code/", Family: RequestFamilyJSONAPI, ManifestID: "search_project_codes"},
	"GetProjectCode":                       {Method: "GET", Path: "/api/project-code/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "get_project_code"},

	// from write_methods.go
	"CreatePart":                      {Method: "POST", Path: "/api/part/", Family: RequestFamilyJSONAPI, ManifestID: "create_part"},
	"UpdatePart":                      {Method: "PATCH", Path: "/api/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_part"},
	"DeletePart":                      {Method: "DELETE", Path: "/api/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_part"},
	"CreatePartRelation":              {Method: "POST", Path: "/api/part/related/", Family: RequestFamilyJSONAPI, ManifestID: "create_part_relation"},
	"UpdatePartRelation":              {Method: "PATCH", Path: "/api/part/related/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_part_relation"},
	"DeletePartRelation":              {Method: "DELETE", Path: "/api/part/related/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_part_relation"},
	"CreatePartInternalPriceBreak":    {Method: "POST", Path: "/api/part/internal-price/", Family: RequestFamilyJSONAPI, ManifestID: "create_internal_price_break"},
	"UpdatePartInternalPriceBreak":    {Method: "PATCH", Path: "/api/part/internal-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_internal_price_break"},
	"DeletePartInternalPriceBreak":    {Method: "DELETE", Path: "/api/part/internal-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_internal_price_break"},
	"CreatePartSalePriceBreak":        {Method: "POST", Path: "/api/part/sale-price/", Family: RequestFamilyJSONAPI, ManifestID: "create_sale_price_break"},
	"UpdatePartSalePriceBreak":        {Method: "PATCH", Path: "/api/part/sale-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_sale_price_break"},
	"DeletePartSalePriceBreak":        {Method: "DELETE", Path: "/api/part/sale-price/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_sale_price_break"},
	"CreateSupplierPriceBreak":        {Method: "POST", Path: "/api/company/price-break/", Family: RequestFamilyJSONAPI, ManifestID: "create_supplier_price_break"},
	"UpdateSupplierPriceBreak":        {Method: "PATCH", Path: "/api/company/price-break/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_supplier_price_break"},
	"DeleteSupplierPriceBreak":        {Method: "DELETE", Path: "/api/company/price-break/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_supplier_price_break"},
	"UpdatePartPricing":               {Method: "PATCH", Path: "/api/part/{id}/pricing/", Family: RequestFamilyJSONAPI, ManifestID: "update_part_pricing"},
	"RefreshPartPricing":              {Method: "PATCH", Path: "/api/part/{id}/pricing/", Family: RequestFamilyJSONAPI, ManifestID: "update_part_pricing"},
	"CreatePartCategory":              {Method: "POST", Path: "/api/part/category/", Family: RequestFamilyJSONAPI, ManifestID: "create_part_category"},
	"UpdatePartCategory":              {Method: "PATCH", Path: "/api/part/category/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_part_category"},
	"DeletePartCategory":              {Method: "DELETE", Path: "/api/part/category/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_part_category"},
	"CreateCompany":                   {Method: "POST", Path: "/api/company/", Family: RequestFamilyJSONAPI, ManifestID: "create_company"},
	"UpdateCompany":                   {Method: "PATCH", Path: "/api/company/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_company"},
	"CreateSupplierPart":              {Method: "POST", Path: "/api/company/part/", Family: RequestFamilyJSONAPI, ManifestID: "create_supplier_part"},
	"UpdateSupplierPart":              {Method: "PATCH", Path: "/api/company/part/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_supplier_part"},
	"CreateManufacturerPart":          {Method: "POST", Path: "/api/company/part/manufacturer/", Family: RequestFamilyJSONAPI, ManifestID: "create_manufacturer_part"},
	"UpdateManufacturerPart":          {Method: "PATCH", Path: "/api/company/part/manufacturer/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_manufacturer_part"},
	"CreatePartParameter":             {Method: "POST", Path: "/api/parameter/", Family: RequestFamilyJSONAPI, ManifestID: "create_part_parameter"},
	"UpdatePartParameter":             {Method: "PATCH", Path: "/api/parameter/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_part_parameter"},
	"DeletePartParameter":             {Method: "DELETE", Path: "/api/parameter/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_part_parameter"},
	"CreateParameterTemplate":         {Method: "POST", Path: "/api/parameter/template/", Family: RequestFamilyJSONAPI, ManifestID: "create_parameter_template"},
	"UpdateParameterTemplate":         {Method: "PATCH", Path: "/api/parameter/template/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_parameter_template"},
	"DeleteParameterTemplate":         {Method: "DELETE", Path: "/api/parameter/template/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_parameter_template"},
	"CreateCategoryParameterTemplate": {Method: "POST", Path: "/api/part/category/parameters/", Family: RequestFamilyJSONAPI, ManifestID: "create_category_parameter_template"},
	"UpdateCategoryParameterTemplate": {Method: "PATCH", Path: "/api/part/category/parameters/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_category_parameter_template"},
	"DeleteCategoryParameterTemplate": {Method: "DELETE", Path: "/api/part/category/parameters/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_category_parameter_template"},
	"CreateStockItem":                 {Method: "POST", Path: "/api/stock/", Family: RequestFamilyJSONAPI, ManifestID: "create_stock_item"},
	"UpdateStockItem":                 {Method: "PATCH", Path: "/api/stock/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_stock_item"},
	"CreateStockLocation":             {Method: "POST", Path: "/api/stock/location/", Family: RequestFamilyJSONAPI, ManifestID: "create_stock_location"},
	"UpdateStockLocation":             {Method: "PATCH", Path: "/api/stock/location/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_stock_location"},
	"CreateStockLocationType":         {Method: "POST", Path: "/api/stock/location-type/", Family: RequestFamilyJSONAPI, ManifestID: "create_stock_location_type"},
	"UpdateStockLocationType":         {Method: "PATCH", Path: "/api/stock/location-type/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_stock_location_type"},
	"DeleteStockLocationType":         {Method: "DELETE", Path: "/api/stock/location-type/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_stock_location_type"},
	"DeleteStockLocation":             {Method: "DELETE", Path: "/api/stock/location/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_stock_location"},
	"AddStock":                        {Method: "POST", Path: "/api/stock/add/", Family: RequestFamilyJSONAPI, ManifestID: "add_stock"},
	"RemoveStock":                     {Method: "POST", Path: "/api/stock/remove/", Family: RequestFamilyJSONAPI, ManifestID: "remove_stock"},
	"CountStock":                      {Method: "POST", Path: "/api/stock/count/", Family: RequestFamilyJSONAPI, ManifestID: "count_stock"},
	"TransferStock":                   {Method: "POST", Path: "/api/stock/transfer/", Family: RequestFamilyJSONAPI, ManifestID: "transfer_stock"},
	"ChangeStockStatus":               {Method: "POST", Path: "/api/stock/change_status/", Family: RequestFamilyJSONAPI, ManifestID: "change_stock_status"},
	"InstallStockItem":                {Method: "POST", Path: "/api/stock/{id}/install/", Family: RequestFamilyJSONAPI, ManifestID: "install_stock_item"},
	"UninstallStockItem":              {Method: "POST", Path: "/api/stock/{id}/uninstall/", Family: RequestFamilyJSONAPI, ManifestID: "uninstall_stock_item"},
	"CreatePurchaseOrder":             {Method: "POST", Path: "/api/order/po/", Family: RequestFamilyJSONAPI, ManifestID: "create_purchase_order"},
	"UpdatePurchaseOrder":             {Method: "PATCH", Path: "/api/order/po/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_purchase_order"},
	"UpdatePurchaseOrderDetail":       {Method: "PATCH", Path: "/api/order/po/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_purchase_order"},
	"CreatePurchaseOrderLine":         {Method: "POST", Path: "/api/order/po-line/", Family: RequestFamilyJSONAPI, ManifestID: "add_purchase_order_line"},
	"UpdatePurchaseOrderLine":         {Method: "PATCH", Path: "/api/order/po-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_purchase_order_line"},
	"DeletePurchaseOrderLine":         {Method: "DELETE", Path: "/api/order/po-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_purchase_order_line"},
	"CreatePurchaseOrderExtraLine":    {Method: "POST", Path: "/api/order/po-extra-line/", Family: RequestFamilyJSONAPI, ManifestID: "create_purchase_order_extra_line"},
	"UpdatePurchaseOrderExtraLine":    {Method: "PATCH", Path: "/api/order/po-extra-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_purchase_order_extra_line"},
	"DeletePurchaseOrderExtraLine":    {Method: "DELETE", Path: "/api/order/po-extra-line/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_purchase_order_extra_line"},
	"ReceivePurchaseOrder":            {Method: "POST", Path: "/api/order/po/{id}/receive/", Family: RequestFamilyJSONAPI, ManifestID: "receive_purchase_order_items"},
	"CompletePurchaseOrder":           {Method: "POST", Path: "/api/order/po/{id}/complete/", Family: RequestFamilyJSONAPI, ManifestID: "complete_purchase_order"},
	"IssuePurchaseOrder":              {Method: "POST", Path: "/api/order/po/{id}/issue/", Family: RequestFamilyJSONAPI, ManifestID: "issue_purchase_order"},
	"HoldPurchaseOrder":               {Method: "POST", Path: "/api/order/po/{id}/hold/", Family: RequestFamilyJSONAPI, ManifestID: "hold_purchase_order"},
	"CancelPurchaseOrder":             {Method: "POST", Path: "/api/order/po/{id}/cancel/", Family: RequestFamilyJSONAPI, ManifestID: "cancel_purchase_order"},
	"UploadAttachment":                {Method: "POST", Path: "/api/attachment/", Family: RequestFamilyMultipartAPI, ManifestID: "upload_attachment"},
	"CreateLinkAttachment":            {Method: "POST", Path: "/api/attachment/", Family: RequestFamilyMultipartAPI, ManifestID: "upload_attachment"},
	"UpdateAttachmentMetadata":        {Method: "PATCH", Path: "/api/attachment/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "update_attachment_metadata"},
	"DeleteAttachment":                {Method: "DELETE", Path: "/api/attachment/{id}/", Family: RequestFamilyJSONAPI, ManifestID: "delete_attachment"},
	"SetPartPrimaryImage":             {Method: "PATCH", Path: "/api/part/{id}/", Family: RequestFamilyMultipartAPI, ManifestID: "update_part"},
	"SetCompanyPrimaryImage":          {Method: "PATCH", Path: "/api/company/{id}/", Family: RequestFamilyMultipartAPI, ManifestID: "set_company_primary_image"},

	// from global_search.go
	"GlobalSearch": {Method: "POST", Path: "/api/search/", Family: RequestFamilyJSONAPI, ManifestID: "global_search"},

	// from instance_info.go
	"GetServerInfo":    {Method: "GET", Path: "/api/", Family: RequestFamilyJSONAPI, ManifestID: "get_instance_server_info"},
	"GetVersionInfo":   {Method: "GET", Path: "/api/version/", Family: RequestFamilyJSONAPI, ManifestID: "get_instance_version_info"},
	"GetGlobalSetting": {Method: "GET", Path: "/api/settings/global/{key}/", Family: RequestFamilyJSONAPI, ManifestID: "get_instance_global_setting"},
	"GetUserSetting":   {Method: "GET", Path: "/api/settings/user/{key}/", Family: RequestFamilyJSONAPI, ManifestID: "get_instance_user_setting"},

	// from stock_serial.go
	"GetPartSerialNumbers": {Method: "GET", Path: "/api/part/{id}/serial-numbers/", Family: RequestFamilyJSONAPI, ManifestID: "get_part_serial_numbers"},
}
