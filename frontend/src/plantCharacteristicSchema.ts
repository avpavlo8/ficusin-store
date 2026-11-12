import type { CategoryAttribute } from "./adminTypes";

const choices = {
  light_level: { sunny: "Прямое солнце", diffused: "Рассеянный свет", low_light: "Полутень" },
  growth_habit: {
    upright: "Вертикальная",
    bushy: "Кустовая",
    trailing: "Ампельная",
    climbing: "На опоре",
    rosette: "Розетка",
    tree: "Деревце",
  },
  leaf_coloring: { plain: "Однотонная", variegated: "Вариегатная", patterned: "Узорчатая" },
  wintering_type: {
    warm: "Тёплая (+20…+27 °C)",
    cool: "Прохладная (+10…+19 °C)",
    cold: "Холодная (0…+9 °C)",
  },
};

// A saved browser draft can predate the server schema migration. Keep it
// editable without requiring the old duplicate collection flags in the form.
export function normalizePlantAttributes(
  values: Record<string, string | number | boolean | string[] | null>,
  migrateLegacy = false,
): Record<string, string | number | boolean | string[] | null> {
  const next = { ...values };
  for (const code of ["light_level", "growth_habit"]) {
    if (typeof next[code] === "string" && next[code]) next[code] = [next[code]];
  }
  if (!migrateLegacy || !Array.isArray(next.storefront_traits)) return next;
  const traits = next.storefront_traits as string[];
  for (const [trait, code, value] of [
    ["partial_shade", "light_level", "low_light"],
    ["sunny_window", "light_level", "sunny"],
    ["trailing", "growth_habit", "trailing"],
    ["support_climber", "growth_habit", "climbing"],
    ["tree_form", "growth_habit", "tree"],
  ]) {
    if (traits.includes(trait))
      next[code] = [
        ...new Set([...(Array.isArray(next[code]) ? (next[code] as string[]) : []), value]),
      ];
  }
  for (const [trait, code, value] of [
    ["ornamental_bloom", "flowering", true],
    ["fruiting", "fruiting", true],
    ["patterned_foliage", "leaf_coloring", "patterned"],
    ["warm_bonsai", "wintering_type", "warm"],
  ] as const) {
    if (traits.includes(trait) && next[code] == null) next[code] = value;
  }
  return next;
}

// Also supplies metadata for drafting locally while the connected backend is
// still on the preceding schema. The matching SQL migration owns persistence.
export function plantEditorSchema(schema: CategoryAttribute[]): CategoryAttribute[] {
  const fields: Array<[string, string, CategoryAttribute["dataType"]]> = [
    ["light_level", "Освещение", "multi_enum"],
    ["growth_habit", "Форма роста", "multi_enum"],
    ["flowering", "Цветущее", "boolean"],
    ["leaf_coloring", "Окраска листьев", "enum"],
    ["fruiting", "Плодовое растение", "boolean"],
    ["wintering_type", "Зимовка", "enum"],
  ];
  const result = schema.filter(
    (item) =>
      item.code !== "storefront_traits" &&
      !(item.code !== "wintering_type" && /зимовк/i.test(item.name)),
  );
  for (const [code, name, dataType] of fields) {
    const old = result.find((item) => item.code === code);
    const labels = choices[code as keyof typeof choices];
    const field: CategoryAttribute = {
      unit: "",
      audience: "customer",
      scope: "product",
      required: false,
      filterable: true,
      showOnPdp: true,
      keyCharacteristic: false,
      badge: false,
      sortOrder: 100,
      ...old,
      code,
      name,
      dataType,
      options: labels ? Object.keys(labels) : [],
      optionLabels: labels || {},
    };
    const index = result.findIndex((item) => item.code === code);
    if (index < 0) result.push(field);
    else result[index] = field;
  }
  return result;
}
