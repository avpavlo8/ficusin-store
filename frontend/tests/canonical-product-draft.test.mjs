import assert from "node:assert/strict";
import test from "node:test";
import {
  emptyProductDraft,
  loadProductDraft,
  draftStorageKey,
} from "../src/productCreationDraft.ts";

function storage(t, raw, reject = () => false) {
  const before = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const values = new Map([[draftStorageKey, raw]]);
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        if (reject(key)) throw new DOMException("Quota exhausted", "QuotaExceededError");
        values.set(key, value);
      },
    },
  });
  t.after(() =>
    before
      ? Object.defineProperty(globalThis, "localStorage", before)
      : delete globalThis.localStorage,
  );
  return values;
}
function legacy() {
  return {
    ...emptyProductDraft(),
    version: 1,
    name: "Не потерять работу",
    categoryId: 7,
    description: "Полный текст",
    latinName: "Latin",
    care: "Уход",
    warnings: "Первая строка\nВторая строка",
    creationId: "existing-create-id",
    remoteCreatedProductId: 81,
    selectedVariantId: "saved-b",
    editorTab: "faq",
    collectionMemberships: { 42: true },
    attributes: { storefront_traits: ["partial_shade", "trailing"] },
    plantAttributesVersion: undefined,
    faq: [{ id: "faq-id", question: "Вопрос", answer: "Ответ" }],
    media: [{ id: "cover-id", name: "cover.png", type: "image/png" }],
    cover: null,
    unknownFutureWork: { mustRemain: [1, 2] },
    variants: [
      {
        id: "saved-a",
        serverId: 80,
        sku: "A",
        label: "D12",
        price: "0",
        wholesaleMinQty: "3",
        active: true,
        archived: false,
        attributes: { pot_diameter_cm: 12 },
        externalIds: [{ provider: "saby", type: "id", externalId: "3980" }],
        marketplacePriceMode: "manual",
        ozon: { price: "0" },
        wb: { price: "1990.50" },
        marketplace: {
          name: "Название МП",
          article: "COMMON",
          description: "Текст МП",
          brand: "Бренд",
          model: "Модель",
          tnved: "0602",
          units: 2,
          ozon: {
            article: "",
            linkedArticle: "OLD",
            replaceLink: true,
            enabled: true,
            mode: "link",
            existingId: "OZON",
            category: { id: "1", name: "Саженцы", typeId: 4 },
          },
          wb: {
            article: "WB-A",
            enabled: true,
            mode: "create",
            existingId: "",
            category: { id: "2", name: "Растения" },
          },
        },
        sabyEditor: {
          mode: "create",
          existingCode: "",
          category: { id: "", name: "" },
          folder: { id: "", name: "" },
        },
        sabyEditorTouched: { mode: true, existingCode: true, folder: true },
        media: [{ id: "local-file-id", name: "a.webp", type: "image/webp" }],
        mediaInitialized: true,
        mediaCustomized: true,
      },
      {
        id: "saved-b",
        serverId: 81,
        sku: "B",
        label: "D17",
        price: "200",
        active: false,
        archived: false,
        attributes: {},
        externalIds: [],
        ozon: { article: "O-B", category: "Старая категория", price: "500", enabled: true },
        wb: { article: "W-B", price: "600" },
        saby: {
          mode: "link",
          selected: { id: "technical-id", code: "X123" },
          folderId: "folder-original",
        },
      },
    ],
  };
}

test("V1 conversion preserves saved work, identities, explicit clears and per-channel articles", (t) => {
  const source = legacy(),
    raw = JSON.stringify(source),
    values = storage(t, raw);
  const result = loadProductDraft();
  assert.equal(result.error, "");
  assert.equal(result.draft.version, 2);
  for (const field of [
    "name",
    "categoryId",
    "description",
    "latinName",
    "care",
    "warnings",
    "creationId",
    "remoteCreatedProductId",
    "selectedVariantId",
    "editorTab",
    "faq",
    "media",
    "cover",
    "collectionMemberships",
    "unknownFutureWork",
  ])
    assert.deepEqual(result.draft[field], source[field], field);
  assert.deepEqual(result.draft.attributes.light_level, ["low_light"]);
  const [a, b] = result.draft.variants;
  assert.equal(a.id, "saved-a");
  assert.equal(a.serverId, 80);
  assert.equal(a.price, "0");
  assert.equal(a.marketplacePriceMode, "manual");
  assert.equal(a.ozon.price, "0");
  assert.equal(a.wb.price, "1990.50");
  assert.equal(a.marketplace.ozon.article, "");
  assert.equal(a.marketplace.wb.article, "WB-A");
  assert.equal(a.marketplace.ozon.replaceLink, true);
  assert.equal(a.marketplace.ozon.linkedArticle, "OLD");
  assert.deepEqual(a.sabyEditor, source.variants[0].sabyEditor);
  assert.deepEqual(a.sabyEditorTouched, source.variants[0].sabyEditorTouched);
  assert.deepEqual(a.media, source.variants[0].media);
  assert.deepEqual(a.externalIds, source.variants[0].externalIds);
  assert.equal(b.marketplace.ozon.article, "O-B");
  assert.equal(b.marketplace.wb.article, "W-B");
  assert.deepEqual(b.marketplace.ozon.category, { id: "", name: "Старая категория" });
  assert.equal(b.sabyEditor.existingCode, "X123");
  assert.notEqual(b.sabyEditor.existingCode, "technical-id");
  assert.equal(values.get(`${draftStorageKey}:migration-backup:v1`), raw);
  const canonical = values.get(draftStorageKey),
    count = values.size;
  assert.deepEqual(loadProductDraft().draft, result.draft);
  assert.equal(values.get(draftStorageKey), canonical);
  assert.equal(values.size, count);
  assert.equal(JSON.stringify(source), raw, "input object mutated");
});

for (const phase of ["backup", "canonical"])
  test(`failed ${phase} write preserves the exact original and blocks autosave`, (t) => {
    const raw = JSON.stringify(legacy()),
      values = storage(t, raw, (key) =>
        phase === "backup" ? key !== draftStorageKey : key === draftStorageKey,
      );
    const result = loadProductDraft();
    assert.equal(result.blocked, true);
    assert.ok(result.error);
    assert.equal(values.get(draftStorageKey), raw);
    if (phase === "canonical")
      assert.equal(values.get(`${draftStorageKey}:migration-backup:v1`), raw);
  });

for (const raw of [
  "{broken",
  JSON.stringify({ version: 1, name: "Malformed", media: "not an array", faq: [] }),
  JSON.stringify({ version: 99 }),
])
  test(`unreadable or unsupported drafts remain untouched: ${raw.slice(0, 15)}`, (t) => {
    const values = storage(t, raw);
    assert.equal(loadProductDraft().blocked, true);
    assert.equal(values.get(draftStorageKey), raw);
  });

test("another migration source never overwrites an earlier source backup", (t) => {
  const raw = JSON.stringify(legacy()),
    values = storage(t, raw);
  values.set(`${draftStorageKey}:migration-backup:v1`, "earlier untouched source");
  assert.equal(loadProductDraft().error, "");
  assert.equal(values.get(`${draftStorageKey}:migration-backup:v1`), "earlier untouched source");
  assert.equal(values.get(`${draftStorageKey}:migration-backup:v1:1`), raw);
});

test("invalid variant identities are not silently replaced and overwritten", (t) => {
  const source = legacy();
  source.variants[0].id = 12;
  const raw = JSON.stringify(source),
    values = storage(t, raw);
  assert.equal(loadProductDraft().blocked, true);
  assert.equal(values.get(draftStorageKey), raw);
  assert.equal(values.size, 1);
});

test("legacy marketplace amounts infer manual once while explicit auto remains auto", (t) => {
  const source = legacy();
  source.variants[1].marketplacePriceMode = "auto";
  const values = storage(t, JSON.stringify(source));
  const result = loadProductDraft();
  assert.equal(result.draft.variants[1].marketplacePriceMode, "auto");
  delete source.variants[1].marketplacePriceMode;
  values.set(draftStorageKey, JSON.stringify(source));
  assert.equal(loadProductDraft().draft.variants[1].marketplacePriceMode, "manual");
});

test("null marketplace articles inherit while an explicit empty string remains cleared", (t) => {
  const source = legacy();
  source.variants[0].marketplace.ozon.article = null;
  source.variants[0].ozon.article = ""; // Retired channel default must not override the shared modern article.
  source.variants[0].marketplace.wb.article = "";
  const values = storage(t, JSON.stringify(source));
  let result = loadProductDraft();
  assert.equal(result.draft.variants[0].marketplace.ozon.article, "COMMON");
  assert.equal(result.draft.variants[0].marketplace.wb.article, "");
  delete source.variants[0].marketplace;
  source.variants[0].ozon.article = null;
  source.variants[0].wb.article = "LEGACY-SHARED";
  values.set(draftStorageKey, JSON.stringify(source));
  result = loadProductDraft();
  assert.equal(result.draft.variants[0].marketplace.ozon.article, "LEGACY-SHARED");
});

for (const duplicate of ["local", "server", "blank"])
  test(`ambiguous ${duplicate} variant identities block conversion without changing the source`, (t) => {
    const source = legacy();
    if (duplicate === "local") source.variants[1].id = source.variants[0].id;
    if (duplicate === "server") source.variants[1].serverId = source.variants[0].serverId;
    if (duplicate === "blank") source.variants[1].id = " ";
    const raw = JSON.stringify(source),
      values = storage(t, raw);
    assert.equal(loadProductDraft().blocked, true);
    assert.equal(values.get(draftStorageKey), raw);
    assert.equal(values.size, 1);
  });
