// Мёртвые CSS-классы накапливаются незаметно: страницу переписали, правила
// остались. К августу 2026 в styles.css было 40 таких классов и около 200
// строк, которые ехали в каждый бандл. Проверка ловит их на CI.
//
// Классы, которые собираются в рантайме, статикой не найти, поэтому они
// перечислены явными префиксами ниже. Добавляете новый динамический класс —
// впишите префикс сюда, иначе проверка упадёт.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const stylesheet = join(root, "frontend", "src", "styles.css");

// Префиксы классов, которые склеиваются из переменных.
const dynamicPrefixes = ["sales-sync-", "admin-pill", "procurement-"];

const sources = [];
const walk = (directory, extensions) => {
  for (const entry of readdirSync(directory)) {
    if (entry === "node_modules" || entry === "dist") continue;
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) walk(path, extensions);
    else if (extensions.some((extension) => entry.endsWith(extension))) sources.push(path);
  }
};
walk(join(root, "frontend", "src"), [".ts", ".tsx"]);
walk(join(root, "e2e", "tests"), [".ts"]);
walk(join(root, "backend", "internal"), [".go"]);
sources.push(join(root, "frontend", "index.html"));
walk(join(root, "public"), [".html", ".webmanifest"]);

const usage = sources
  .filter((path) => !path.endsWith("styles.css"))
  .map((path) => readFileSync(path, "utf8"))
  .join("\n");

const css = readFileSync(stylesheet, "utf8");
const declared = new Set();
for (const match of css.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) declared.add(match[1]);

const dead = [...declared]
  .filter((name) => !dynamicPrefixes.some((prefix) => name.startsWith(prefix)))
  .filter((name) => !new RegExp(`(^|[^\\w-])${name}([^\\w-]|$)`).test(usage))
  .sort();

if (dead.length > 0) {
  console.error(`Мёртвые CSS-классы в frontend/src/styles.css (${dead.length}):`);
  for (const name of dead) console.error(`  .${name}`);
  console.error("\nУдалите правила или, если класс собирается динамически,");
  console.error("добавьте его префикс в dynamicPrefixes в этом скрипте.");
  process.exit(1);
}
console.log(`CSS-классов проверено: ${declared.size}, мёртвых нет.`);
