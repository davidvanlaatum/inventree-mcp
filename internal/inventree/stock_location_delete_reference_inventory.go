package inventree

// StockLocationDeleteReferenceSurface documents one verified place a
// StockLocation primary key is referenced from, and how the guarded deletion
// preflight scans it. This inventory is checked in so a future schema/API
// review has an explicit place to classify newly discovered references.
type StockLocationDeleteReferenceSurface struct {
	Name       string
	Endpoint   string
	Filter     string
	Bound      string
	Permission string
	Blocker    string
}

var StockLocationDeleteReferenceInventory = []StockLocationDeleteReferenceSurface{
	{Name: "direct_stock_items", Endpoint: "GET /api/stock/", Filter: "location=<id>", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary stock read permission; no elevated scope", Blocker: "StockItemIDs"},
	{Name: "direct_child_locations", Endpoint: "GET /api/stock/location/", Filter: "parent=<id>", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary stock-location read permission; no elevated scope", Blocker: "ChildLocationIDs"},
	{Name: "part_default_locations", Endpoint: "GET /api/part/", Filter: "default_location=<id>", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary part read permission; no elevated scope", Blocker: "PartIDs"},
	{Name: "category_default_locations", Endpoint: "GET /api/part/category/", Filter: "scan all pages and retain DefaultLocation == <id> locally; API 530 has no verified exact filter", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary category read permission; no elevated scope", Blocker: "CategoryIDs"},
	{Name: "purchase_order_destinations", Endpoint: "GET /api/order/po/", Filter: "scan all pages and retain Destination == <id> locally; API 530 has no verified exact filter", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary purchasing read permission; no elevated scope", Blocker: "PurchaseOrderIDs"},
	{Name: "purchase_order_line_destinations", Endpoint: "GET /api/order/po-line/", Filter: "scan all pages and retain Destination == <id> locally; API 530 has no verified exact filter", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary purchasing read permission; no elevated scope", Blocker: "PurchaseOrderLineIDs"},
	{Name: "generic_location_parameters", Endpoint: "GET /api/parameter/", Filter: "model_type=stock.stocklocation&model_id=<id>", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary parameter read permission; no elevated scope", Blocker: "ParameterValueIDs"},
	{Name: "build_locations", Endpoint: "GET /api/build/", Filter: "scan all pages and retain TakeFrom or Destination == <id> locally", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary build read permission; no elevated scope", Blocker: "BuildIDs"},
	{Name: "transfer_order_locations", Endpoint: "GET /api/order/transfer-order/", Filter: "scan all pages and retain TakeFrom or Destination == <id> locally", Bound: "bounded 1,000-row scan, 100-row pages, fails closed above the bound", Permission: "ordinary transfer-order read permission; no elevated scope", Blocker: "TransferOrderIDs"},
}
