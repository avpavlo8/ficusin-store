import assert from "node:assert/strict";
import test from "node:test";
import { mergeCustomerOrderPage } from "../src/lib/customer-order-list.ts";

test("appended pages stay newest first without duplicate links after an overlapping page", () => {
  const original = [
    { orderNumber: "0001-2", createdAt: "2026-11-12", status: "new" },
    { orderNumber: "0001-1", createdAt: "2026-11-10", status: "new" },
  ];
  const next = [
    { orderNumber: "0001-1", createdAt: "2026-11-10", status: "ready" },
    { orderNumber: "0001-0", createdAt: "2026-11-09", status: "completed" },
  ];
  const merged = mergeCustomerOrderPage(original, next);
  assert.deepEqual(
    merged.map((order) => order.orderNumber),
    ["0001-2", "0001-1", "0001-0"],
  );
  assert.equal(merged[1].status, "ready");
  assert.equal(original[1].status, "new");
});

test("first-page refresh replaces the previous view and sorts server data", () => {
  assert.deepEqual(
    mergeCustomerOrderPage(
      [],
      [
        { orderNumber: "old", createdAt: "2026-10-01" },
        { orderNumber: "new", createdAt: "2026-11-01" },
      ],
    ).map((order) => order.orderNumber),
    ["new", "old"],
  );
});
