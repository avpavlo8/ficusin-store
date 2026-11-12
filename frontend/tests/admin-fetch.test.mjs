import assert from "node:assert/strict";
import test from "node:test";
import { adminFetch } from "../src/lib/admin-fetch.ts";

test("a temporary gateway failure retries a read and returns fresh data", async () => {
  let calls = 0;
  const response = await adminFetch("/api/categories", {}, async () =>
    ++calls === 1 ? new Response("", { status: 502 }) : Response.json({ categories: [14] }),
  );
  assert.equal(calls, 2);
  assert.deepEqual(await response.json(), { categories: [14] });
});
test("a connection reset retries GET but persistent failures are bounded", async () => {
  let calls = 0;
  await adminFetch("/api/categories", {}, async () => {
    if (++calls === 1) throw new TypeError("Failed to fetch");
    return new Response("ok");
  });
  assert.equal(calls, 2);
  calls = 0;
  const response = await adminFetch("/api/categories", {}, async () => {
    calls++;
    return new Response("", { status: 503 });
  });
  assert.equal(calls, 3);
  assert.equal(response.status, 503);
});
test("writes are never replayed after a server or connection failure", async () => {
  for (const method of ["POST", "PATCH", "PUT", "DELETE"]) {
    let calls = 0;
    assert.equal(
      (
        await adminFetch("/api/product", { method }, async () => {
          calls++;
          return new Response("", { status: 502 });
        })
      ).status,
      502,
    );
    assert.equal(calls, 1);
    calls = 0;
    await assert.rejects(
      adminFetch("/api/product", { method }, async () => {
        calls++;
        throw new TypeError("Failed to fetch");
      }),
    );
    assert.equal(calls, 1);
  }
});
test("authorization, validation and missing data errors do not retry", async () => {
  for (const status of [400, 401, 403, 404, 405]) {
    let calls = 0;
    await adminFetch("/api/categories", {}, async () => {
      calls++;
      return new Response("", { status });
    });
    assert.equal(calls, 1);
  }
});
test("closing the editor cancels queued retries", async () => {
  const controller = new AbortController();
  let calls = 0;
  const request = adminFetch("/api/media", { signal: controller.signal }, async () => {
    calls++;
    return new Response("", { status: 502 });
  });
  setTimeout(() => controller.abort(), 10);
  await assert.rejects(request, { name: "AbortError" });
  assert.equal(calls, 1);
});
