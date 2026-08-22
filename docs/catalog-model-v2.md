# Catalogue model v2

## Identities and ownership

- `products.id` is the internal PRODUCT foreign key; immutable `product_code` is used in `/product/{code}`.
- `product_variants.id` is the internal SKU foreign key; immutable numeric `sku` identifies the sellable unit.
- Cart keys are SKUs. An order snapshots PRODUCT, variant, SKU, label, price and core variant characteristics.
- Saby, Wildberries and Ozon identifiers are external mappings, never catalogue identity.
- Reviews belong to PRODUCT and retain the purchased variant/SKU.

## Attribute schema

An attribute definition owns a stable Latin `code`, data type, PRODUCT/SKU scope, customer/technical audience and optional enum values. Enum definitions must have at least one option. Codes are API contracts: rename the visible name freely, but create a new definition instead of changing the meaning of an existing code.

Categories assign definitions and may mark them required, filterable, visible on PDP, a key characteristic, a badge, ordered in characteristics, or excluded. A child category inherits the nearest ancestor assignment. A local assignment replaces it completely; deleting the local assignment restores inheritance. Global definitions are the lowest-priority fallback.

Technical attributes are available to operations and logistics but cannot be exposed through storefront flags or filters. An excluded assignment cannot simultaneously be required or visible.

PRODUCT values are saved on the product. SKU values are saved per variant. The API validates type, active enum options and effective category scope. An active SKU cannot be saved while a required SKU attribute is absent; an inactive draft may remain incomplete.

## Storefront filters

Filters reference active customer attributes. Numeric attributes use `range`; non-numeric attributes use `select` or `chips`. A filter may apply globally or to one category. Filter codes are stable Latin identifiers.

## Collections and covers

Manual collections store explicit PRODUCT membership. Dynamic collections contain AND-combined PIM rules and are evaluated by PostgreSQL; membership is not duplicated. Rule operators are constrained by attribute type in admin. The storefront consumes membership from `/api/v1/collections` and the `collections` field of catalogue products.

Each collection has `cover_url`. Admin accepts either a URL/path or an uploaded JPEG, PNG or GIF; uploads are normalized through the same image pipeline and object storage as product media. Migration 059 restores the curated bathroom, office, bedroom, low-light, easy-care, pet-safe, tall, compact and rare-watering collections without overwriting an existing custom cover.

## Operational rules

1. Add schema changes only in a new numbered migration; never edit a production-applied migration.
2. Create definitions before category assignments, assignments before values, and filters/collections only after their referenced attributes exist.
3. Archive definitions and sold SKUs instead of deleting historical identity.
4. Validate migrations from 001 on PostgreSQL and with the historical orphan fixture.
5. A release is complete only after production returns health `ok`, a non-empty catalogue, categories/collections, and the exact frontend bundles from current `main`.

## Known trade-offs

- Attribute definition type/scope changes remain technically possible for owners. They should be treated as migrations; changing them after values exist can require data conversion.
- Dynamic collection preview reflects the last saved rule set, not unsaved edits.
- The admin schema editor favors explicit controls over bulk editing. This is safer but slower for very large taxonomies.
