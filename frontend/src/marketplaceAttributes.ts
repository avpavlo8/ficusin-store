import type { MarketplaceAttributeValue, OzonAttribute } from "./catalogEditorChannels";
type Choice = { id?: number; value: string };
export function attributeValues(raw: MarketplaceAttributeValue | undefined): Choice[] {
  if (typeof raw === "string") return raw.trim() ? [{ value: raw }] : [];
  return (raw || []).map((v) => ({ value: v.value, id: v.dictionaryValueId }));
}
export function attributeRole(a: OzonAttribute, channel: "wb" | "ozon"): string {
  const name = a.name.toLowerCase().trim();
  if (channel === "ozon" && a.id === 8229) return "type";
  if (/бренд/.test(name)) return "brand";
  if (/тн.*вэд/.test(name)) return "tnved";
  if (
    [
      "название",
      "наименование",
      "название товара",
      "наименование товара",
      "аннотация",
      "описание",
      "артикул",
      "артикул продавца",
      "ндс",
      "ставка ндс",
    ].includes(name)
  )
    return "common";
  if (
    channel === "ozon" &&
    (/название модели/.test(name) || /единиц в одном товаре|количество в упаковке/.test(name))
  )
    return "common";
  return "";
}
