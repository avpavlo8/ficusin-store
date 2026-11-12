export type PackagingSeason = "summer" | "winter";

export function marketplacePricingInput(
  price: string,
  attributes: Record<string, unknown>,
  season: PackagingSeason,
) {
  const retailRub = Number(price.replace(",", "."));
  if (!Number.isFinite(retailRub) || retailRub <= 0 || retailRub > 1e9)
    return { error: "Укажите цену на сайте больше нуля" } as const;
  const prefix = season === "winter" ? "package_winter_" : "package_";
  const dimensions = ["length", "width", "height"].map((dimension) =>
    Number(attributes[`${prefix}${dimension}_cm`]),
  );
  if (dimensions.some((value) => !Number.isFinite(value) || value <= 0 || value > 1000))
    return {
      error: `Заполните длину, ширину и высоту ${season === "winter" ? "зимней" : "летней"} упаковки (до 1000 см)`,
    } as const;
  return {
    body: {
      mode: "retail",
      retailRub,
      package: {
        lengthCm: dimensions[0],
        widthCm: dimensions[1],
        heightCm: dimensions[2],
        source: season === "winter" ? "Редактор: зимняя упаковка" : "Редактор: летняя упаковка",
      },
    },
  } as const;
}
