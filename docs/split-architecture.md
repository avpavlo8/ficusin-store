# Frontend and backend split

## Current structure

```text
frontend/  React + TypeScript + Vite
backend/   Go HTTP API, PostgreSQL and external integrations
```

Production uses one deployable Docker image:

1. Vite builds the browser application into static files.
2. Go builds a single API binary.
3. The Go process serves `/api/v1/*` and the static frontend.

This keeps one Timeweb application while establishing a strict code boundary.
The frontend and API can be deployed independently later without changing the
business layer or public API.

The canonical Vite configuration is `frontend/vite.config.ts`. The dev, build
and preview scripts select it explicitly. TypeScript checks the Node config
project with `noEmit`; it does not generate sibling `.js`/`.d.ts` copies that
could shadow newer TypeScript settings during local development.

Run `npm test` in `frontend` to execute every `tests/*.test.mjs` file, including
the CSS/module guard regression fixtures. CI uses the same discovery command;
new test files do not require a separate workflow list. `npm run lint` enforces
the actual runtime module graph and CSS usage once, without rerunning those
fixtures. Browser tests remain the separate `e2e` package.

The Docker context excludes root `docs/`, `scripts/`, `e2e/` and
`frontend/tests/`: CI runs those tools from its checkout, and the image build
does not import them. This also excludes local browser reports and staging
session files from the context. Keep `frontend/scripts/` and backend Go test
files: the latter participate in the documented backend build-version hash.

## Boundary rules

- Go reads `DATABASE_URL` and application integration secrets. Operational
  workflows use their own configured secrets; the browser receives neither.
- The frontend never imports backend packages or server environment variables.
- New endpoints are versioned under `/api/v1`.
- Request and response schemas are documented before frontend consumption.
- The current API contract is [`openapi.yaml`](openapi.yaml).
- Business rules are enforced in Go even when the frontend validates them too.
- Scheduled Saby synchronization runs in the Go application. The manual GitHub
  Actions fallback can send Saby data to Go with a repository, branch and
  workflow bound OIDC token. Both paths keep credentials outside the browser.
- The legacy Next.js/Cloudflare runtime has been removed, so there is one
  authoritative implementation of every active endpoint.

## Integrations and verification

The split is complete. Catalog, phone-call authentication, orders, account,
administration, CDEK and Saby use the Go API. Card payments and fiscal receipts
are implemented through YooKassa; availability depends on the configured keys
and payment setting. Empty payment credentials do not prevent startup.

The Docker image, clean database migrations and commerce flows are verified by
CI. Isolated staging verifies the exact candidate before production. Current
payment behavior is documented in [commerce-architecture.md](commerce-architecture.md)
and [commerce-audit-remediation.md](commerce-audit-remediation.md); staging gates
are described in [staging.md](staging.md).
