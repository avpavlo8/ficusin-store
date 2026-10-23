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

// Existing API values are rubles. Arithmetic uses integer kopecks only.
export function toKopecks(value: number): number {
  if (!Number.isFinite(value) || Math.abs(value) > 1e12) throw new RangeError("Invalid money amount");
  const decimal = Math.abs(value).toString();
  if (decimal.includes("e")) return 0; // Only sub-kopeck magnitudes within the bound above.
  const [whole, fraction = ""] = decimal.split(".");
  const digits = fraction.padEnd(3, "0");
  return Math.sign(value) * (Number(whole) * 100 + Number(digits.slice(0, 2)) + (Number(digits[2]) >= 5 ? 1 : 0));
}
export function sumMoney(lines: ReadonlyArray<{ price: number; quantity: number }>): number {
  return lines.reduce((sum, line) => sum + toKopecks(line.price) * line.quantity, 0) / 100;
}
export function addMoney(...values: number[]): number {
  return values.reduce((sum, value) => sum + toKopecks(value), 0) / 100;
}
