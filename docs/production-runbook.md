# Production release and incident runbook

Owner: Platform
Last reviewed: 2026-09-07

## Release gate

1. Merge only a reviewed pull request whose required Verify and CodeQL checks
   are green. Never push directly to `main`.
2. Record the expected commit SHA, frontend asset hashes and backend build
   version. The production smoke workflow must observe that exact release.
3. Verify `/api/v1/health`, `/api/v1/ready`, catalogue read, cart read/write with
   a disposable guest session and the browser smoke suite.
4. Stop the rollout when 5xx, checkout failures, latency or worker errors exceed
   the current baseline. Do not wait for a customer report.

## Rollback

- Roll back to the last known-good immutable image/revision in Timeweb.
- Database migrations use expand/contract rules. A release must remain readable
  by the previous application version; destructive cleanup is a later release.
- Never rename an applied migration. Never attempt an ad-hoc reverse migration
  while handling an incident unless a rehearsed procedure explicitly requires it.

## Severity 1: checkout, payment, stock or data integrity

1. Assign incident commander and communications owner; start a timestamped log.
2. Preserve request IDs, deploy SHA, provider IDs and relevant structured logs.
3. If integrity is uncertain, disable the affected delivery/payment integration
   through store settings while keeping safe order intake available where the
   documented business rule permits it.
4. Reconcile orders, payments, reservations, receipts, outbox and shipment jobs;
   never infer success from a browser redirect or webhook body alone.
5. Restore or roll back, run the synthetic flow, then monitor for at least one
   full worker retry interval.

## Required alert families

- availability and p95/p99 latency for health, ready, catalogue, cart and orders;
- order-create error ratio and zero-order anomaly during normal trading hours;
- pending/partially-paid payments, receipt failures and provider mismatch;
- stale Saby catalogue watermark and stock/price reconciliation failures;
- growing outbox, notification, shipment, procurement and marketplace retry queues;
- PostgreSQL pool exhaustion, slow queries, migration duration and storage pressure.

## Evidence and retention

Retain release metadata, failed browser artifacts, incident timelines and audit
logs for at least the period approved by the business and security owners. Run a
documented restore drill monthly and record measured RPO/RTO.
