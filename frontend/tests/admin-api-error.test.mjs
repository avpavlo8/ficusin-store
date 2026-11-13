import assert from "node:assert/strict";
import test from "node:test";
import { adminApiError } from "../src/lib/admin-api-error.ts";

test("generic server failures retain the operation, status and correlation id", () => {
  const error = adminApiError(
    503,
    "/api/v1/admin/categories/14/attributes",
    "GET",
    "Не удалось выполнить операцию",
    "request-123",
  );
  assert.match(error.message, /HTTP 503/);
  assert.match(error.message, /GET \/api\/v1\/admin\/categories\/14\/attributes/);
  assert.match(error.message, /request-123/);
});
test("missing endpoints are distinguishable from validation failures", () => {
  assert.match(
    adminApiError(405, "/api/v1/admin/variants/81/media/copy", "POST").message,
    /не поддерживает действие/,
  );
  assert.match(
    adminApiError(400, "/api/v1/admin/products/81", "PATCH", "Укажите название").message,
    /Укажите название/,
  );
});
test("diagnostics exclude query parameters and never repeat an HTML response", () => {
  const error = adminApiError(502, "/api/test?private=value#fragment");
  assert.doesNotMatch(error.message, /private|value|fragment/);
  assert.match(error.message, /HTTP 502, GET \/api\/test/);
  assert.match(adminApiError(200, "/api/test").message, /некорректный ответ/);
});
