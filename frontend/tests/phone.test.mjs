import assert from "node:assert/strict";
import test from "node:test";
import { formatRussianPhoneInput, normalizeRussianPhone } from "../src/lib/phone.ts";

test("explicit +7 needs all ten national digits", () => {
  for (const national of ["", "91512345", "915123456"]) {
    assert.equal(normalizeRussianPhone(`+7 ${national}`), null);
    assert.equal(normalizeRussianPhone(formatRussianPhoneInput(national)), null);
  }
  assert.equal(normalizeRussianPhone("+7 915 123-45-67"), "+79151234567");
  assert.equal(normalizeRussianPhone("+7 91512345678"), null);
});

test("complete national and domestic numbers remain supported", () => {
  for (const phone of ["9151234567", "79151234567", "89151234567", "+79151234567"]) {
    assert.equal(normalizeRussianPhone(phone), "+79151234567");
    assert.equal(normalizeRussianPhone(formatRussianPhoneInput(phone)), "+79151234567");
  }
  assert.equal(normalizeRussianPhone("915123456"), null);
  assert.equal(normalizeRussianPhone("+49151234567"), null);
});

test("typing and deleting the tenth digit changes validity at the boundary", () => {
  const incomplete = formatRussianPhoneInput("915123456");
  const complete = formatRussianPhoneInput(`${incomplete}7`);
  const deleted = formatRussianPhoneInput(complete.slice(0, -1));
  assert.equal(normalizeRussianPhone(incomplete), null);
  assert.equal(normalizeRussianPhone(complete), "+79151234567");
  assert.equal(normalizeRussianPhone(deleted), null);
});
