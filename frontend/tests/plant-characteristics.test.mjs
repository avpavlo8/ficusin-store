import assert from "node:assert/strict";
import test from "node:test";
import { normalizePlantAttributes, plantEditorSchema } from "../src/plantCharacteristicSchema.ts";

test("old drafts preserve multivalued light and growth without reintroducing cleared choices", () => {
  const old = {
    light_level: "diffused",
    growth_habit: "climbing",
    storefront_traits: [
      "partial_shade",
      "sunny_window",
      "trailing",
      "fruiting",
      "warm_bonsai",
      "ornamental_bloom",
    ],
    flowering: false,
  };
  const migrated = normalizePlantAttributes(old, true);
  assert.deepEqual(migrated.light_level, ["diffused", "low_light", "sunny"]);
  assert.deepEqual(migrated.growth_habit, ["climbing", "trailing"]);
  assert.equal(migrated.fruiting, true);
  assert.equal(migrated.wintering_type, "warm");
  assert.equal(migrated.flowering, false);
  assert.equal(old.light_level, "diffused");
  migrated.light_level = ["diffused"];
  migrated.wintering_type = null;
  const reopened = normalizePlantAttributes(migrated);
  assert.deepEqual(reopened.light_level, ["diffused"]);
  assert.equal(reopened.wintering_type, null);
});

test("unknown plant facts stay unknown rather than becoming false or a guessed winter mode", () => {
  const result = normalizePlantAttributes(
    { wintering: "0…+8 °C", storefront_traits: ["bonsai"] },
    true,
  );
  assert.equal(result.wintering_type, undefined);
  assert.equal(result.fruiting, undefined);
  assert.equal(result.flowering, undefined);
});

test("winter has only the agreed three ranges, with no second explanatory field", () => {
  const schema = plantEditorSchema([{ code: "winter_notes", name: "Зимовка", dataType: "text" }]);
  assert.equal(
    schema.some((x) => x.code === "winter_notes"),
    false,
  );
  const winter = schema.find((x) => x.code === "wintering_type");
  assert.deepEqual(winter.options, ["warm", "cool", "cold"]);
  assert.deepEqual(Object.values(winter.optionLabels), [
    "Тёплая (+20…+27 °C)",
    "Прохладная (+10…+19 °C)",
    "Холодная (0…+9 °C)",
  ]);
  assert.equal(schema.find((x) => x.code === "light_level").dataType, "multi_enum");
  assert.equal(schema.find((x) => x.code === "growth_habit").dataType, "multi_enum");
  assert.equal(
    schema.some((x) => x.code === "storefront_traits"),
    false,
  );
});
