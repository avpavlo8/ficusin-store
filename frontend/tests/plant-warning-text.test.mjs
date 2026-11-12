import assert from "node:assert/strict";
import test from "node:test";
import { bulletWarningText, warningLines, deleteWarningLine } from "../src/plantWarningText.ts";
test("pasted and existing warning lines have exactly one bullet; stored payload has none", () => {
  const text = "• Первый пункт\r\n- Второй пункт\nТретий пункт";
  assert.equal(bulletWarningText(text), "• Первый пункт\n• Второй пункт\n• Третий пункт");
  assert.equal(warningLines(bulletWarningText(text)), "Первый пункт\nВторой пункт\nТретий пункт");
});
test("a new line immediately has a bullet while a blank point stays editable", () => {
  assert.equal(bulletWarningText("Первый\n"), "• Первый\n• ");
  assert.equal(warningLines("• Первый\n• "), "Первый\n");
  assert.equal(bulletWarningText(""), "• ");
});

test("Backspace removes an empty bullet line in one press; Delete joins without a stray bullet", () => {
  const text = "• Первый\n• ";
  assert.deepEqual(deleteWarningLine(text, text.length, text.length, "Backspace"), {
    value: "• Первый",
    cursor: 8,
  });
  const two = "• Первый\n• Второй";
  assert.deepEqual(deleteWarningLine(two, 8, 8, "Delete"), { value: "• ПервыйВторой", cursor: 8 });
  assert.equal(deleteWarningLine(two, 5, 5, "Backspace"), null);
  assert.equal(deleteWarningLine(two, 0, 3, "Backspace"), null);
});
