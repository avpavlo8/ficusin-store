# Review and integration of unused legacy cleanup

Reviewed cleanup commit `1b828a3` against its base `4a3f4dd` and integrated it
with feature commit `f7bdc33`. The cleanup contains 24 commits and changes 485
files. The integration keeps the current product editor, manager permissions,
chat keyboard behavior, nonblocking installation prompt and early order
realizations. Retired individual variant APIs and the old Saby exporter remain
removed.

## Findings corrected before merging

- The content worker checked busy price jobs without their execution mode.
  A copied live price job could indefinitely block mock content publishing.
  The worker and admission guards now check the selected mode. Content-only
  saves also use the price guard to avoid creating mutually waiting jobs;
  a blocked synchronization returns an explicit warning after the site content
  has been saved.
- The new price approval/retry HTTP handler and domain methods restricted
  writes to the owner even when the editor exposed them to a manager. They
  now use `products.manage`, with unrelated settings, finance and catalogue
  import/bulk operations remaining separately protected.
- Ozon content updates attempted to preserve a price from the price API even
  for a verified incomplete card that had no price record. Initial-price
  handling is limited to an explicitly uncreated, identity-verified draft.
  Completed cards and responses without an explicit creation state retain
  the existing price-preservation checks. Unknown import outcomes remain
  read-only reconciliation operations.

## Verification

The combined schema installs with 229 migrations. An authentic installation of
the previous feature state applies 214 migrations, then upgrades through the
15 cleanup migrations. Archive/data-preservation regression tests also pass.
Checks use a separate PostgreSQL instance on local port 55440; existing local
and external databases are not changed.

Frontend verification covers the editor and installation flows plus checkout,
account and PDP behavior across desktop, iPhone and Android viewport projects.
The frontend unit suite, TypeScript build, runtime module graph, CSS checks and
format checks pass. The lazy JavaScript budget gains 128 bytes to accommodate
the combined manager and pricing controls, while retaining the existing entry,
CSS and asset limits. The OpenAPI inventory has 241 operations.

Real vendor writes and deployment are outside this review. Migration retirement
guards still require an unresolved legacy queue to be reconciled before a live
deployment; the integration does not bypass them.
