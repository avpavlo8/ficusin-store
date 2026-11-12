import assert from "node:assert/strict";
import test from "node:test";
import {
  marketplaceMissing,
  blankMarketplaceChannel,
  flattenCategories,
} from "../src/catalogEditorChannels.ts";
const common = () => ({
  name: "",
  article: "",
  description: "",
  brand: "",
  model: "",
  tnved: "",
  units: 1,
  ozon: blankMarketplaceChannel(),
  wb: blankMarketplaceChannel(),
});
test("unselected marketplaces never block incomplete drafts", () =>
  assert.deepEqual(marketplaceMissing(common()), []));
test("existing product requires only its channel article", () => {
  const m = common();
  m.wb = { ...m.wb, enabled: true, mode: "link", existingId: "123456" };
  assert.deepEqual(marketplaceMissing(m), []);
  m.wb.existingId = "";
  assert.deepEqual(marketplaceMissing(m), ["Артикул существующего товара Wildberries"]);
});
test("enabled new marketplaces require common data and their own category", () => {
  const m = common();
  m.ozon.enabled = true;
  assert.ok(marketplaceMissing(m).includes("Категория Ozon"));
  m.ozon.category = { id: "10", name: "Саженец", typeId: 20 };
  Object.assign(m, {
    article: "site-1",
    brand: "Бренд",
    model: "Нолина",
    tnved: "060290",
    units: 1,
  });
  assert.deepEqual(marketplaceMissing(m), []);
});
test("Ozon leaf keeps both category ID and type ID", () =>
  assert.deepEqual(
    flattenCategories("ozon", {
      result: [
        {
          description_category_id: 10,
          category_name: "Растения",
          children: [{ type_id: 20, type_name: "Саженец", children: [] }],
        },
      ],
    }),
    [{ id: "10", name: "Растения / Саженец", typeId: 20 }],
  ));

test("model and units are required only for Ozon", () => {
  const m = common();
  Object.assign(m, { article: "test", brand: "brand", tnved: "060290", units: 0, model: "" });
  m.wb = { ...m.wb, enabled: true, category: { id: "10", name: "Растения" } };
  assert.deepEqual(marketplaceMissing(m), []);
  m.ozon = { ...m.ozon, enabled: true, category: { id: "20", typeId: 30, name: "Саженец" } };
  assert.deepEqual(marketplaceMissing(m), ["Название модели", "Единиц в одном товаре"]);
});

test("required category attributes retain dictionary IDs and are not hidden by a generic name", async () => {
  const { attributeValues, attributeRole } = await import("../src/marketplaceAttributes.ts");
  assert.deepEqual(attributeValues([{ value: "Россия", dictionaryValueId: 99 }]), [
    { value: "Россия", id: 99 },
  ]);
  assert.equal(attributeRole({ id: 8229, name: "Тип" }, "ozon"), "type");
  assert.equal(attributeRole({ id: 42, name: "Название сорта" }, "ozon"), "");
  assert.equal(attributeRole({ id: 9048, name: "Название модели" }, "wb"), "");
});

test("creation choices never disable existing links or an explicit link action", () => {
  const m = common();
  m.wb.mode = "link";
  m.wb.enabled = false;
  assert.deepEqual(marketplaceMissing(m), ["Артикул существующего товара Wildberries"]);
  assert.deepEqual(marketplaceMissing(m, { wb: "123" }), []);
  m.wb.existingId = "123";
  assert.deepEqual(marketplaceMissing(m), []);
});

test("linked card autofill imports metadata and preserves local edits", async () => {
  const { mergeMarketplaceCards } = await import("../src/marketplaceAutofill.ts");
  const initial = common();
  initial.article = "81";
  const source = {
    ...common(),
    article: "remote-offer",
    brand: "Фикусин",
    brandId: 99,
    model: "Нолина",
    tnved: "0602909900",
    tnvedId: 98,
    units: 2,
  };
  const remote = {
    ozon: {
      card: {
        name: "Название Ozon",
        description: "Описание Ozon",
        marketplace: source,
        channel: {
          ...initial.ozon,
          category: { id: "10", typeId: 20, name: "Растения / Саженец" },
          attributes: { country: [{ value: "Нидерланды", dictionaryValueId: 97 }] },
        },
      },
      externalIds: { productId: "100", offerId: "remote-offer" },
    },
  };
  const site = {
    name: "Название сайта",
    description: "Описание сайта",
    originalName: "Название сайта",
    originalDescription: "Описание сайта",
    sku: "81",
  };
  const loaded = mergeMarketplaceCards(initial, remote, site);
  assert.equal(loaded.ozon.category.id, "10");
  assert.equal(loaded.article, "remote-offer");
  assert.equal(loaded.brandId, 99);
  assert.equal(loaded.model, "Нолина");
  assert.equal(loaded.units, 2);
  assert.equal(loaded.name, "Название Ozon");
  initial.brand = "Мой бренд";
  initial.model = "Моя модель";
  initial.article = "Мой артикул";
  initial.ozon.category = { id: "55", name: "Мой выбор" };
  initial.ozon.attributes = { country: "Мой выбор" };
  const kept = mergeMarketplaceCards(initial, remote, { ...site, name: "Моя правка названия" });
  assert.equal(kept.brand, "Мой бренд");
  assert.equal(kept.article, "Мой артикул");
  assert.equal(kept.model, "Моя модель");
  assert.equal(kept.ozon.category.id, "55");
  assert.equal(kept.ozon.attributes.country, "Мой выбор");
  assert.equal(kept.name, "");
});

test("channel articles validate independently and empty values do not fall back", () => {
  const m = common();
  Object.assign(m, { article: "legacy", brand: "brand", tnved: "060290", model: "model" });
  m.wb = { ...m.wb, enabled: true, article: "wb-only", category: { id: "1", name: "WB" } };
  m.ozon = {
    ...m.ozon,
    enabled: true,
    article: "",
    category: { id: "2", typeId: 3, name: "Ozon" },
  };
  assert.deepEqual(marketplaceMissing(m), ["Артикул продавца Ozon"]);
  m.ozon.article = "ozon-only";
  assert.deepEqual(marketplaceMissing(m), []);
  assert.equal(m.wb.article, "wb-only");
});
test("remote identity migrates legacy drafts without making their shared article a relink", async () => {
  const { mergeMarketplaceCards } = await import("../src/marketplaceAutofill.ts");
  const m = common();
  m.article = "old-user-shared-value";
  const card = {
    card: { name: "", description: "", marketplace: common(), channel: m.wb },
    externalIds: { productId: "77", offerId: "actual-wb" },
  };
  const site = { name: "", description: "", sku: "81" };
  const loaded = mergeMarketplaceCards(m, { wb: card }, site);
  assert.equal(loaded.wb.article, "actual-wb");
  assert.equal(loaded.wb.linkedArticle, "actual-wb");
  assert.equal(loaded.wb.replaceLink, false);
  assert.equal(loaded.article, "old-user-shared-value");
  loaded.wb.article = "new-intent";
  loaded.wb.replaceLink = true;
  const pending = mergeMarketplaceCards(loaded, { wb: card }, site);
  assert.equal(pending.wb.article, "new-intent");
  assert.equal(pending.wb.replaceLink, true);
  const done = mergeMarketplaceCards(
    pending,
    { wb: { ...card, externalIds: { productId: "88", offerId: "new-intent" } } },
    site,
  );
  assert.equal(done.wb.linkedArticle, "new-intent");
  assert.equal(done.wb.replaceLink, false);
});

test("switching an article replaces populated common fields and channel attributes", async () => {
  const { replaceMarketplaceCard } = await import("../src/marketplaceAutofill.ts");
  const current = common();
  Object.assign(current, {
    name: "Старая карточка",
    description: "Старое описание",
    brand: "Старый бренд",
    brandId: 9,
    tnved: "old",
    tnvedId: 10,
    model: "Старая модель",
    units: 7,
  });
  current.ozon = {
    ...current.ozon,
    article: "new-ozon",
    linkedArticle: "old-ozon",
    replaceLink: true,
    category: { id: "old", name: "old" },
    attributes: { old: "Удалить", country: "Россия" },
  };
  const remote = {
    card: {
      name: "Новая карточка",
      description: "Новое описание",
      marketplace: { ...common(), brand: "Новый бренд", model: "Новая модель", units: 2 },
      channel: {
        ...blankMarketplaceChannel(),
        category: { id: "new", name: "new" },
        attributes: { country: "Нидерланды" },
      },
    },
    externalIds: { productId: "99", offerId: "new-ozon" },
  };
  const next = replaceMarketplaceCard(current, "ozon", remote);
  assert.equal(next.name, "Новая карточка");
  assert.equal(next.description, "Новое описание");
  assert.equal(next.brand, "Новый бренд");
  assert.equal(next.brandId, undefined);
  assert.equal(next.tnved, "");
  assert.equal(next.tnvedId, undefined);
  assert.equal(next.model, "Новая модель");
  assert.equal(next.units, 2);
  assert.equal(next.ozon.category.id, "new");
  assert.deepEqual(next.ozon.attributes, { country: "Нидерланды" });
  assert.equal(next.ozon.linkedArticle, "old-ozon");
  assert.equal(next.ozon.replaceLink, true);
  assert.equal(next.ozon.existingId, "new-ozon");
  assert.deepEqual(next.wb, current.wb);
  assert.equal(next.contentFromMarketplace, true);
  const empty = replaceMarketplaceCard(current, "ozon", {
    ...remote,
    card: { ...remote.card, description: "" },
  });
  assert.equal(empty.description, "");
  current.ozon.article = "changed-again";
  assert.equal(
    replaceMarketplaceCard(current, "ozon", remote),
    current,
    "late response replaced a newer selection",
  );
});

test("WB lookup preserves Ozon-only model and unit fields", async () => {
  const { replaceMarketplaceCard } = await import("../src/marketplaceAutofill.ts");
  const current = common();
  current.wb.article = "wb";
  current.model = "Модель Ozon";
  current.units = 3;
  const next = replaceMarketplaceCard(current, "wb", {
    card: {
      name: "WB",
      description: "",
      marketplace: common(),
      channel: blankMarketplaceChannel(),
    },
    externalIds: { productId: "99", offerId: "wb" },
  });
  assert.equal(next.model, "Модель Ozon");
  assert.equal(next.units, 3);
});
