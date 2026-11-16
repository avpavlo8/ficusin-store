import assert from "node:assert/strict";
import test from "node:test";
import {
  bulkSelectableRecommendations,
  currentActionFailures,
  recommendedPackageCount,
  salesDataProblems,
} from "../src/lib/procurement-state.ts";

test("bulk selection skips unavailable rows and keeps one supplier without a supplier filter", () => {
  const items = [
    { sabyId: "unassigned", supplierId: 0, status: "recommended" },
    { sabyId: "first", supplierId: 1, status: "recommended" },
    { sabyId: "other", supplierId: 2, status: "recommended" },
    { sabyId: "excluded", supplierId: 1, status: "excluded" },
    { sabyId: "second", supplierId: 1, status: "recommended" },
  ];
  assert.deepEqual(
    bulkSelectableRecommendations(items).map((item) => item.sabyId),
    ["first", "second"],
  );
  assert.deepEqual(
    bulkSelectableRecommendations(items, 2).map((item) => item.sabyId),
    ["other"],
  );
  assert.deepEqual(bulkSelectableRecommendations(items, 3), []);
  assert.deepEqual(bulkSelectableRecommendations([]), []);
});

const batch = (id, status, channel, itemStatus, lineId = 1, kind = "prices") => ({
  id,
  kind,
  status,
  createdAt: `2026-11-${String(id).padStart(2, "0")}`,
  items: [{ id, lineId, channel, status: itemStatus, productCode: "A", externalArticle: "A" }],
});

test("a successful retry clears the old error only for the same channel and line", () => {
  const batches = [
    batch(1, "failed", "wb", "failed"),
    batch(2, "failed", "ozon", "failed"),
    batch(3, "completed", "wb", "completed"),
  ];
  assert.deepEqual(
    currentActionFailures(batches).map((item) => item.channel),
    ["ozon"],
  );
  assert.equal(
    currentActionFailures([...batches, batch(4, "completed", "ozon", "completed", 2)]).length,
    1,
  );
});

test("cancelled batches never raise an active receipt warning or undo a confirmed result", () => {
  assert.deepEqual(
    currentActionFailures([
      batch(1, "cancelled", "saby_receipt", "failed", 0, "receipt"),
      batch(2, "completed", "saby_receipt", "completed", 0, "receipt"),
    ]),
    [],
  );
  assert.equal(
    currentActionFailures([
      batch(1, "failed", "wb", "failed"),
      batch(2, "cancelled", "wb", "completed"),
    ]).length,
    1,
  );
});

test("batch order from the API does not change which attempt is current", () => {
  assert.deepEqual(
    currentActionFailures([
      batch(3, "completed", "wb", "completed"),
      batch(1, "failed", "wb", "failed"),
    ]),
    [],
  );
});

test("stale sales and unlinked sales are visible even when status says ok", () => {
  const fresh = {
    channel: "site",
    status: "ok",
    lastSuccessAt: "2026-11-14",
    lastError: "",
    rowsUnlinked: 0,
    freshness: "fresh",
  };
  const stale = { ...fresh, channel: "ozon", freshness: "stale" };
  const unlinked = { ...fresh, channel: "wb", rowsUnlinked: 4 };
  assert.deepEqual(
    salesDataProblems([fresh, stale, unlinked]).map((item) => item.channel),
    ["ozon", "wb"],
  );
  assert.equal(salesDataProblems([{ ...fresh, status: "error" }]).length, 1);
  assert.equal(salesDataProblems([{ ...fresh, lastSuccessAt: null }]).length, 1);
  assert.equal(
    salesDataProblems([{ ...fresh, status: "disabled", lastSuccessAt: null }]).length,
    0,
  );
});

test("recommendations keep the required units when creating a draft", () => {
  assert.equal(
    recommendedPackageCount({ suggestedQty: 24, orderMultiple: 6, minimumOrderQty: 1 }),
    "4",
  );
  assert.equal(
    recommendedPackageCount({ suggestedQty: 19, orderMultiple: 6, minimumOrderQty: 1 }),
    "4",
  );
  assert.equal(
    recommendedPackageCount({ quantityKnown: false, suggestedQty: 0, orderMultiple: 6 }),
    "",
  );
  assert.equal(recommendedPackageCount(), "1");
});
