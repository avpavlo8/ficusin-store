# File storage: local implementation and rollout contract

Public product photos keep using `S3_*`. New private files use `PRIVATE_S3_BUCKET`; it deliberately never falls back to the public bucket. Endpoint, region and credentials may inherit the corresponding `S3_*` values, or be set independently. Configuration rejects identical public/private buckets at the same endpoint. Bucket policy must deny anonymous reads; private uploads also carry a signed `x-amz-acl: private` and `Cache-Control: private, no-store`. No keys reach the browser.

`internal/fileassets.Storage` is the shared contract. `Save` writes a random immutable key and reads it back, comparing SHA-256 and length before a database reference may commit. Failed verification or a failure before a database commit attempt cleans only the new uncommitted object. An unknown COMMIT/autocommit outcome retains the object rather than risk deleting committed bytes; later inventory can identify a true orphan. `Read` uses the stored object key if present, without silently reviving stale legacy bytes during storage failure. Without an object key, old bytea remains readable.

Avatar, return/discount photos, procurement invoices, new review media and chat attachments use the private store. Their existing authorized or publication-filtered API reads remain in place; storage keys are internal metadata. Uploads fail clearly with 503 if private storage is unavailable; text-only chat/review operations and legacy bytea reads do not require a bucket. Private storage reads are bounded at 64 MiB and fail rather than truncate larger files.

Existing review object keys originally pointed at the public photo bucket. New `storage_scope` defaults to `legacy-public` for these rows, while new uploads write `private`. `WithLegacyMediaStorage(photoStorage)` preserves reads from the original bucket. An explicit old-key copy migration may be added later; do not reinterpret old keys inside the new bucket.

The retired finance import table is preserved in `legacy_archive.finance_imports`, including its original bytes and object references. It is excluded from the active file migration command.

## Legacy bytea migration

`go -C backend run ./cmd/migrate-file-assets` is **dry-run by default**, using `DATABASE_URL` and the same private S3 configuration. It reports kind, row ID, size and checksum; never file contents or credentials. Optional `--kinds avatars,returns,procurement,reviews,chat` and `--limit 100` bound the inventory.

Only an explicit `--apply` performs copies. Every file is PUT, GET and checksummed before a compare-and-set object reference update. Concurrently replaced rows are skipped. **Source bytea is retained**, including after success; repeated runs skip committed references. Cleanup/retention is a separate task, not a migration side effect. Existing public review object keys stay in the legacy reader and are not part of this bytea-only copier.

No application daemon starts the migration. An operator must first review dry-run output against the intended database and storage policy. This implementation was tested only with fake storage and isolated local database fixtures; it does not mean working data has been migrated.

## Static assets in S3

Build the frontend normally. Prepare a manifest without uploading:

```
go -C backend run ./cmd/publish-static-assets --dir /absolute/path/to/frontend/dist --prefix site/releases/UNIQUE_RELEASE --manifest /tmp/assets-manifest.json
```

The manifest includes relative path, object key, MIME, byte count and SHA-256. `--apply` explicitly uploads and verifies each remote object. It rejects the default preview prefix. Use a unique release prefix and never overwrite a previously published immutable release. HTML, service worker, offline HTML and manifests stay in the same-origin shell and are excluded from asset publication.

After the manifest is fully verified, configure `STATIC_ASSETS_BASE_URL=https://s3.twcstorage.ru/BUCKET/site/releases/UNIQUE_RELEASE`. Do not enable it before the files are ready. Go serves browser asset URLs from this physical S3 source while preserving their same-origin URLs, so existing CSP, service-worker scope and hardcoded `/assets/...` links do not need to be weakened or rewritten. It forwards safe conditional/range headers but **never session cookies, authorization or query strings** to storage. Storage failures fall back to files still present in the application image. Assets also work when no local asset copy exists. `X-Ficusin-Asset-Source: s3` identifies a successfully proxied response. Blank configuration preserves local delivery.

`sw.js`, HTML, web manifests, dynamic SEO/API/sitemap/feed routes remain served by the application. Do not move the service-worker URL to a cross-origin S3 address. Keep old immutable assets for open PWA clients and rollback. Asset filenames without a content hash must be versioned when their content changes, because browser URLs retain immutable caching semantics. No worker cache strategy was changed here.

## Remaining product-media work

The product editor still retains draft blobs in IndexedDB until Save, then uploads photos through the existing public S3 photo endpoints. Product video Save is not yet supported. A dedicated draft upload API, publication/promotion semantics, offline cache, abandonment cleanup and end-to-end video gallery support remain a separate implementation. This document does not claim those browser-only drafts already live in S3.

## Verification

- Core Save/Read: unavailable store, SHA mismatch, rollback cleanup, no stale bytea fallback.
- Local isolated database: migration dry-run, copy/verify/reference commit, preserved bytea, idempotence and checksum-failure rejection.
- Local domain fixtures: avatar metadata-only Save/read and bytea compatibility; return/approved discount read; procurement PDF read; reviews original public bucket/new private bucket/bytea and hidden-review filtering.
- Static proxy: credential/query isolation, same-origin shell exclusions, remote-only asset, local rollback fallback.
- Static manifest: no side effects without `--apply`, unsafe prefix rejection, successful fake-store publication and stale-manifest rejection.

All working S3 bucket creation/policy, existing-data migration and deploy steps remain unperformed.

## Explicit local private-file emulator

For the local preview only, set `LOCAL_PRIVATE_FILES_DIR` to a dedicated absolute directory **outside** the public asset/media directories. Config permits it exclusively with `APP_ENV=development` and `CATALOG_PUBLICATION_MODE=mock`; staging/production reject the flag. With no flag, no fallback is installed: real private S3 remains required for new file uploads.

`fileassets.LocalPrivateStorage` implements the same Put/Get/Delete interface, persisting objects across backend restarts. It is a development emulator, not a transfer to S3. Directories are 0700 and files 0600. Object keys are strictly validated; Go `os.Root` confines filesystem operations, with explicit symlink/traversal rejection and atomic file replacement. It exposes no PublicURL and is not registered in a file server or a static route. Existing authenticated/publication-filtered API reads retain all their permission checks. Never point the public `local-media` server at this directory.

## Explicit staging file provider

A staging deployment can set `FILE_STORAGE_MODE=mock` explicitly to test upload → Save → reload without production S3 credentials or ephemeral container files. The mode is accepted only for `APP_ENV=staging`; the provider additionally checks the actual connected database name against the isolated `ficusin_staging[_...]` namespace. `s3` remains the strict default. Development continues to use the separate optional `LOCAL_PRIVATE_FILES_DIR` emulator. Production rejects mock mode.

`private_file_mock_objects` persists only synthetic staging private objects, MIME and SHA-256. Existing authorized reads for avatars, chat, returns, invoices and reviews use this provider. `public_file_mock_objects` is a **different table** and namespace for product/variant/collection image uploads. It accepts image MIME and `products/` or `collections/` keys only. Its absolute `SITE_URL/api/v1/staging/media/{encodedKey}` URLs survive application deploys and are usable by the test catalogue/marketplace emulator. The image endpoint exists only with the staging provider, remains behind the common environment-access gate, returns no-store test-image responses and never reads the private table. A private object stays inaccessible even if its internal key starts with `products/`.

Both providers avoid outbound S3 entirely. This is a named staging test backend, **not a silent production bytea fallback or a completed S3 migration**. Existing copied production credentials remain disabled by staging isolation. Configure `FILE_STORAGE_MODE=mock` in the staging app before releasing this version if no dedicated staging S3 bucket is configured; do not configure `LOCAL_PRIVATE_FILES_DIR` on staging. Logs explicitly identify the test provider. Uploaded staging test objects remain only in the isolated staging database and must never be copied to production.
