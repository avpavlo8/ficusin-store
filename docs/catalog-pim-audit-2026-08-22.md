# Catalogue/PIM audit — 2026-08-22

## Scope

Reviewed attribute definitions, category inheritance, PRODUCT/SKU values, filters, dynamic collections, collection covers and admin layouts on desktop and phone widths.

## Defects corrected

- SKU inheritance selected a non-direct ancestor nondeterministically. Effective rules now use explicit recursive depth.
- Required SKU attributes were only shown as completeness in the browser. The transaction now rejects an incomplete active SKU, and the UI explains and blocks the save.
- Optional booleans had no “unset” state. They now support unset/yes/no.
- Technical attributes could be flagged for the storefront. Both server and UI now reject/disable that configuration.
- An excluded attribute could remain required and visible. Contradictory assignments are rejected.
- Local category overrides could not be removed in the UI. “Вернуть наследование” now calls the existing DELETE endpoint.
- Numeric filters could be configured as chips and text filters as ranges. Server validation and type-aware controls now keep these consistent.
- Category-scoped filters had an API field but no control. The category selector is now present.
- Collection rule operators were not type-aware. Admin now offers only meaningful operators and resets the value when type/operator changes.
- Collection membership updates could publish stale editor state. The saved state is now assembled once and used by both local state and parent list.
- Curated image collections disappeared when database definitions became authoritative. Migration 059 restores them and adds editable covers.

## Tests and verification

- Go unit cases cover attribute codes/options, filter contracts and dynamic collection requirements.
- Playwright covers collection cover/editor and PIM layouts on Desktop Chrome, iPhone and Android project widths, including horizontal overflow.
- Existing migration gate remains authoritative for 001→latest and historical orphan orders.
- Frontend TypeScript build and ESLint run on every Verify workflow.

## Honest critique

The model is now internally consistent for normal editorial work, but it is not a full enterprise PIM. Structural changes to a populated definition are not backed by an automated conversion wizard; owners need a migration. Collection preview is save-based, so it is accurate but not live while typing. Filter editing remains deliberately minimal (create/delete rather than a dense spreadsheet). These are conscious limitations, not hidden correctness gaps, and should be the next investments only if catalogue volume makes them painful.
