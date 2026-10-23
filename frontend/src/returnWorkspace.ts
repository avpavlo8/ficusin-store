import type { MarketplaceReturn } from "./adminTypes";
export type ReturnCondition = MarketplaceReturn["condition"];
export const returnConditions = {
  inspection: {
    label: "На осмотре",
    tab: "Осмотр",
    note: "Состояние ещё не оценено",
    className: "state-inspection",
  },
  ready: {
    label: "Готово к продаже",
    tab: "В продажу",
    note: "Растение готово. Поступление — отдельно",
    className: "state-ready",
  },
  restoring: {
    label: "На восстановлении",
    tab: "Восстановление",
    note: "Нужны уход и повторная оценка",
    className: "state-restoring",
  },
  dead: {
    label: "Списано",
    tab: "Списано",
    note: "Стоимость учтена как потеря",
    className: "state-dead",
  },
} as const;
export const returnChannels: Record<string, string> = {
  wb: "ВБ",
  ozon: "Озон",
  other: "Другое",
  avito: "Авито",
  saby: "Розница СБИС",
};
export function receiptLabel(
  item: Pick<MarketplaceReturn, "receiptStatus" | "receiptExternalUrl">,
) {
  if (item.receiptStatus === "correction_required") return "Проверьте состав в СБИС";
  if (item.receiptStatus === "posted") return "Поступление проведено";
  if (item.receiptExternalUrl) return "Черновик в СБИС";
  return (
    {
      none: "Документ не создан",
      queued: "Создаётся поступление",
      checking: "Проверяем поступление",
      draft_created: "Черновик в СБИС",
      failed: "Нужно проверить операцию",
    }[item.receiptStatus] || "Нужно проверить операцию"
  );
}
export function plantCount(count: number) {
  const n = count % 100;
  return `${count} ${n >= 11 && n <= 14 ? "растений" : n % 10 === 1 ? "растение" : n % 10 >= 2 && n % 10 <= 4 ? "растения" : "растений"}`;
}
export function discountPrice(price = 0, discount = 0) {
  return Math.round((Math.round(price * 100) * (100 - discount)) / 100) / 100;
}
export function returnDate(value: string) {
  return new Date(value.length === 10 ? `${value}T00:00:00` : value).toLocaleDateString("ru-RU");
}
