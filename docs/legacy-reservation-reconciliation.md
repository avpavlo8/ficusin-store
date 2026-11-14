The reservation cutover command plans by default. It never calls a payment,
carrier or Saby client. Payment attempts without a provider ID need manual
lookup; the command cannot turn an unknown creation into a safe failure.

Run `reconcile-legacy-reservations --expected-database <name>` with `DATABASE_URL`.
Standard output contains aggregate counts. `--report <new-local-path>` writes
group IDs and fingerprints to a new file with mode 0600. No raw customer data,
request bodies or provider identifiers are exported. A limited report explicitly
sets `complete=false`; it cannot certify a complete cutover.

`--verify-payments` explicitly enables serial, bounded provider GET observations
using the existing YooKassa environment credentials. The observer interface has
only `FetchPayment`; it cannot create, capture, cancel, refund or issue receipts.
Unknown creation without a provider ID stays manual. Observations report identity
and amount mismatches, pending payments, authorizations and paid balances; they
do not update financial rows or authorize reservation release. Combining this
flag with `--apply` is rejected. Without this flag no credentials are loaded and
no provider is contacted.

`--apply --variant-id <id> --expected-fingerprint <hash>` is currently a local
pilot restricted in the domain to loopback `ficusin_legacy_reconcile_test`.
It cannot mutate a production or staging database. There is no force option.
Apply repeats the snapshot under locks and accepts only complete, exactly
balanced single-variant ownership by cancelled orders. Active orders, uncertain
payments, unresolved carrier/realization cancellation, previous release receipts,
multi-variant owners and shipment-offer allocations remain blocked for review.
No allocation across unrelated orders or warehouses is guessed.
Even locally cancelled payment rows require separate verified reconciliation;
the pilot accepts only groups without payment attempts.

An accepted transaction preserves before/after snapshots in immutable
`legacy_reservation_reconciliations`, records a per-order `local_reconciled`
release movement, clears the matched inventory and owner order-item reservation
quantities, and sets the old order release guard. Original reservation quantities
and item identities remain in the immutable before snapshot and movement; the
after snapshot records their zero reservations. Physical stock, ordered item
quantities, payment state and the FIFO realization ledger remain unchanged. Repeat uses the
same operation key; a lost commit reply must be resolved by that receipt, without
an automatic write retry.

The runtime `ReleaseStock`, reservation journal producer, edit release and
shipment-offer reservation cleanup are retired together with the explicit
`20261114_zzzzz_legacy_reservation_cutover.sql` barrier. This migration never
resets quantities: any nonzero inventory/order-item/offer reservation or any
allocation row aborts the transaction. It archives every original release flag,
timestamp and reservation identity, including NULLs and zeros, in detached
`legacy_archive.reservation_cutover_*` tables. Current source values and physical
stock stay unchanged. Validated zero constraints reject old producers; the old
order release flag cannot be changed by a cached obsolete writer.

Zero compatibility columns remain for current inventory readers, without a
reservation producer or runtime releaser. Current shipment offers, payment
statuses, checkout locks and the realization/FIFO lifecycle remain active. The
pilot is a pre-cutover tool for historical disposable copies, not an automatic
worker or a production cutover certificate. Unknown payment/carrier/document
outcomes still require separate read-only verification; this release sends no
financial, fiscal or stock operation as part of cleanup.
