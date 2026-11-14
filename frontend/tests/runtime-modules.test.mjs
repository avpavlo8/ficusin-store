import { strict as assert } from "node:assert";
import { test } from "node:test";
import { posix as path } from "node:path";
import { analyzeRuntimeModules } from "../scripts/check-runtime-modules.mjs";

function analyze(input) {
  const sources = new Map(Object.entries(input).map(([name, text]) => ["/src/" + name, text]));
  return analyzeRuntimeModules({
    files: [...sources.keys()],
    entry: "/src/main.tsx",
    readFile: (file) => sources.get(file),
    resolveModule: (specifier, from) => {
      if (!specifier.startsWith(".")) return null;
      const base = path.resolve(path.dirname(from), specifier);
      return [base, base + ".ts", base + ".tsx"].find((file) => sources.has(file));
    },
  });
}

test("type-only references cannot keep a retired component alive, while pure types need no runtime entry", () => {
  const result = analyze({
    "main.tsx": 'import type { Draft } from "./retired"; import { type Shape } from "./types";',
    "retired.tsx": "export type Draft = {}; export function Retired() { return null; }",
    "types.ts":
      "export interface Shape { name: string }; export type ID = number; declare const env: string;",
  });
  assert.deepEqual(result.unreachable, ["/src/retired.tsx"]);
});

test("runtime reexports and lazy imports preserve live modules, including mixed import specifiers", () => {
  const result = analyze({
    "main.tsx":
      'import { type Shape, Live } from "./barrel"; import("./lazy"); import(`./template`);',
    "barrel.ts": 'export { type Shape, Live } from "./live";',
    "live.tsx": "export interface Shape {}; export const Live = () => null;",
    "lazy.tsx": "export default function Lazy() { return null; }",
    "template.tsx": "export const ready = true;",
  });
  assert.deepEqual(result, { unreachable: [], computedImports: [] });
});

test("type-only reexports do not execute modules; export star does", () => {
  const result = analyze({
    "main.tsx": 'import "./barrel";',
    "barrel.ts": 'export { type Shape } from "./retired"; export * from "./live";',
    "retired.tsx": "export type Shape = {}; export const retired = true;",
    "live.ts": "export const live = true;",
  });
  assert.deepEqual(result.unreachable, ["/src/retired.tsx"]);
});

test("computed imports are reported as a static analysis limit", () => {
  const result = analyze({ "main.tsx": "const name = location.hash; import(name);" });
  assert.deepEqual(result.computedImports, ["/src/main.tsx"]);
});

// A module can execute effects without declaring any local values.
test("unreachable import-only and reexport-only modules are runtime modules", () => {
  const result = analyze({
    "main.tsx": "export type Live = string;",
    "effect.ts": 'import "external-effect-package";',
    "barrel.ts": 'export * from "./effect";',
  });
  assert.deepEqual(result.unreachable, ["/src/barrel.ts", "/src/effect.ts"]);
});
