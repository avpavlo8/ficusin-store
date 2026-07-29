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

export function formatRussianPhoneInput(value: string) {
  const digits = value.replace(/\D/g, "");
  if (!digits) return "";

  const nationalNumber = (
    digits.startsWith("7") || digits.startsWith("8")
      ? digits.slice(1)
      : digits
  ).slice(0, 10);

  let formatted = "+7";
  if (nationalNumber.length > 0) {
    formatted += ` ${nationalNumber.slice(0, 3)}`;
  }
  if (nationalNumber.length > 3) {
    formatted += ` ${nationalNumber.slice(3, 6)}`;
  }
  if (nationalNumber.length > 6) {
    formatted += `-${nationalNumber.slice(6, 8)}`;
  }
  if (nationalNumber.length > 8) {
    formatted += `-${nationalNumber.slice(8, 10)}`;
  }
  return formatted;
}
