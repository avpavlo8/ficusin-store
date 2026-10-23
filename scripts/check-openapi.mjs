// Syntax validation and bidirectional route coverage. No app/server/provider calls.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const require = createRequire(new URL("../frontend/package.json", import.meta.url));
const YAML = require("yaml");
const SwaggerParser = require("@apidevtools/swagger-parser");
const spec = YAML.parse(readFileSync(new URL("../docs/openapi.yaml", import.meta.url), "utf8"));
const inventory = JSON.parse(execFileSync("go", ["run", "./cmd/api-contract"], {
  cwd: `${root}backend`, encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
}));
const methods = new Set(["get", "post", "put", "patch", "delete", "head", "options"]);
const expected = new Set(inventory.routes.map(route => `${route.method.toLowerCase()} ${route.path.startsWith("/api/v1/") ? route.path.slice(7) : route.path}`));
const documented = new Set();
const operationIDs = new Set();
for (const [path, item] of Object.entries(spec.paths)) {
  for (const [method, operation] of Object.entries(item)) {
    if (!methods.has(method)) continue;
    const key = `${method} ${path}`;
    documented.add(key);
    assert(expected.has(key), `OpenAPI documents an unregistered route: ${key}`);
    assert(operation.operationId && !operationIDs.has(operation.operationId), `Missing/duplicate operationId: ${key}`);
    operationIDs.add(operation.operationId);
    assert(Array.isArray(operation.security), `Explicit security declaration required: ${key}`);
    assert(operation["x-source"], `Source handler required: ${key}`);
    const parameters = [...item.parameters ?? [], ...operation.parameters ?? []];
    for (const [, name] of path.matchAll(/\{([^}]+)\}/g)) {
      assert(parameters.some(p => p.in === "path" && p.name === name && p.required), `Missing required path parameter ${name}: ${key}`);
    }
    if (path.startsWith("/admin/")) assert(operation.security.some(s => s.Session), `Admin authentication missing: ${key}`);
  }
}
for (const key of expected) assert(documented.has(key), `Registered route missing in OpenAPI: ${key}`);
// Refuse external refs before invoking a resolver. Contract validation is offline.
function checkRefs(value) {
  if (!value || typeof value !== "object") return;
  if (value.$ref) assert(value.$ref.startsWith("#/"), `External ref is not allowed: ${value.$ref}`);
  for (const child of Object.values(value)) checkRefs(child);
}
checkRefs(spec);
await SwaggerParser.validate(structuredClone(spec), { resolve: { external: false } });
// Hand-edited critical request contracts must include every current wire field.
for (const [method,path] of [["POST","/orders"],["POST","/auth/request-code"],["POST","/auth/verify-code"],["POST","/admin/orders/{id}/refund"]]) {
  const route=inventory.routes.find(r=>r.method===method&&r.path===`/api/v1${path}`);
  let source=route.request[0];
  if(source.$ref) source=inventory.schemas[source.$ref.split("/").at(-1)];
  const documented=spec.paths[path][method.toLowerCase()].requestBody.content["application/json"].schema;
  assert.deepEqual(Object.keys(documented.properties).sort(),Object.keys(source.properties).sort(),`Request fields drifted: ${method} ${path}`);
}
const order = spec.paths["/orders"].post.requestBody.content["application/json"].schema;
assert(order.required.includes("consent") && order.required.includes("offerAccepted"), "Independent checkout confirmations required");
const refund = spec.paths["/admin/orders/{id}/refund"].post;
assert(refund["x-permissions"].includes("payments.refund"), "Refund permission missing");
assert(refund.requestBody.content["application/json"].schema.required.includes("requestId"), "Durable refund request ID required");
const payment = spec.paths["/payments/orders/{orderNumber}"].post;
assert(payment.security.some(s => s.Session) && payment.security.some(s => s.GuestOrderPayment), "Order owner / guest capability alternatives required");
assert.equal(spec.components.schemas.RUBAmount.multipleOf, .01, "RUB wire contract must preserve kopecks");
console.log(`OpenAPI valid: ${expected.size}/${expected.size} registered operations, ${Object.keys(spec.components.schemas).length} schemas; commerce authorization and consent contracts checked.`);
