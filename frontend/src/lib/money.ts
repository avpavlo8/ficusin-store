const wholeRubles = new Intl.NumberFormat("ru-RU", {
  style: "currency", currency: "RUB", maximumFractionDigits: 0,
});
const rublesAndKopecks = new Intl.NumberFormat("ru-RU", {
  style: "currency", currency: "RUB", minimumFractionDigits: 2, maximumFractionDigits: 2,
});

export function formatMoney(value: number): string {
  // Keep whole prices compact, but never hide kopecks on a discounted order.
  return Math.round(value * 100) % 100 === 0
    ? wholeRubles.format(value)
    : rublesAndKopecks.format(value);
}

export const money = { format: formatMoney };
