import assert from "node:assert/strict";
import test from "node:test";
import { readDraftMedia, saveDraftMedia } from "../src/productCreationDraft.ts";

function storage(t) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
  const entries = new Map();
  const state = { fail: false, opens: [], entries };
  Object.defineProperty(globalThis, "indexedDB", {
    configurable: true,
    value: {
      open(name, version) {
        state.opens.push([name, version]);
        const request = {
          result: {
            close() {},
            transaction() {
              const tx = {},
                staged = new Map(entries);
              let pending = 0,
                completed = false,
                aborted = false;
              const schedule = (fn) => {
                pending++;
                queueMicrotask(() => {
                  if (!aborted) fn();
                  pending--;
                  queueMicrotask(() => {
                    if (!pending && !completed && !aborted) {
                      completed = true;
                      entries.clear();
                      for (const [key, value] of staged) entries.set(key, value);
                      tx.oncomplete?.();
                    }
                  });
                });
              };
              let putCount = 0;
              tx.objectStore = () => ({
                put(value, key) {
                  if (value instanceof Blob)
                    throw new DOMException("Error preparing Blob/File data", "UnknownError");
                  const result = {};
                  const ordinal = ++putCount;
                  schedule(() => {
                    if (state.fail || state.failPut === ordinal) {
                      aborted = true;
                      tx.error = result.error = new DOMException(
                        "Quota exhausted",
                        "QuotaExceededError",
                      );
                      result.onerror?.();
                      tx.onerror?.();
                      tx.onabort?.();
                    } else {
                      staged.set(key, structuredClone(value));
                      result.onsuccess?.();
                    }
                  });
                  return result;
                },
                get(key) {
                  const result = {};
                  schedule(() => {
                    result.result = staged.get(key);
                    result.onsuccess?.();
                  });
                  return result;
                },
              });
              return tx;
            },
          },
        };
        queueMicrotask(() => request.onsuccess?.());
        return request;
      },
    },
  });
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, "indexedDB", previous);
    else delete globalThis.indexedDB;
  });
  return state;
}

test("draft files retain exact bytes and MIME without relying on IndexedDB Blob support", async (t) => {
  const db = storage(t);
  const bytes = new Uint8Array([0, 255, 137, 80, 78, 71, 0, 1]);
  for (const mime of ["image/png", "image/webp", "video/mp4"]) {
    await saveDraftMedia(mime, new Blob([bytes], { type: mime }));
    assert.equal(db.entries.get(mime).version, 1);
    assert.ok(db.entries.get(mime).bytes instanceof ArrayBuffer);
    // A new read opens its own database handle, as after an editor reload.
    const restored = await readDraftMedia(mime);
    assert.ok(restored instanceof Blob);
    assert.equal(restored.type, mime);
    assert.deepEqual(new Uint8Array(await restored.arrayBuffer()), bytes);
  }
  assert.ok(
    db.opens.every(([name, version]) => name === "ficusin-product-drafts" && version === 1),
  );
});

test("legacy Blob drafts remain readable and malformed binary records fail explicitly", async (t) => {
  const db = storage(t);
  const old = new Blob([new Uint8Array([17, 0, 255])], { type: "image/jpeg" });
  db.entries.set("old", old);
  const converted = await readDraftMedia("old");
  assert.deepEqual(
    new Uint8Array(await converted.arrayBuffer()),
    new Uint8Array(await old.arrayBuffer()),
  );
  assert.equal(converted.type, old.type);
  assert.equal(db.entries.get("old").version, 1);
  assert.deepEqual(
    new Uint8Array(db.entries.get("migration-backup:v1:old").bytes),
    new Uint8Array(await old.arrayBuffer()),
  );
  assert.equal(await readDraftMedia("missing"), undefined);
  db.entries.set("broken", { version: 1, type: "image/png", bytes: "not binary" });
  await assert.rejects(readDraftMedia("broken"), /Файл черновика повреждён/);
});

test("failed quota writes reject and preserve existing draft bytes", async (t) => {
  const db = storage(t);
  await saveDraftMedia("file", new Blob(["original"], { type: "image/png" }));
  db.fail = true;
  await assert.rejects(saveDraftMedia("file", new Blob(["changed"], { type: "image/png" })), {
    name: "QuotaExceededError",
  });
  assert.equal(await (await readDraftMedia("file")).text(), "original");
});

test("a failed lazy Blob conversion rolls back backup and replacement together", async (t) => {
  const db = storage(t),
    original = new Blob(["precious file"], { type: "image/webp" });
  db.entries.set("old", original);
  db.failPut = 2;
  await assert.rejects(readDraftMedia("old"), { name: "QuotaExceededError" });
  assert.equal(db.entries.get("old"), original);
  assert.equal(db.entries.has("migration-backup:v1:old"), false);
});

test("an existing media backup is never overwritten by another legacy source", async (t) => {
  const db = storage(t),
    original = new Blob(["current file"], { type: "image/png" });
  const earlier = {
    version: 1,
    bytes: new TextEncoder().encode("earlier").buffer,
    type: "image/png",
  };
  db.entries.set("old", original);
  db.entries.set("migration-backup:v1:old", earlier);
  assert.equal(await (await readDraftMedia("old")).text(), "current file");
  assert.equal(db.entries.get("old"), original);
  assert.equal(db.entries.get("migration-backup:v1:old"), earlier);
});
