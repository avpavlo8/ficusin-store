# Ficusin API

Go API is the only component that may connect to PostgreSQL or external
commerce integrations. The web application communicates with it through
versioned HTTP endpoints under `/api/v1`.

## Local commands

```bash
go mod download
go test ./...
go run ./cmd/api
```

Required environment variables:

- `DATABASE_URL`;
- `DATABASE_SSL_CA` for verified public TLS;
- `DATABASE_SSL_VERIFY=false` only as a temporary Timeweb workaround;
- `PORT` (defaults to `8080`);
- `STATIC_DIR` when the API should also serve a built SPA.
- `MIGRATIONS_DIR` to apply ordered `.sql` migrations on startup;
- `AUTH_COOKIE_SECURE` and `AUTH_SESSION_DAYS`;
- `CDEK_CLIENT_ID` / `CDEK_CLIENT_SECRET` for pick-up point delivery;
- `TELEGRAM_BOT_TOKEN` / `TELEGRAM_ORDER_CHAT_ID`.

Every integration key comes from the environment only. An empty value means
"the feature is off", never a crash. The full table with the consequence of
each empty value is in [`../AGENTS.md`](../AGENTS.md).

## Endpoints

The routing table is the single source of truth:
[`internal/httpapi/router.go`](internal/httpapi/router.go). It is deliberately
not duplicated here — a hand-kept copy went stale within a week.

Public schemas are documented in
[`../docs/openapi.yaml`](../docs/openapi.yaml).

Checkout accepts online card payment through YooKassa
(`internal/payment`, webhook `POST /api/v1/payments/yookassa/webhook`). Only a
request to YooKassa marks an order paid; the unsigned webhook is a reason to go
and ask for status, never a source of truth. Without `YOOKASSA_SHOP_ID` and
`YOOKASSA_SECRET_KEY` the card option is hidden from checkout instead of
failing.
