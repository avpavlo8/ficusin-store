// Only runtime markup can keep CSS alive. Negative assertions and screenshots
// in tests are not consumers; selectors are parsed separately from declarations.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import postcss from "postcss";

// Exact dynamic families used by ReturnState, procurement reconciliation,
// marketplace sync and account shipment offers. Do not exempt whole UI domains.
const dynamicClasses = [
  /^receipt-(checking|correction_required|draft_created|failed|posted|queued)$/,
  /^sales-sync-(error|ok|running)$/,
  /^sync-(ok|queued|running)$/,
  /^procurement-reconciliation-(added|changed|excluded|matched|missing)$/,
  /^account-shipment-offer-(expired|stale)$/,
];

const isRuntimeSource = (path) =>
  !/(^|[/\\])(tests?|e2e|__tests__)([/\\]|$)/.test(path) &&
  !/\.(test|spec)\.[^.]+$/.test(path) &&
  !/_test\.go$/.test(path);

export function analyzeCssUsage({ styles, sources }) {
  const usage = sources
    .filter(({ path }) => isRuntimeSource(path))
    .map(({ text }) => text)
    .join("\n");
  const declared = new Set();
  for (const { path, text } of styles) {
    postcss.parse(text, { from: path }).walkRules((rule) => {
      // Quoted attribute values can contain URLs; they are not class selectors.
      const selector = rule.selector.replace(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'/g, "");
      for (const match of selector.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) declared.add(match[1]);
    });
  }
  const dead = [...declared]
    .filter((name) => !dynamicClasses.some((pattern) => pattern.test(name)))
    .filter((name) => !new RegExp(`(^|[^\\w-])${name}([^\\w-]|$)`).test(usage))
    .sort();
  return { declared: declared.size, dead };
}

export function checkCssTree(root) {
  const walk = (directory, extensions) => {
    const files = [];
    for (const entry of readdirSync(directory)) {
      if (["node_modules", "dist", ".git", "tests", "__tests__"].includes(entry)) continue;
      const path = join(directory, entry);
      if (statSync(path).isDirectory()) files.push(...walk(path, extensions));
      else if (extensions.some((extension) => entry.endsWith(extension))) files.push(path);
    }
    return files;
  };
  const read = (path) => ({ path, text: readFileSync(path, "utf8") });
  const sources = [
    ...walk(join(root, "frontend", "src"), [".ts", ".tsx"]),
    ...walk(join(root, "backend", "internal"), [".go"]),
    ...walk(join(root, "public"), [".html", ".webmanifest", ".js"]),
    join(root, "frontend", "index.html"),
  ].map(read);
  const styles = walk(join(root, "frontend", "src"), [".css"]).map(read);
  return analyzeCssUsage({ styles, sources });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
  const result = checkCssTree(root);
  if (result.dead.length) {
    console.error(`Мёртвые CSS-классы в frontend/src/ (${result.dead.length}):`);
    for (const name of result.dead) console.error(`  .${name}`);
    console.error(
      "Удалите правила; для нового динамического статуса подтвердите runtime consumer.",
    );
    process.exitCode = 1;
  } else console.log(`CSS-классов проверено: ${result.declared}, мёртвых нет.`);
}
