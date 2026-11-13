# Isolated staging realization worker smoke — prepared, not run

This branch is separate from `codex/ops-stage-order-sale-audit-20261113`.
The audit branch still owns the separately reviewed environment setter. This
smoke branch cannot modify Timeweb app settings or deploy code.

Prerequisites: parent has deployed and verified the exact candidate SHA to
app **280757**, branch **staging**; APP_ENV=staging; isolated database
`ficusin_staging_20261031` with its nonprivileged staging role; file/catalog
publication modes mock; ORDER_REALIZATION_MODE=mock,
ORDER_REALIZATION_LIVE_ENABLED=false, ORDER_REALIZATION_SITE_WAREHOUSE_ID=1;
actual database control enabled/mock/warehouse1. The exact SHA must also be
the checked-out source so the runner imports deployed `ordersale.Enqueue`.

Only after parent review, push THIS feature branch (never main/staging), then:

```
gh workflow run 339870867 --ref codex/ops-stage-order-sale-smoke-20261113 \
  -f source_sha=EXACT_DEPLOYED_40_HEX_SHA -f run_smoke=false
```

The default only reads app metadata and the isolated database. After that
passes, explicitly run the same command with `-f run_smoke=true`. Do not run
until the final deployment is active; no deployment or env change is included.

The opted-in phase inserts one random-marker hidden product, inactive variant,
and synthetic paid/completed pickup order with no customer, contact, stock,
media, payment event or notification recipient. It calls core Go Enqueue in
that same transaction. It does not call the provider directly and does not
insert a fabricated success receipt. The actual running STG worker must reach
posted/mock, stock_verified=true, a Test=true proof, STG-SALE-{intentID}, and
matching own order/item/variant, quantity1 and exact 1234.56 price/sum.

Cleanup locks only its captured publication, waits for processing to end,
locks the exact random marker/ownership chain, rejects changed child rows, and deletes only its captured
IDs in FK-safe order. It never resets sequences. Existing customers, orders,
products, warehouses and balances are not modified. A stuck processing lease
is retained rather than bypassed; the masked report gives only this run's
technical IDs for bounded manual cleanup. Never delete by a broad marker
prefix or ID range. A process kill/host failure can likewise require cleanup
of only explicitly captured IDs; cleanup cannot be guaranteed after a kill.

Raw subprocess, SQL, HTTP and compiler errors are captured and never echoed.
Only bounded technical reports are printed; no DSN, app env map, cloud token,
contacts, addresses or integration credentials are logged. The Go child
receives no Timeweb token or marketplace credentials and contains no HTTP
client. Only fixed Timeweb GET and guarded isolated DB access are used.

Checks performed locally: 16 Python guard/masking tests; the three Go runner
unit tests and go vet against the hydrated final source; py_compile and
`git diff --check`. No real smoke, environment apply or staging push executed.
