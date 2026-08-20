package inventree

// CategoryDeleteReferenceSurface documents one verified place a PartCategory
// primary key is referenced from, and how delete_part_category's preflight
// blocks on it. The inventory is intentionally exhaustive: every reference
// surface exposed by the pinned InvenTree 1.5.0 / API 530 schema or verified
// live behavior must appear here before it can be treated as safe to ignore.
type CategoryDeleteReferenceSurface struct {
	Name       string
	Endpoint   string
	Filter     string
	Bound      string
	Permission string
	Blocker    string
}

// CategoryDeleteReferenceInventory is the checked-in, pinned map from every
// verified PartCategory reference surface to its scanning endpoint/filter,
// record bound, permission behavior, and the delete_part_category blocker it
// backs. Deleting a category is refused outright -- without ever issuing a
// confirmation plan -- while any of these scans reports a stable ID.
var CategoryDeleteReferenceInventory = []CategoryDeleteReferenceSurface{
	{
		Name:       "direct_parts",
		Endpoint:   "GET /api/part/",
		Filter:     "category=<id>&cascade=false",
		Bound:      "bounded 1,000-row scan, 100-row pages, fails closed above the bound",
		Permission: "ordinary part read permission; no elevated scope",
		Blocker:    "PartIDs",
	},
	{
		Name:       "direct_child_categories",
		Endpoint:   "GET /api/part/category/",
		Filter:     "parent=<id>",
		Bound:      "bounded 1,000-row scan, 100-row pages, fails closed above the bound",
		Permission: "ordinary category read permission; no elevated scope",
		Blocker:    "ChildCategoryIDs",
	},
	{
		Name:       "category_parameter_template_links",
		Endpoint:   "GET /api/part/category/parameters/",
		Filter:     "fetch_parent=false; scan all bounded pages and retain only records with Category == <id> locally because the server-side category filter is unreliable",
		Bound:      "bounded 1,000-row scan, 100-row pages, fails closed above the bound",
		Permission: "ordinary parameter-template read permission; no elevated scope",
		Blocker:    "ParameterTemplateLinkIDs",
	},
	{
		Name:       "generic_parameter_values",
		Endpoint:   "GET /api/parameter/",
		Filter:     "model_type=part.partcategory&model_id=<id>",
		Bound:      "bounded 1,000-row scan, 100-row pages, fails closed above the bound",
		Permission: "ordinary parameter read permission; no elevated scope",
		Blocker:    "ParameterValueIDs",
	},
}

// CategorySchemaForeignKeys pins every OpenAPI component schema known to
// declare an integer "category" property -- i.e. a genuine PartCategory
// foreign key -- as of the pinned InvenTree 1.5.0 / API 530 schema. This
// backs a drift test that fails if a future schema bump adds a new "category"
// foreign key this inventory has not yet classified. Category's own
// self-referential hierarchy field is named "parent", not "category", and is
// covered by the direct_child_categories surface above instead.
// NotificationMessage.category and Icon.category are unrelated string
// classification fields, not PartCategory foreign keys, and are deliberately
// excluded.
//
//   - Part.category is a persisted reference: the direct_parts surface above.
//   - CategoryParameterTemplate.category is a persisted reference: the
//     category_parameter_template_links surface above.
//   - PartStocktakeGenerate.category is a write-only, nullable scoping
//     parameter on the (unimplemented, F-S60 "Future") stocktake-generation
//     action endpoint, not a stored reference on any persisted record; there
//     is nothing for delete_part_category to block on, since no
//     PartStocktakeGenerate row is ever created or retained.
var CategorySchemaForeignKeys = []string{"CategoryParameterTemplate", "Part", "PartStocktakeGenerate"}
