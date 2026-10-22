# E-commerce analytics

The canonical funnel is stored in PostgreSQL. Yandex Metrika is an optional
acquisition and session-replay adapter; it is not the source of truth for
orders or revenue.

## Identity and attribution

- `visitor_id`: random first-party UUID retained by the browser.
- `session_id`: random UUID renewed after 30 minutes of inactivity.
- first and last external touch are captured from UTM parameters/referrer;
  internal referrers never overwrite acquisition.
- no IP address, user agent, phone or email is stored in analytics events.
- `Do Not Track` and Global Privacy Control disable browser analytics.
- the order stores a snapshot of visitor/session and attribution in its own
  row, so raw-event retention cannot erase order attribution.

## Event contract

Browser events are allow-listed and idempotent by `event_id`. The public API
cannot submit trusted revenue events. `order_created` and
`order_item_requested` are emitted only by the Go backend after the order
transaction has committed, using the server-calculated total and order lines.
These events include unpaid orders and are not sales. Historical
`order_item_purchased` events had the same meaning and must not be counted as
paid purchases. Revenue and paid conversion come from confirmed sales and
payment status. Yandex Metrika does not receive a `purchase` ecommerce event
when checkout merely creates an order; the first-party sales ledger remains
the source for paid revenue.

Main funnel:

1. `page_view`
2. `view_item`
3. `add_to_cart`
4. `begin_checkout`
5. `order_created` (trusted)

Diagnostic events include search/result count, filters, item-list selection,
cart removals, checkout steps, shipping/payment choice, payment redirect and
checkout errors.

## Operations

`GET /api/v1/admin/analytics?days=7|30|90` is available to owner and manager
roles. The admin section shows funnel loss, acquisition sources, product
performance, zero-result searches, revenue and abandoned checkouts.

Set the Yandex Metrika counter in the store settings panel. An empty value keeps
the external counter disabled while first-party analytics continues to work.

## Yandex goals configured on 22 October 2026

Counter `111907858` is accessible under the owner's Yandex account
`svarog62rus`. Existing automatic contact goals remain enabled.
Manual exact-match goals cover `view_item_list`, `select_item`, `search`,
`filter`, `view_item`, `add_to_cart`, `remove_from_cart`, `view_cart`,
`begin_checkout`, `add_shipping_info`, `add_payment_info`, `checkout_error`,
and `payment_redirect`. The goal named "Сайт: заказ создан" uses
`add_shipping_info`: the current checkout emits it only after the server has
successfully created the order, not when delivery is merely selected.

`paid_order` is a separate browser goal with RUB revenue, sent from the payment
result page only after its authenticated order response passes the existing
full-payment balance checks. Pending, partial, failed, cancelled, refunded and
preview states never emit it. Carrier-paid delivery is excluded. The order
number and customer contacts are not sent as goal parameters. Repeated polling
and reloads are deduplicated in the same browser (memory/localStorage); clearing
storage or using another browser can produce another conversion. A customer
who never returns to the result page is not tracked by this goal. It is not
an ecommerce `purchase` event or a replacement for the server sales ledger.


## Server-confirmed purchases (Measurement Protocol)

Set `METRIKA_MP_TOKEN` in the server environment and enable Measurement Protocol
for the configured counter. Empty token leaves the worker disabled and preserves
the existing browser conversion. The token is never stored in settings, database,
request logs, public config or frontend bundles.

New checkout attribution carries the actual `_ym_uid` client ID and explicit
analytics permission (DNT/GPC opt out). `RecordOrder` sets server ownership only
when the integration is configured and that client ID is valid. Public ownership
flags are ignored. Orders without a counter cookie keep browser delivery; this
integration does not override privacy preferences or reconstruct blocked visits.
Existing orders are not backfilled, avoiding duplication of old browser events.
Authenticated order detail exposes `metrikaServer`; for those orders the browser
never sends `paid_order`, including after a server token is removed.

Every 30 seconds a worker reads the confirmed payment ledger independently of
buyer navigation. Full settlement, finalized delivery fee and a positive payable
amount are required. Partial payments, holds, cancelled orders, outstanding
attempts and refunds are excluded. Carrier delivery is excluded from revenue.
A durable outbox with a unique order key and atomic claims prevents duplicate
polls and concurrent dispatch. Enqueued settlement is rechecked before claim.

The worker sends a pageview followed by the `paid_order` goal (RUB value) and an
ecommerce purchase (opaque UUID transaction ID, no item breakdown). No order
number, customer contacts or arbitrary attribution strings leave the server.
A pageview is required when the original visit is over 12 hours old. Events use
receipt time; long-delayed payments may appear in a new visit, and acquisition
attribution can differ from the original session. The financial ledger retains
the real payment time and remains authoritative.

Only HTTP 200 with the documented OK body marks delivery sent. Failed connection
setup is retried with backoff; an explicit rejection fails visibly. A timeout,
5xx, unexpected response or interrupted claim becomes `uncertain`: JS goals have
no documented idempotency key, so automatic replay could duplicate revenue.
Review these cases in Metrika before any manual recovery. Do not reset uncertain
rows blindly. Later refunds are not exported as ecommerce refunds in this change.

`GET /api/v1/admin/analytics/metrika` exposes enabled state and outbox counts to
users with analytics.read. Operations health warns on failed/uncertain delivery
or an outbox older than 15 minutes. Disabled delivery is not an application fault.

Protocol references:
- https://yandex.ru/dev/metrika/ru/data-import/measurement-about
- https://yandex.ru/dev/metrika/ru/data-import/measurement-upload
