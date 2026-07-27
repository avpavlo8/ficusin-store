export function normalizeRussianPhone(value: string) {
  const digits = value.replace(/\D/g, "");
  let nationalNumber = "";

  if (digits.length === 11 && (digits.startsWith("7") || digits.startsWith("8"))) {
    nationalNumber = digits.slice(1);
  } else if (digits.length === 10) {
    nationalNumber = digits;
  } else {
    return null;
  }

  if (
    !/^[3-9]\d{9}$/.test(nationalNumber) ||
    /^(\d)\1{9}$/.test(nationalNumber)
  ) {
    return null;
  }

  return `+7${nationalNumber}`;
}
