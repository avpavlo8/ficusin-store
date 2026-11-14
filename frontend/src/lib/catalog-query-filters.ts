export type RangeValue = { min: string; max: string };

// URL keys are dictionary entries, never properties of Object.prototype.
export function initialAttributeFilters(search: string) {
  const result: Record<string, string> = Object.create(null);
  new URLSearchParams(search).forEach((value, key) => {
    if (key.startsWith("filter.")) result[key.slice(7)] = value;
  });
  return result;
}

export function initialRangeFilters(search: string) {
  const result: Record<string, RangeValue> = Object.create(null);
  new URLSearchParams(search).forEach((value, key) => {
    if (key.startsWith("min.")) {
      const code = key.slice(4);
      result[code] = { min: value, max: result[code]?.max || "" };
    }
    if (key.startsWith("max.")) {
      const code = key.slice(4);
      result[code] = { min: result[code]?.min || "", max: value };
    }
  });
  return result;
}
