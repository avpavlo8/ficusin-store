# Product editor: marketplace publication

The editor stores common name, brand, annotation and TN VED once per variant, with a separate seller article per marketplace. Model name and units per item are Ozon-specific. Each marketplace has its own category/type, enabled flag, create/link mode and typed category attributes. Category schemas come from the vendor APIs; dictionary values retain vendor IDs, numeric values remain numbers, and multi-value attributes retain their values. Brand/TN VED work for WB without an Ozon category. Required attributes are validated on the server against a fresh category schema and cached dictionary selections.

Creation and editing of site variants use the same category-scoped attribute validation, including summer and winter packaging. `GET/PUT /api/v1/admin/products/{id}/editor-plan` persists channel settings. Incomplete drafts use `submit:false`. `submit:true` freezes category schemas, current prices, the active packaging season/dimensions/weight, resolved image URLs and stable generated EAN-13 into a durable fingerprinted publication request. Uploaded/mirrored photos resolve through `media_mirror`; videos are not sent as images. Live publication rejects local/private/non-HTTPS photo URLs.

## Modes and local testing

`CATALOG_PUBLICATION_MODE` defaults to `disabled`. Saving creates preparation records but does not call seller write APIs.

Publication requests retain the mode of the explicit Save/Create action. Workers consume only their exact mode. Enabling `live` never promotes an old `disabled` or `mock` preparation; the operator must explicitly save that card again. Unresolved write receipts cannot be reset by switching modes.

`mock` runs the real HTTP adapters through an in-memory vendor emulator. Its transport rejects all external hosts and never makes a network connection. The UI clearly labels local API checks and fixture categories. Emulator cards survive backend restarts in `catalog_editor_mock_state`. Mock dictionaries are namespaced separately (`mock/*`), and verified mock IDs are stored separately from canonical production integration IDs. The local preview launcher explicitly selects this mode and sets `CATALOG_PUBLICATION_LIVE_ENABLED=false`.

`live` requires **both** `APP_ENV=production` and `CATALOG_PUBLICATION_LIVE_ENABLED=true`, as well as the mode value itself. Development and staging refuse live publication. Production inspection on 2026-11-13 found live receipts for Ozon (`linked`), Saby (`needs_attention`) and WB (`failed`). This proves the live worker ran, not that all channels passed acceptance. Review failed/ambiguous receipts through the existing recovery process; never discard them during cleanup.

Metadata uses `CATALOG_OZON_CLIENT_ID`, `CATALOG_OZON_API_KEY`, `CATALOG_WB_API_TOKEN` (standard integration variables remain fallbacks). Production write execution uses the same configured seller account. No keys are stored in drafts or sent to the frontend.

## Vendor protocols

The article picker keeps `linkedArticle` as its local baseline. `marketplaceOf`
removes that UI field from both channels before strict editor-plan decoding;
the selected `article` and explicit `replaceLink` intent remain in the request.

- Ozon: build `/v3/product/import` with offer ID, category/type, attribute/dictionary IDs, VAT, price, ordered photos, millimetres and grams; persist `task_id`; poll `/v1/product/import/info`; surface item errors even on HTTP 200; read back `/v3/product/info/list`, external IDs, photo availability and remote status.
- WB: build `/content/v2/cards/upload` with subject, article, characteristics, barcode, centimetres and kilograms; reconcile asynchronous creation through the exact article, checking paginated `/content/v2/cards/error/list` batches; attach the complete photo set through `/content/v3/media/save`; submit the price and read it back before reporting completion. Check that the photo set appeared on the card.
- A later content save for a card already created by this editor updates its verified ID. WB updates preserve existing sizes, barcodes, documents and unmanaged characteristics. Repeated unchanged saves retain the receipt and do not recreate cards.
- Link mode only reads the seller catalogue, accepts exact WB nmID/vendorCode or Ozon seller offer ID (`sku:...` and `id:...` are explicit alternatives), and verifies ownership of the local link. The first binding is read-only and does not require a creation checkbox. On subsequent saves, established editor or canonical catalogue links always receive updates, regardless of creation checkboxes or link/create mode. The UI offers a read-only “Проверить товар” action before saving.

The bottom checkboxes only select new destinations and are hidden for established links. Site/Saby creation badges only appear for a new site product. The explanatory photo/packaging hint has been removed. WB pot diameter is mapped from the selected variant’s canonical `pot_diameter_cm`, not entered a second time.

Publication leases, attempts and progress are persisted. The latest edit is queued in a separate pending snapshot while an earlier operation is in flight, and promoted once the receipt is resolved. Lost create responses and expired create leases are reconciled by exact article plus barcode, never blindly resent. An unresolved creation becomes `needs_attention`. Confirmed validation failures can be corrected and saved; `POST /admin/products/{id}/editor-publications/{channel}/retry` resumes failed work without losing receipts. External links are unique per marketplace and mode. Publication success means a card exists; it does not assert moderation approval, required documents, sale eligibility or stock availability.

## Existing-card prices

Existing-card content saves preserve seller prices: WB skips the price stages,
Ozon re-reads and retains its current price/old_price tuple in the content import,
and Saby omits PriceData from content Update. Initial card creation still sets
its initial price. Subsequent price changes are separate immutable draft commands
in the same editor publication process, explicitly approved by an owner or manager with `products.manage`.
WB keeps base price/integer discount, Ozon keeps exact kopecks and old_price, and
Avito uses its direct price API with fresh-stock/minimum-price checks. Card receipts
and price receipts remain separate; price-only actions never create or rebind cards.
See [editor-price-publications.md](editor-price-publications.md) for the UI, mode
gates, target serialization, recovery and migration from retired manual proposals.

## Native Saby publication

An explicit Save/Create in enabled production queues Saby through the same durable publication worker. Disabled preparations remain disabled, while local/staging Saby simulation remains isolated. The retired CommerceML exporter and its HTTP endpoint have been removed; all editor catalogue writes use the native publisher.

The frozen Saby request includes the selected native category/folder, site name with a pot-size suffix only when applicable, description, current site price, public photos, canonical attributes and a stable generated 200-prefixed EAN-13. Saby's numeric integration ID, native business number, UUID and EAN remain separate identifiers. Existing numeric links always mean Update, including when a recovered browser draft still says `mode:create`. Unchanged saves retain their finished receipt.

Native Create creates a server draft. The worker records the irreversible boundary before POST, then durably saves the returned draft ID before later updates or media work. An unknown Create response without a recoverable ID requires manual review; neither automatic retry nor a changed editor mode/name may create it again. Verified completion alone updates canonical Saby IDs and activates the Saby stock source. Binding is not a stock confirmation: a real inventory exchange must still provide fresh quantities.

Native Save prepares its exact managed payload, expected image IDs and verification evidence in a separate durable step before sending Update. If a process dies during Update, its expired lease resumes verification, not another POST. An interrupted Move likewise reads its result back first. An interrupted temporary image upload pauses for manual review because its unknown attachment ID cannot be safely recreated. Incomplete older Save evidence also pauses rather than reporting an unverified success.

The retirement migration refuses unfinished legacy exports under an exclusive lock and archives terminal history. Historical export identities remain protected; there is no second catalogue writer. Existing unlinked nomenclature is selected by an exact unique cached business code; names never establish a link.

## Verification

Run frontend channel tests and lint/type-check/build; Go tests cover payloads, units, dictionaries, HTTP 200 item failures, current WB error batches, lost responses, duplicate protection and live-mode gates. `TestEditorPublicationLocalDatabase` optionally creates a fully migrated `ficusin_local_publisher_test` database fixture and verifies save → publish → readback → unchanged save → update → conflicting-link rejection, with no mock IDs leaking into canonical integrations. Browser checks use a disposable local site card and the mock transport, never a real seller card.

Vendor references: [Ozon import flow](https://dev.ozon.ru/start/294-Avtomaticheskii-import-i-obnovlenie-tovarov-v-Seller-API), [WB product management](https://dev.wildberries.kg/docs/openapi/item-management).


`GET /admin/products/{id}/marketplace-cards?variantId=...` reads existing cards for established links, including WB links stored only as seller `vendor_code`. It resolves the selected categories and loads article, brand, TN VED, model, units and typed category attributes. Per-channel load errors are returned independently with a retry action. The editor fills missing values while preserving populated local draft fields, selected categories and edited site names/descriptions. General site photos and calculated prices remain authoritative. Established channels show an editable «Артикул» with a green check instead of create/link controls; a free replacement article shows NEW; their creation checkboxes are hidden. The local emulator seeds existing-card reads from the copied catalogue and remains explicitly labelled as a test source.


Each channel now stores `article` separately. Omitting it keeps legacy shared-article compatibility; an explicitly empty article never falls back. Existing-card imports initialize channel articles from their actual remote identities, not the old shared draft article. Explicit changes set `replaceLink`; the worker resolves the exact new seller article before any write. An existing target is hydrated and updated; a free target gets a new card and a fresh barcode, leaving the previous card intact. A target owned by another local variant fails before updates and retains the previous link. Same-article saves keep the receipt. Link replacements retain history in `catalog_editor_link_history`; live canonical links retire previous identities to legacy and update the procurement mirror. Read-only exact-article preview uses optional `channel` and `article` query parameters on the marketplace-cards endpoint. New links apply on Save, never on typing.


Exact article selection now replaces the editor's common name/description, brand and dictionary IDs, TN VED and dictionary ID, plus the selected channel's category and entire attribute set. Ozon selection also replaces model and units. Missing remote values clear old values; imported empty descriptions do not restore old site text. Site photos, packaging and calculated prices keep their existing source. A changed article response is applied to the latest draft only if its offer still matches. Returning to the original article reloads its content as well. The green check and NEW status sit outside the input on its right.

## Flowwow

Flowwow uses a separate site-authoritative channel in the same editor plan and
durable publication worker. Its fields/categories are independent of shared
WB/Ozon content. Price and stock changes are additionally handled by the
Flowwow service. See [flowwow.md](flowwow.md) for setup, supported product types,
full-update rules, duplicate protection, database and browser validation, and
remaining order/reservation scope.
