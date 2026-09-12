import { gzipSync } from "node:zlib";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("../dist/", import.meta.url).pathname;
const limits = {
  entryJS: 105 * 1024,
  css: 45 * 1024,
  // CRM stage 05 adds the auditable recommendation and supplier follow-up UI
  // to the already lazy-loaded admin chunk. Keep the allowance tight around
  // that isolated chunk rather than moving admin code into the public entry.
  asyncJS: 38 * 1024,
  staticAsset: 900 * 1024,
};

const failures = [];
function files(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    return statSync(path).isDirectory() ? files(path) : [path];
  });
}

for (const path of files(root)) {
  const name = relative(root, path);
  const bytes = readFileSync(path);
  const gzip = gzipSync(bytes).length;
  let maximum = limits.staticAsset;
  let measured = bytes.length;
  if (/^assets\/index-.*\.js$/.test(name)) { maximum = limits.entryJS; measured = gzip; }
  else if (name.endsWith(".css")) { maximum = limits.css; measured = gzip; }
  else if (name.endsWith(".js")) { maximum = limits.asyncJS; measured = gzip; }
  if (measured > maximum) failures.push(`${name}: ${measured} bytes, budget ${maximum}`);
}

if (failures.length) {
  console.error(`Bundle budget exceeded:\n${failures.join("\n")}`);
  process.exit(1);
}
console.log("Bundle budgets satisfied.");
