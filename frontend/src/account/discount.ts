// Путь покупателя до следующей ступени скидки.
//
// Сами ступени — каждые 10 000 ₽ выполненных заказов дают процент, потолок
// десять процентов — живут в бэкенде: backend/internal/order/loyalty.go
// считает по ним retail_discount_bps, и к цене заказа применяется именно
// он. Здесь то же правило нужно только для одной фразы в кабинете: сколько
// осталось до следующего процента.
//
// Если ступени поменяются, поменять придётся оба места. Это осознанная
// цена: тянуть в браузер ещё одну ручку ради одной строки текста дороже,
// чем держать рядом семь строк арифметики.
const stepMinor = 1_000_000; // 10 000 ₽ в копейках
const maxPercent = 10;

export type DiscountProgress = {
  /** Заработанный процент — столько даёт накопленная сумма. */
  percent: number;
  /** Следующая ступень или null, если покупатель уже на потолке. */
  nextPercent: number | null;
  /** Сколько ещё нужно выполненных заказов, в копейках. */
  remainingMinor: number;
};

export function discountProgress(spendMinor: number): DiscountProgress {
  const spend = Number.isFinite(spendMinor) ? Math.max(0, spendMinor) : 0;
  const percent = Math.min(maxPercent, Math.floor(spend / stepMinor));
  if (percent >= maxPercent) {
    return { percent: maxPercent, nextPercent: null, remainingMinor: 0 };
  }
  return {
    percent,
    nextPercent: percent + 1,
    remainingMinor: (percent + 1) * stepMinor - spend,
  };
}
