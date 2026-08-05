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
- `TELEGRAM_ORDER_CHAT_ID`.

## Current endpoints

- `GET /api/v1/health`;
- `GET /api/v1/catalog`;
- `POST /api/v1/auth/register`;
- `POST /api/v1/auth/login`;
- `POST /api/v1/auth/logout`;
- `GET /api/v1/auth/me`;
- `GET /api/v1/account/orders`;
- `GET|POST /api/v1/delivery/cdek`;
- `POST /api/v1/orders`;
- `GET /api/v1/admin/dashboard`;
- `POST /api/v1/integrations/saby/catalog` (GitHub Actions OIDC).

Online payment is intentionally not enabled in the split runtime yet. Checkout
saves the order with a pending payment-provider status and sends the new-order
notification to Telegram.

The schemas are documented in [`../docs/openapi.yaml`](../docs/openapi.yaml).
