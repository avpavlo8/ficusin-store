# Catalogue model v2

## Identities and ownership

- `products.id` is the internal PRODUCT foreign key; immutable `product_code` is used in `/product/{code}`.
- `product_variants.id` is the internal SKU foreign key; immutable numeric `sku` identifies the sellable unit.
- Cart keys are SKUs. An order snapshots PRODUCT, variant, SKU, label, price and core variant characteristics.
- A manager editing an order resolves each line by SKU as well, and writes the full PRODUCT + variant + SKU tuple. Nothing in the order path resolves a line through `products.slug`: that column is not identity and is not read by application code.
- Saby, Wildberries and Ozon identifiers are external mappings, never catalogue identity.
- Reviews belong to PRODUCT and retain the purchased variant/SKU.

## Attribute schema

An attribute definition owns a stable Latin `code`, data type, PRODUCT/SKU scope, customer/technical audience and optional enum values. Enum definitions must have at least one option. Codes are API contracts: rename the visible name freely, but create a new definition instead of changing the meaning of an existing code.

Categories assign definitions and may mark them required, filterable, visible on PDP, a key characteristic, a badge, ordered in characteristics, or excluded. A child category inherits the nearest ancestor assignment. A local assignment replaces it completely; deleting the local assignment restores inheritance. Global definitions are the lowest-priority fallback.

Technical attributes are available to operations and logistics but cannot be exposed through storefront flags or filters. An excluded assignment cannot simultaneously be required or visible.

Physical dimensions of a SKU — height, pot diameter, parcel length/width/height and weight — live in `variant_attribute_values` and nowhere else. The `product_variants` columns of the same name are deprecated since migration 061 and are read by nothing; they exist only so a rollback of that release still works. Both editing screens write the attribute store, and delivery, the product page and marketplace logistics read it through `variant_numeric_attribute`. These six definitions are global: a parcel size is a property of a physical object, not of a catalogue section, and delivery has to price accessories and soil too.

PRODUCT values are saved on the product. SKU values are saved per variant. The API validates type, active enum options and effective category scope. An active SKU cannot be saved while a required SKU attribute is absent; an inactive draft may remain incomplete.

## Storefront filters

Filters reference active customer attributes. Numeric attributes use `range`; non-numeric attributes use `select` or `chips`. A filter may apply globally or to one category. Filter codes are stable Latin identifiers.

## Collections and covers

Manual collections store explicit PRODUCT membership. Dynamic collections contain AND-combined PIM rules and are evaluated by PostgreSQL; membership is not duplicated. Rule operators are constrained by attribute type in admin. The storefront consumes membership from `/api/v1/collections` and the `collections` field of catalogue products.

Each collection has `cover_url`. Admin accepts either a URL/path or an uploaded JPEG, PNG or GIF; uploads are normalized through the same image pipeline and object storage as product media. Migration 059 restores the curated bathroom, office, bedroom, low-light, easy-care, pet-safe, tall, compact and rare-watering collections without overwriting an existing custom cover.

## Operational rules

1. Add schema changes only in a new numbered migration; never edit a production-applied migration.
2. A migration must stay readable by the **previous** release of the application. Timeweb replaces containers in place: the old binary keeps serving traffic while the new one starts and migrates. Migration 056 dropped and retyped `order_items.product_id` in one step, the previous runtime started answering 503, and migration 058 had to install a global `bigint = text` operator to keep the shop alive. Change a type in two releases: new column, dual write, switch reads, drop the old one next time.
3. Create definitions before category assignments, assignments before values, and filters/collections only after their referenced attributes exist.
4. Archive definitions and sold SKUs instead of deleting historical identity.
5. Validate migrations from 001 on PostgreSQL and with the historical orphan fixture, and run the live-database test that executes the storefront query and a manager order edit against that schema.
6. A release is complete only after production returns health `ok`, a non-empty catalogue, categories/collections, and the exact frontend bundles from current `main`.

## Known trade-offs

- An attribute `code`, data type and value scope are frozen once any value exists: collection rules reference attributes by code, so renaming one used to empty every dynamic collection that pointed at it, silently. Create a new definition instead. Archiving is refused while an active filter or dynamic collection depends on the attribute.
- Dynamic collection preview reflects the last saved rule set, not unsaved edits.
- The admin schema editor favors explicit controls over bulk editing. This is safer but slower for very large taxonomies.

## What the migration did to existing data

These are one-time consequences of moving to catalogue v2. They are not reversible and they are not visible in the data itself, so they belong here rather than in a commit message.

- **Public product addresses changed.** Cards used to live at `/product/{slug}` with a transliterated name; they now live at `/product/{code}` with a plain number. Old links are not redirected. This was accepted deliberately: the shop had no external audience on the old addresses yet.
- **Carts collapsed onto the first variant.** A cart was keyed by product before the migration and by SKU after it, so every saved line was attached to the first variant of its card. A customer who had put a large size aside got the first one, at its price.
- **SKU in old order lines is reconstructed, not recorded.** Historical rows without a variant were attached to the first variant of their product. Money is untouched — `unit_price` is the price that was actually paid — but the size attribution in orders older than 21.08.2026 is a guess. Do not use it for returns or size analytics.
- **Old Ficusin article numbers are gone.** `product_external_ids` rows with `provider = 'ficusin'` were deleted. If a printed label or an old marketplace listing carries one of those numbers, nothing maps it back.

## What the tests do and do not cover

Be honest about this when planning work.

- The Python contract tests and the SQL-string assertions in Go check that certain text appears in certain files. They guard intent, not behaviour: they stay green on invalid SQL. That is how a broken catalogue query reached production.
- Playwright runs against `vite preview` with every `/api/v1/*` answered by a fixture. It proves the React app renders the agreed shape; it proves nothing about the backend.
- `backend/internal/admin/live_database_test.go` is the only place where application queries run against a fully migrated PostgreSQL. It runs in the `migrations` job. Anything new in the catalogue or order path should get an assertion there, because everywhere else a mistake is invisible until a customer finds it.
