# Disaster recovery and commerce reconciliation

Owner: Platform
Last reviewed: 2026-09-07

## Approved targets

| Measure | Target | Evidence |
|---|---:|---|
| Database RPO | no more than 15 minutes | provider backup/PITR timestamp |
| Safe-service RTO | no more than 30 minutes | incident timeline and external smoke |
| Restore drill | monthly and before changing backup technology | drill log and `restore_verified` output |
| Integrity detection | no more than 5 minutes | Production monitor run |

These are acceptance targets, not a claim that the hosting plan already meets
them. The Platform owner must confirm that Timeweb backup frequency, retention,
encryption and regional failure model satisfy the RPO before marking the control
complete.

## What is reconciled automatically

`GET /api/v1/operations/health` runs read-only invariants and exposes only a
coarse status plus non-sensitive check codes. It returns `503` for an integrity
violation and `200` with `degraded` for delayed asynchronous work. Counts and
ages are available only to an authenticated administrator at
`GET /api/v1/admin/operations`.

The probe checks:

- inventory reservations against active order-line reservations;
- negative or overcommitted inventory;
- duplicate paid attempts and a provider-paid payment not reflected on its order;
- recent stock-reservation journal consistency;
- stale payments, mail outbox entries and exhausted delivery attempts;
- CDEK manual review/retry state;
- failed procurement actions and expired processing leases.

The probe never rewrites money, stock or provider state. An inconsistency may be
evidence of an external operation that succeeded while the response was lost;
blind repair could charge, refund or ship twice. The on-call operator reconciles
provider identifiers and audit records before a corrective transaction.

## Restore drill

1. Choose a recent encrypted production backup and record its creation time.
2. Create an isolated PostgreSQL database whose name starts with
   `ficusin_restore_`. Never point the drill at production or staging.
3. Restore the backup and run:

   ```bash
   bash scripts/verify-backup-restore.sh "$SOURCE_DATABASE_URL" "$RESTORE_DATABASE_URL"
   ```

4. Start the candidate application against the restored database with every
   outbound provider credential removed or replaced by a sandbox credential.
5. Verify health, readiness, operations health, guest cart and one disposable
   pickup/pay-on-delivery checkout. Confirm that no email, message, charge or
   shipment left the isolated environment.
6. Record backup age (measured RPO), time until safe service (measured RTO),
   application commit, row signatures, failures and follow-up owner.
7. Destroy the isolated database after evidence is retained.

CI executes the same dump/restore verifier on every change. That catches schema
and tooling regressions; it does not replace the monthly drill of the hosting
provider's actual backup.

## Pager setup

GitHub issues are the incident record and fallback. Configure the repository
Actions secret `PRODUCTION_ALERT_WEBHOOK_URL` with a Slack-compatible incoming
webhook owned by the on-call team. Send a test through `Production monitor`,
verify delivery outside business hours, and document primary/secondary owners.
The monitor runs every five minutes and deduplicates Sev-1 and degraded-commerce
issues.

## Failure drill matrix

| Injection | Expected safe result |
|---|---|
| YooKassa timeout after create | one pending payment; reconciliation fetches provider truth |
| repeated payment webhook | no duplicate state transition or receipt |
| SMTP unavailable | order commits; outbox retries and becomes degraded after 15 minutes |
| CDEK timeout | no duplicate shipment; durable retry, then manual review |
| process death during procurement | expired lease is detected and safely reclaimable |
| two buyers take last units | reservations serialize; excess demand is preorder, never oversold |
| PostgreSQL unavailable | readiness and monitor fail; no request runs against half-ready schema |

Run at least one provider failure and one database restore scenario each quarter,
in addition to the monthly restore drill.
