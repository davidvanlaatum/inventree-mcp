# InvenTree API Schema Notes

The local OpenAPI schema is stored at `docs/api-schema.yaml`. Endpoint coverage for implemented and milestone-planned InvenTree client methods is tracked in `docs/endpoint-manifest.yaml`.

Source:

```sh
curl -fsSL https://inventory.internal.vanlaatum.id.au/api/schema/ -o docs/api-schema.yaml
```

Current fetched schema:

- OpenAPI: `3.0.3`
- API title: `InvenTree API`
- API version: `511`
- Runtime InvenTree version: `1.4.0`
- Runtime commit: `0a9a8b1`
- Runtime commit date: `2026-06-24`
- Fetched at: `2026-07-03T23:21:00+09:30` approximately
- Source instance: `inventory.internal.vanlaatum.id.au`
- Authentication used for schema fetch: none from this workspace
- SHA256: `a574d8c055e36e2efa16dfaad093b77b4126f3a230c12a56c31b90f224d526a1`
- Runtime evidence: operator-provided InvenTree About dialog screenshot on `2026-07-04`, confirmed upstream Git tag `1.4.0` resolves to commit `0a9a8b1c54d0811ede0a61ffe3b0427f82ee28e5`.

When `docs/api-schema.yaml` changes, update this provenance block and any endpoint capability tables in the same change.
The manifest stores the same schema SHA256 and API version; schema drift tests fail until both this provenance block and `docs/endpoint-manifest.yaml` are updated. The blocking Testcontainers suite pins `inventree/inventree:1.4.0` and fails if the runtime version or API version differs from this provenance.

## Verified Auth and Token Endpoints

These are InvenTree upstream authentication endpoints and schemes. They are not the MCP server's HTTP OAuth endpoints.

The MCP server's ChatGPT-facing OAuth issuer is separate from these InvenTree endpoints. Do not point ChatGPT directly at InvenTree `/o/authorize/` or `/o/token/` unless the product plan is explicitly changed.

Security schemes:

- `tokenAuth` uses `Authorization: Token <token>`.
- The schema also describes OAuth2 endpoints with `authorizationUrl: /o/authorize/`, `tokenUrl: /o/token/`, and `refreshUrl: /o/revoke_token/`.

Current-user validation endpoints:

- `GET /api/user/me/` retrieves the authenticated user's record and is suitable as a cheap credential validity check.
- `GET /api/user/me/roles/` retrieves the authenticated user's roles and is also suitable as a cheap credential validity check.

Current-user API token endpoints:

- `GET /api/user/me/token/?name=<name>` is schema-confirmed for token issuance/lookup behavior, but implementation must verify whether the response includes a usable secret only at creation time. Do not rely on any InvenTree endpoint to recover an already-created token secret.
- `GET /api/user/tokens/` lists current-user API tokens.
- `POST /api/user/tokens/` creates a current-user API token.
- `GET /api/user/tokens/{id}/` retrieves current-user API token metadata.
- `DELETE /api/user/tokens/{id}/` revokes or deletes a current-user API token.

HTTP MCP connector auth mapping:

- STDIO mode may use configured `Token` or `Bearer` upstream credentials directly.
- HTTP mode should not pass raw InvenTree `Authorization` headers through unchanged. The MCP server should validate its own OAuth access-token envelope, recover the sealed upstream credential, and then call InvenTree using `Authorization: Token ...` or `Authorization: Bearer ...`.

## Verified Attachment Endpoints

- `GET /api/attachment/` lists attachments.
- `POST /api/attachment/` creates attachments and supports `multipart/form-data`.
- `DELETE /api/attachment/` performs bulk delete.
- `GET /api/attachment/{id}/` retrieves attachment metadata.
- `PUT /api/attachment/{id}/` updates attachment data.
- `PATCH /api/attachment/{id}/` partially updates attachment data.
- `DELETE /api/attachment/{id}/` deletes one attachment.

Useful list filters:

- `model_type`
- `model_id`
- `is_file`
- `is_image`
- `is_link`
- `has_thumbnail`
- `tags`
- `upload_user`
- `search`
- `limit`
- `offset`

Attachment fields include:

- `attachment`
- `thumbnail`
- `filename`
- `link`
- `comment`
- `is_image`
- `upload_date`
- `upload_user`
- `file_size`

- `model_type`
- `model_id`
- `tags`

Attachment content download is not a separate schema endpoint in the current schema snapshot. Implement `download_attachment` by resolving metadata first, rejecting out-of-scope attachment `model_type` values, then fetching the schema-exposed `attachment` URL by default or `thumbnail` URL in explicit thumbnail mode through the InvenTree client, scoped to the configured InvenTree base URL and authenticated as the current InvenTree user. Do not treat arbitrary user-provided URLs or stored link targets as downloadable attachment content.

Workflow mapping:

- `upload_attachment` posts a file attachment using the `attachment` field and never accepts HTTP(S) URLs.
- `download_attachment` reads an existing file attachment using the metadata `attachment` field by default, or `thumbnail` field in explicit thumbnail mode, and returns bounded content to the caller.
- `download_part_image` reads a part's primary image by resolving only that part's readable schema-exposed `image` field or the part thumbnail endpoint and returns bounded content to the caller. It does not accept arbitrary URLs and does not require the operator to know a generic attachment ID.
- `upload_attachment_from_url` fetches remote bytes under the server's URL-fetch policy, then posts a file attachment using the `attachment` field.
- `create_link_attachment` stores a URL in the `link` field without fetching the URL.

Attachment model types in the schema include:

- `build`
- `company`
- `manufacturerpart`
- `supplierpart`
- `purchaseorder`
- `returnorder`
- `salesorder`
- `salesordershipment`
- `transferorder`
- `part`
- `stockitem`

Initial implementation should expose only non-sales model types relevant to the current product scope.

## Verified Image Fields

- `Part` exposes readable `image` and write-only `existing_image`.
- `Company` exposes `image`.
- Generic attachments expose `is_image`, `thumbnail`, and `file_size`.
- `/api/part/thumbs/` and `/api/part/thumbs/{id}/` expose part thumbnail listing and update behavior using `PartThumb` and `PartThumbSerializerUpdate`.
- `/api/notes-image-upload/` exposes note image upload behavior, which is separate from inventory object attachments.

Primary-image behavior must be implemented per object type from schema-verified fields rather than assumed generically.
Part primary image download is part-specific for the first release. It should use the part record's readable `image` value or the part thumbnail endpoint, scope the fetch to the configured InvenTree base URL, authenticate as the current InvenTree user, and apply the same maximum-size and redaction controls as attachment downloads. `existing_image` is write-only and is only valid as assignment/update input.
Part primary image assignment must verify whether the current InvenTree version expects `/api/part/{id}/` PATCH, `/api/part/thumbs/{id}/` PATCH, or both. Keep the tool contract as `set_primary_image`, but choose the client endpoint from schema-backed behavior and integration tests.
Notes image upload, generated report attachments, stock test-result attachments, and other app-specific file surfaces are out of first-release scope unless a later task explicitly adds them.

## Attachment and Image Capability Table

| Object type | Generic attachment support | Upload field / storage | Metadata PATCH | Primary image support | Initial scope |
| --- | --- | --- | --- | --- | --- |
| `part` | `/api/attachment/` with `model_type=part`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | `PATCH /api/part/{id}/` with `PatchedPart.image` or `PatchedPart.existing_image` | yes |
| `stockitem` | `/api/attachment/` with `model_type=stockitem`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `company` | `/api/attachment/` with `model_type=company`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | `PATCH /api/company/{id}/` with `PatchedCompany.image` | yes, attachments only; primary image later |
| `manufacturerpart` | `/api/attachment/` with `model_type=manufacturerpart`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `supplierpart` | `/api/attachment/` with `model_type=supplierpart`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `purchaseorder` | `/api/attachment/` with `model_type=purchaseorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `build` | `/api/attachment/` with `model_type=build`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | later |
| `returnorder` | `/api/attachment/` with `model_type=returnorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales/returns deferred |
| `salesorder` | `/api/attachment/` with `model_type=salesorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales deferred |
| `salesordershipment` | `/api/attachment/` with `model_type=salesordershipment`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales deferred |
| `transferorder` | `/api/attachment/` with `model_type=transferorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | later |

Registered attachment/image tools must only expose object types marked in scope, and tests should fail if a tool exposes an object type not listed here.

Bulk attachment delete (`DELETE /api/attachment/`) is schema-confirmed but out of scope for the initial implementation. If exposed later, it needs a separate destructive tool, dry-run listing, object/prefix scoping, and stricter confirmation than single attachment delete.

## Milestone Endpoint Coverage

The initial endpoint manifest covers schema-confirmed paths, methods, operation IDs, selected query filters, request schemas, response schemas, and PATCH support for every milestone client-method dependency in these areas:

- Part and category search/create/update.
- Company search/create/update and role filters.
- Manufacturer part and supplier part link creation.
- Stock location search, stock item search, and stock item creation.
- Parameter values, parameter templates, and category parameter template links.
- Purchase order preview inputs and supplier-part validation dependencies, including direct supplier-part retrieval for stable `supplier_part_id` validation.
- Attachment, link attachment, URL upload, and primary-image update behavior.

Future endpoint-specific client methods must use manifest entries rather than ad hoc path strings. Adding a method without a manifest entry or changing `docs/api-schema.yaml` without updating the manifest/provenance should fail the schema checks.
The manifest is endpoint-level schema coverage, not a complete upload authorization boundary. Attachment `model_type`, accepted file fields, URL sources, and primary-image object rules remain enforced by the attachment/image client and tool tests when those tools are implemented. The manifest records in-scope and deferred attachment model types so those later tests have a machine-readable scope source.

## Verified Parameter Endpoints

- `GET /api/parameter/` lists parameter values.
- `POST /api/parameter/` creates parameter values.
- `PATCH /api/parameter/{id}/` partially updates parameter values.
- `GET /api/parameter/template/` lists parameter templates.
- `POST /api/parameter/template/` creates parameter templates.
- `PATCH /api/parameter/template/{id}/` partially updates parameter templates.
- `GET /api/part/category/parameters/` lists category parameter template links.
- `POST /api/part/category/parameters/` creates category parameter template links.
- `PATCH /api/part/category/parameters/{id}/` partially updates category parameter template links.

Template and category-link creation/update endpoints are schema-verified for reference, but they are not milestone-1 tool scope. `set_part_parameters` must use existing templates and category links unless a later task explicitly adds template administration workflows.

Parameter guidance:

- Search and reuse existing parameter templates before creating new ones.
- Use category parameter links to understand expected parameters for a category.
- Ask the operator when multiple templates match by name, units, choices, checkbox state, or category association.
- Do not create new parameter templates from natural language unless the caller explicitly confirms that a new template is required.
