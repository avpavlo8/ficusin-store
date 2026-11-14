export type RangeValue = { min: string; max: string };

// Accumulate URL keys in Maps; expose only own properties without a prototype.
export function initialAttributeFilters(search: string) {
  const result = new Map<string, string>();
  new URLSearchParams(search).forEach((value, key) => {
    if (key.startsWith("filter.")) result.set(key.slice(7), value);
  });
  return Object.setPrototypeOf(Object.fromEntries(result), null) as Record<string, string>;
}

export function initialRangeFilters(search: string) {
  const result = new Map<string, RangeValue>();
  new URLSearchParams(search).forEach((value, key) => {
    if (key.startsWith("min.")) {
      const code = key.slice(4);
      result.set(code, { min: value, max: result.get(code)?.max || "" });
    }
    if (key.startsWith("max.")) {
      const code = key.slice(4);
      result.set(code, { min: result.get(code)?.min || "", max: value });
    }
  });
  return Object.setPrototypeOf(Object.fromEntries(result), null) as Record<string, RangeValue>;
}
