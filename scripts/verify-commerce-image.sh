#!/usr/bin/env bash
set -euo pipefail

# Exercise the actual production binary over HTTP while PostgreSQL remains an
# ephemeral CI service. No provider credentials are present and no real money,
# delivery or customer notification can leave the runner.
base_url="${FICUSIN_TEST_BASE_URL:-http://127.0.0.1:3000}"
db_host="${FICUSIN_TEST_DB_HOST:-127.0.0.1}"
db_name="${FICUSIN_TEST_DB_NAME:-ficusin_runtime}"
db_user="${FICUSIN_TEST_DB_USER:-postgres}"
suffix="${GITHUB_RUN_ID:-local}-$$"
slug="ci-commerce-${suffix}"
warehouse_key="ci-commerce-${suffix}"
email="commerce-${suffix}@example.invalid"
work="$(mktemp -d)"
cookie_jar="${work}/cookies"

psql_ci() {
  psql -h "$db_host" -U "$db_user" -d "$db_name" -v ON_ERROR_STOP=1 "$@"
}

product_id=""
warehouse_id=""
order_number=""
cleanup() {
  if [[ -n "$order_number" ]]; then
    order_id="$(psql_ci -Atq -c "SELECT id FROM orders WHERE order_number='${order_number}'" || true)"
    if [[ -n "$order_id" ]]; then
      psql_ci -q -c "DELETE FROM stock_movements WHERE order_id=${order_id}; DELETE FROM consent_events WHERE order_id=${order_id}; DELETE FROM orders WHERE id=${order_id};" || true
    fi
  fi
  psql_ci -q -c "DELETE FROM outbox WHERE recipient='${email}'" || true
  if [[ -n "$product_id" ]]; then psql_ci -q -c "DELETE FROM products WHERE id=${product_id}" || true; fi
  if [[ -n "$warehouse_id" ]]; then psql_ci -q -c "DELETE FROM warehouses WHERE id=${warehouse_id}" || true; fi
  rm -rf "$work"
}
trap cleanup EXIT

product_id="$(psql_ci -Atq -c "
  INSERT INTO products(name,slug,short_description,description,search_text,status,category_id)
  SELECT 'CI commerce product','${slug}','Release test','Release test product',
         'ci commerce product','published',id
  FROM categories WHERE slug='accessories' RETURNING id")"
read -r variant_id sku < <(psql_ci -Atq -F ' ' -c "
  INSERT INTO product_variants(product_id,label,base_price_minor)
  VALUES (${product_id},'CI variant',149000) RETURNING id,sku")
warehouse_id="$(psql_ci -Atq -c "
  INSERT INTO warehouses(saby_id,name,city,address)
  VALUES ('${warehouse_key}','CI warehouse','Рязань','CI only') RETURNING id")"
psql_ci -q -c "
  INSERT INTO inventory(warehouse_id,variant_id,available_qty)
  VALUES (${warehouse_id},${variant_id},5)"

curl --fail --silent --show-error --cookie-jar "$cookie_jar" "$base_url/api/v1/cart" >"${work}/cart-empty.json"
curl --fail --silent --show-error --cookie "$cookie_jar" --cookie-jar "$cookie_jar" \
  -X PUT -H 'Content-Type: application/json' \
  --data "{\"items\":{\"${sku}\":2}}" \
  "$base_url/api/v1/cart" >"${work}/cart.json"
python3 - "$sku" "${work}/cart.json" <<'PY'
import json, pathlib, sys
sku, path = sys.argv[1:]
cart = json.loads(pathlib.Path(path).read_text())
assert cart["items"] == {sku: 2}, cart
assert len(cart["lines"]) == 1, cart
line = cart["lines"][0]
assert line["sku"] == sku and line["price"] == 1490 and line["stock"] == 5 and line["available"], line
PY

methods="$(curl --fail --silent --show-error "$base_url/api/v1/payments/methods?delivery=pickup")"
python3 - "$methods" <<'PY'
import json, sys
methods = {item["id"] for item in json.loads(sys.argv[1])["methods"]}
assert methods == {"on_delivery"}, methods
PY

order_payload="{\"customer\":{\"name\":\"CI Commerce\",\"phone\":\"+7 900 000-00-00\",\"email\":\"${email}\"},\"delivery\":\"pickup\",\"items\":[{\"id\":\"${sku}\",\"quantity\":2}],\"consent\":true,\"paymentMethod\":\"on_delivery\"}"

# A forged payment choice must be rejected before it can reserve stock.
forged_payload="${order_payload/\"on_delivery\"/\"online\"}"
forged_code="$(curl --silent --show-error --output "${work}/forged-order.json" --write-out '%{http_code}' \
  -X POST -H 'Content-Type: application/json' --data "$forged_payload" "$base_url/api/v1/orders")"
if [[ "$forged_code" != "400" ]]; then
  echo "Unavailable online payment was accepted: HTTP ${forged_code}." >&2
  cat "${work}/forged-order.json" >&2
  exit 1
fi

order_code="$(curl --silent --show-error --max-time 35 --output "${work}/order.json" --write-out '%{http_code}' \
  -X POST -H 'Content-Type: application/json' --data "$order_payload" "$base_url/api/v1/orders")"
if [[ "$order_code" != "201" ]]; then
  echo "Order creation failed: HTTP ${order_code}." >&2
  cat "${work}/order.json" >&2
  exit 1
fi
order_number="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["orderNumber"])' "${work}/order.json")"

facts="$(psql_ci -Atq -F ':' -c "
  SELECT o.payment_status,o.total,oi.sku,oi.quantity,oi.reserved_qty,i.reserved_qty,
         (SELECT COUNT(*) FROM consent_events c WHERE c.order_id=o.id),
         (SELECT COUNT(*) FROM outbox x WHERE x.recipient='${email}'),
         (SELECT COUNT(*) FROM stock_movements m WHERE m.order_id=o.id AND m.kind='reserve')
  FROM orders o
  JOIN order_items oi ON oi.order_id=o.id
  JOIN inventory i ON i.variant_id=oi.variant_id
  WHERE o.order_number='${order_number}'")"
expected="on_delivery:2980.00:${sku}:2:2:2:1:1:1"
if [[ "$facts" != "$expected" ]]; then
  echo "Incomplete commerce transaction: ${facts}; expected ${expected}." >&2
  exit 1
fi

echo "Commerce image E2E passed for disposable order ${order_number}."
