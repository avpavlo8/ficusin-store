export function counted(count: number, forms: readonly [string, string, string]) {
  const n = Math.abs(count) % 100;
  return `${count} ${forms[n > 10 && n < 20 ? 2 : n % 10 === 1 ? 0 : n % 10 >= 2 && n % 10 <= 4 ? 1 : 2]}`;
}

export function catalogSize(label: string, diameter?: number) {
  if (diameter && diameter > 0) {
    const trimmed = label.trim();
    const numeric = trimmed.match(/^(\d+(?:[.,]\d+)?)\s*(?:см)?$/i);
    const detail = numeric && Number(numeric[1].replace(",", ".")) === diameter ? ""
      : trimmed.replace(/(?:\s*[·,|]\s*|\s+)?(?:D|Ø|⌀)\s*\d+(?:[.,]\d+)?\s*(?:см)?$/i, "").trim();
    return `${detail && !/^Основной (вариант|размер)$/i.test(detail) ? `${detail} · ` : ""}Горшок Ø ${diameter} см`;
  }
  if (!label) return "Размер уточняется";
  const size = label.replace(/\bD\s*(\d+(?:[.,]\d+)?)/gi, "Ø $1 см").replace(/\bH\s*(\d+(?:[.,]\d+)?)/gi, "высота $1 см");
  return /^Ø \d+(?:[.,]\d+)? см$/.test(size) ? `Горшок ${size}` : size;
}
