# Ficusin API

Go API is the only component that may connect to PostgreSQL or external
commerce integrations. The web application communicates with it through
versioned HTTP endpoints under `/api/v1`.

## Local commands

```bash
go mod download
# macOS example; on Linux use an installed TTF.
export PROCUREMENT_PDF_FONT=/System/Library/Fonts/Supplemental/Arial.ttf
go test ./...
APP_ENV=development DATABASE_URL='postgresql://...' \
MIGRATIONS_DIR='../timeweb/migrations' AUTH_COOKIE_SECURE=false go run ./cmd/api
```

The PDF font is needed for local PDF generation and its Go tests. The Docker
image supplies DejaVu at `/app/fonts/DejaVuSans.ttf`; its default works without
the macOS path above.

Environment:

- `DATABASE_URL`;
- `DATABASE_SSL_CA` accepts the provider's inline PEM when an additional trust
  root is needed; TLS verification stays enabled by default;
- `PORT` (defaults to `8080`);
- `STATIC_DIR` when the API should also serve a built SPA.
- `MIGRATIONS_DIR` to apply ordered `.sql` migrations on startup;
- `AUTH_COOKIE_SECURE` and `AUTH_SESSION_DAYS`;
- `CDEK_CLIENT_ID` / `CDEK_CLIENT_SECRET` for pick-up point delivery;
- `TELEGRAM_BOT_TOKEN` / `TELEGRAM_ORDER_CHAT_ID` are required for production
  startup; development/staging use the disabled sender.

Every integration key comes from the environment only. Most optional empty keys
disable their feature. Phone verification without `SMSRU_API_KEY` uses the
development stub, so production needs the real key. Exact startup requirements
and the consequence of missing keys are in [`../AGENTS.md`](../AGENTS.md).

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
