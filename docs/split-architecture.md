# Frontend and backend split

## Target structure

```text
frontend/  React + TypeScript + Vite
backend/   Go HTTP API, PostgreSQL and external integrations
```

Production stays in one deployable unit initially:

1. Vite builds the browser application into static files.
2. Go builds a single API binary.
3. The Go process serves `/api/v1/*` and the static frontend.

This keeps one Timeweb application while establishing a strict code boundary.
The frontend and API can be deployed independently later without changing the
business layer or public API.

## Boundary rules

- Only Go reads `DATABASE_URL` and integration secrets.
- The frontend never imports backend packages or server environment variables.
- New endpoints are versioned under `/api/v1`.
- Request and response schemas are documented before frontend consumption.
- The current API contract is [`openapi.yaml`](openapi.yaml).
- Business rules are enforced in Go even when the frontend validates them too.
- GitHub Actions sends Saby data to Go with a repository, branch and workflow
  bound OIDC token. Saby credentials stay in GitHub Actions secrets.
- The legacy Next.js/Cloudflare runtime has been removed, so there is one
  authoritative implementation of every active endpoint.

## Migration order

1. Health and catalog.
2. Registration, login, logout and session lookup.
3. Orders and Telegram notifications.
4. CDEK and Saby.
5. Admin and account read models.
6. Verify the split image in a non-production Timeweb deployment.
7. Enable payments only after 54-ФЗ receipts are in place.
8. Remove Next.js server routes and the legacy Node runtime. Completed.

Payments are intentionally disabled in the split runtime and are not a hidden
startup dependency.
