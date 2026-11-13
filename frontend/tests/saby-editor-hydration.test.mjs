import assert from "node:assert/strict";
import test from "node:test";
import { mergeSabyEditor } from "../src/sabyEditorHydration.ts";

const category = (id) => ({ id, name: `Category ${id}` });
const empty = () => ({
  category: { id: "", name: "" },
  folder: { id: "", name: "" },
  mode: "create",
  existingCode: "",
});
const linked = (id = "1101", code = "X1001") => ({
  id,
  code,
  category: category(`c-${id}`),
  folder: category(`f-${id}`),
  mirrorFound: true,
});

test("existing link hydrates a legacy empty create default using its code, never its internal ID", () => {
  const actual = linked();
  assert.deepEqual(mergeSabyEditor({ local: empty(), saved: empty(), linked: actual }), {
    mode: "link",
    existingCode: "X1001",
    category: actual.category,
    folder: actual.folder,
  });
  assert.equal(mergeSabyEditor({ linked: { ...actual, code: "" } }).existingCode, "");
});

test("missing mirror keeps known canonical code and does not invent category or folder", () => {
  const actual = {
    ...linked(),
    category: { id: "", name: "" },
    folder: { id: "", name: "" },
    mirrorFound: false,
  };
  assert.deepEqual(
    mergeSabyEditor({ linked: actual, defaults: { category: category("other-variant") } }),
    {
      ...empty(),
      mode: "link",
      existingCode: "X1001",
    },
  );
});

test("explicit create and cleared fields survive a late actual-link response", () => {
  const local = empty();
  const value = mergeSabyEditor({
    local,
    saved: { ...empty(), category: category("planned"), existingCode: "XPLAN" },
    linked: linked(),
    touched: { mode: true, existingCode: true, category: true },
  });
  assert.equal(value.mode, "create");
  assert.equal(value.existingCode, "");
  assert.deepEqual(value.category, { id: "", name: "" });
  assert.deepEqual(value.folder, linked().folder);
});

test("newly entered code and per-field plan values win without blocking other metadata", () => {
  const local = { ...empty(), mode: "link", existingCode: "XNEW", category: category("local") };
  const saved = { ...empty(), category: category("plan"), folder: category("plan-folder") };
  const before = JSON.stringify({ local, saved });
  const next = mergeSabyEditor({ local, saved, linked: linked(), touched: { existingCode: true } });
  assert.equal(next.existingCode, "XNEW");
  assert.deepEqual(next.category, category("local"));
  assert.deepEqual(next.folder, category("plan-folder"));
  assert.equal(JSON.stringify({ local, saved }), before);
});

test("nonempty create plans preserve their future intent and local empty defaults do not hide them", () => {
  const saved = { ...empty(), category: category("planned-create") };
  assert.equal(mergeSabyEditor({ local: empty(), saved, linked: linked() }).mode, "create");
  assert.equal(
    mergeSabyEditor({ local: { ...empty(), folder: category("local-create") }, linked: linked() })
      .mode,
    "create",
  );
});

test("two variants resolve their own link and a late response preserves the latest edit", () => {
  const links = { 10: linked("1101", "XONE"), 11: linked("1102", "XTWO") };
  const rows = [10, 11].map((serverId) => ({ serverId, sabyEditor: empty() }));
  const hydrated = rows.map((row) =>
    mergeSabyEditor({ local: row.sabyEditor, linked: links[String(row.serverId)] }),
  );
  assert.deepEqual(
    hydrated.map((value) => value.existingCode),
    ["XONE", "XTWO"],
  );
  const edited = { ...hydrated[1], existingCode: "XUSER" };
  assert.equal(
    mergeSabyEditor({ local: edited, linked: links["11"], touched: { existingCode: true } })
      .existingCode,
    "XUSER",
  );
  assert.equal(
    mergeSabyEditor({ defaults: { category: category("new-card") } }).category.id,
    "new-card",
  );
});
