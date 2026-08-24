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
`order_item_purchased` are emitted only by the Go backend after the order
transaction has committed, using the server-calculated total and order lines.

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
