# API inventory and contract policy

Owner: Storefront & Commerce
Last reviewed: 2026-09-07

`docs/openapi.yaml` is the typed contract for buyer-facing and highest-risk
operations. It must never describe an endpoint that the router no longer
serves. New or changed request/response shapes must update OpenAPI and contract
tests in the same pull request.

The router remains the exhaustive source for route registration. Its current
surface is grouped below so an endpoint cannot become ownerless merely because
it is not yet represented by a detailed OpenAPI schema.

| Surface | Prefix / routes | Owner | Required controls |
|---|---|---|---|
| Runtime | `/health`, `/ready` | Platform | synthetic monitoring, no auth |
| Catalogue reads | `/catalog`, `/categories`, `/collections`, `/products/*` | Storefront | cached reads, schema tests |
| Cart and checkout | `/cart`, `/orders`, `/payments/*`, `/delivery/*` | Commerce | contract, idempotency and live-DB tests |
| Authentication/account | `/auth/*`, `/account/*` | Identity | session, Origin and authorization tests |
| Reviews/media | `/products/*/reviews`, `/review-photos/*`, `/account/reviews*` | Storefront | authorization and media validation |
| Browser capabilities | `/push/*`, `/address/suggest`, `/analytics/*` | Storefront | rate limits and graceful degradation |
| Administration | `/admin/*` | Backoffice | role matrix and audit logging |
| Procurement | `/admin/procurement/*` | Procurement | idempotency, reconciliation and provider budgets |
| Provider ingestion | `/integrations/saby/*` | Integrations | OIDC authentication, size limits and replay safety |
| Payment callback | `/payments/yookassa/webhook` | Commerce | provider-side status verification |

## Compatibility rules

1. `/api/v1` changes are additive. Removing or renaming a field requires a new
   version or a measured deprecation window.
2. Cart keys and order item IDs are exact immutable Ficusin SKUs, never product
   card IDs or external provider IDs.
3. Errors exposed to customers contain a safe Russian message and the response
   carries `X-Request-ID`; internal provider/SQL details stay in structured logs.
4. Mutations from a browser must pass the same-origin boundary. Server-to-server
   callbacks without an `Origin` header authenticate using their provider
   contract and never trust the callback body as final payment truth.
5. Every route has one accountable owner, a success metric and an incident
   runbook before it becomes financially critical.
