# Product editor: marketplace publication

The editor stores common name, brand, annotation and TN VED once per variant, with a separate seller article per marketplace. Model name and units per item are Ozon-specific. Each marketplace has its own category/type, enabled flag, create/link mode and typed category attributes. Category schemas come from the vendor APIs; dictionary values retain vendor IDs, numeric values remain numbers, and multi-value attributes retain their values. Brand/TN VED work for WB without an Ozon category. Required attributes are validated on the server against a fresh category schema and cached dictionary selections.

Creation and editing of site variants use the same category-scoped attribute validation, including summer and winter packaging. `GET/PUT /api/v1/admin/products/{id}/editor-plan` persists channel settings. Incomplete drafts use `submit:false`. `submit:true` freezes category schemas, current prices, the active packaging season/dimensions/weight, resolved image URLs and stable generated EAN-13 into a durable fingerprinted publication request. Uploaded/mirrored photos resolve through `media_mirror`; videos are not sent as images. Live publication rejects local/private/non-HTTPS photo URLs.

## Modes and local testing

`CATALOG_PUBLICATION_MODE` defaults to `disabled`. Saving creates preparation records but does not call seller write APIs.

`mock` runs the real HTTP adapters through an in-memory vendor emulator. Its transport rejects all external hosts and never makes a network connection. The UI clearly labels local API checks and fixture categories. Emulator cards survive backend restarts in `catalog_editor_mock_state`. Mock dictionaries are namespaced separately (`mock/*`), and verified mock IDs are stored separately from canonical production integration IDs. The local preview launcher explicitly selects this mode and sets `CATALOG_PUBLICATION_LIVE_ENABLED=false`.

`live` requires **both** `APP_ENV=production` and `CATALOG_PUBLICATION_LIVE_ENABLED=true`, as well as the mode value itself. Development and staging refuse live publication. No real creation or deployment was performed during implementation. Live credentials, account permissions and category-specific acceptance remain to be verified before any authorised real run.

Metadata uses `CATALOG_OZON_CLIENT_ID`, `CATALOG_OZON_API_KEY`, `CATALOG_WB_API_TOKEN` (standard integration variables remain fallbacks). Production write execution uses the same configured seller account. No keys are stored in drafts or sent to the frontend.

## Vendor protocols

- Ozon: build `/v3/product/import` with offer ID, category/type, attribute/dictionary IDs, VAT, price, ordered photos, millimetres and grams; persist `task_id`; poll `/v1/product/import/info`; surface item errors even on HTTP 200; read back `/v3/product/info/list`, external IDs, photo availability and remote status.
- WB: build `/content/v2/cards/upload` with subject, article, characteristics, barcode, centimetres and kilograms; reconcile asynchronous creation through the exact article, checking paginated `/content/v2/cards/error/list` batches; attach the complete photo set through `/content/v3/media/save`; submit the price and read it back before reporting completion. Check that the photo set appeared on the card.
- A later content save for a card already created by this editor updates its verified ID. WB updates preserve existing sizes, barcodes, documents and unmanaged characteristics. Repeated unchanged saves retain the receipt and do not recreate cards.
- Link mode only reads the seller catalogue, accepts exact WB nmID/vendorCode or Ozon seller offer ID (`sku:...` and `id:...` are explicit alternatives), and verifies ownership of the local link. The first binding is read-only and does not require a creation checkbox. On subsequent saves, established editor or canonical catalogue links always receive updates, regardless of creation checkboxes or link/create mode. The UI offers a read-only “Проверить товар” action before saving.

The bottom checkboxes only select new destinations and are hidden for established links. Site/Saby creation badges only appear for a new site product. The explanatory photo/packaging hint has been removed. WB pot diameter is mapped from the selected variant’s canonical `pot_diameter_cm`, not entered a second time.

Publication leases, attempts and progress are persisted. The latest edit is queued in a separate pending snapshot while an earlier operation is in flight, and promoted once the receipt is resolved. Lost create responses and expired create leases are reconciled by exact article plus barcode, never blindly resent. An unresolved creation becomes `needs_attention`. Confirmed validation failures can be corrected and saved; `POST /admin/products/{id}/editor-publications/{channel}/retry` resumes failed work without losing receipts. External links are unique per marketplace and mode. Publication success means a card exists; it does not assert moderation approval, required documents, sale eligibility or stock availability.

Saby remains a separate path: its category/folder live on Product, existing nomenclature links by exact cached code, and its prior isolated local/staging simulation stays unchanged. New editor plans do not enable real Saby creation.

## Verification

Run frontend channel tests and lint/type-check/build; Go tests cover payloads, units, dictionaries, HTTP 200 item failures, current WB error batches, lost responses, duplicate protection and live-mode gates. `TestEditorPublicationLocalDatabase` optionally creates a fully migrated `ficusin_local_publisher_test` database fixture and verifies save → publish → readback → unchanged save → update → conflicting-link rejection, with no mock IDs leaking into canonical integrations. Browser checks use a disposable local site card and the mock transport, never a real seller card.

Vendor references: [Ozon import flow](https://dev.ozon.ru/start/294-Avtomaticheskii-import-i-obnovlenie-tovarov-v-Seller-API), [WB product management](https://dev.wildberries.kg/docs/openapi/item-management).


`GET /admin/products/{id}/marketplace-cards?variantId=...` reads existing cards for established links, including WB links stored only as seller `vendor_code`. It resolves the selected categories and loads article, brand, TN VED, model, units and typed category attributes. Per-channel load errors are returned independently with a retry action. The editor fills missing values while preserving populated local draft fields, selected categories and edited site names/descriptions. General site photos and calculated prices remain authoritative. Established channels show an editable «Артикул» with a green check instead of create/link controls; a free replacement article shows NEW; their creation checkboxes are hidden. The local emulator seeds existing-card reads from the copied catalogue and remains explicitly labelled as a test source.


Each channel now stores `article` separately. Omitting it keeps legacy shared-article compatibility; an explicitly empty article never falls back. Existing-card imports initialize channel articles from their actual remote identities, not the old shared draft article. Explicit changes set `replaceLink`; the worker resolves the exact new seller article before any write. An existing target is hydrated and updated; a free target gets a new card and a fresh barcode, leaving the previous card intact. A target owned by another local variant fails before updates and retains the previous link. Same-article saves keep the receipt. Link replacements retain history in `catalog_editor_link_history`; live canonical links retire previous identities to legacy and update the procurement mirror. Read-only exact-article preview uses optional `channel` and `article` query parameters on the marketplace-cards endpoint. New links apply on Save, never on typing.


Exact article selection now replaces the editor's common name/description, brand and dictionary IDs, TN VED and dictionary ID, plus the selected channel's category and entire attribute set. Ozon selection also replaces model and units. Missing remote values clear old values; imported empty descriptions do not restore old site text. Site photos, packaging and calculated prices keep their existing source. A changed article response is applied to the latest draft only if its offer still matches. Returning to the original article reloads its content as well. The green check and NEW status sit outside the input on its right.
