import assert from "node:assert/strict";
import test from "node:test";
import {
  emptyProductDraft,
  loadProductDraft,
  saveProductDraft,
} from "../src/productCreationDraft.ts";

test("current drafts and old Saby journals retain useful content and canonical IDs", (t) => {
  let stored;
  const previous = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: () => stored ?? null,
      setItem: (_, value) => {
        stored = value;
      },
    },
  });
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, "localStorage", previous);
    else delete globalThis.localStorage;
  });
  const media = [{ id: "local-photo", name: "photo.jpg", type: "image/jpeg", size: 25 }];
  const variant = {
    id: "variant-10",
    serverId: 10,
    sku: "10",
    label: "D12",
    price: "1490",
    wholesaleMinQty: "1",
    active: true,
    archived: false,
    attributes: {},
    sabyId: "canonical-saby-id",
    externalIds: [{ provider: "saby", type: "id", externalId: "canonical-saby-id" }],
    marketplacePriceMode: "manual",
    wb: { price: "3210.25" },
    ozon: { price: "4321.45" },
    saby: { mode: "link", selected: { id: "retired-ui-id", code: "OLD" }, initialStock: "999" },
  };
  stored = JSON.stringify({
    ...emptyProductDraft(),
    name: "Сохранённое растение",
    media,
    variants: [variant],
    selectedVariantId: "variant-10",
    saby: variant.saby,
    channelTab: "wb",
  });
  const restored = loadProductDraft();
  assert.equal(restored.error, "");
  assert.equal(restored.draft.name, "Сохранённое растение");
  assert.deepEqual(restored.draft.media, media);
  assert.equal(restored.draft.selectedVariantId, "variant-10");
  assert.equal(restored.draft.variants[0].serverId, 10);
  assert.equal(restored.draft.variants[0].sabyId, "canonical-saby-id");
  assert.deepEqual(restored.draft.variants[0].externalIds, variant.externalIds);
  assert.equal(restored.draft.variants[0].wb.price, "3210.25");
  const current = { ...emptyProductDraft(), name: "Новый черновик", media };
  saveProductDraft(current);
  assert.deepEqual(loadProductDraft(), { draft: current, error: "" });
});
