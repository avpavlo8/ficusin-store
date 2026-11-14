// Runtime graph for Vite's main.tsx entry. Type-only imports do not keep UI
// alive. External packages and CSS are outside this graph; computed imports
// require manual review because their target cannot be resolved statically.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

export function analyzeRuntimeModules({ files, entry, readFile, resolveModule }) {
  const modules = new Map();
  const computedImports = [];
  for (const file of files) {
    if (file.endsWith(".d.ts")) continue;
    const source = ts.createSourceFile(file, readFile(file), ts.ScriptTarget.Latest, true);
    const dependencies = [];
    let hasRuntimeLink = false;
    const add = (specifier) => {
      hasRuntimeLink = true;
      const target = resolveModule(specifier, file);
      if (target) dependencies.push(target);
    };
    const visit = (node) => {
      if (ts.isImportDeclaration(node)) {
        const clause = node.importClause;
        const named = clause?.namedBindings;
        if (
          !clause?.isTypeOnly &&
          !(
            clause &&
            !clause.name &&
            named &&
            ts.isNamedImports(named) &&
            named.elements.length &&
            named.elements.every((item) => item.isTypeOnly)
          )
        )
          add(node.moduleSpecifier.text);
      }
      if (ts.isExportDeclaration(node) && node.moduleSpecifier && !node.isTypeOnly) {
        const named = node.exportClause;
        if (
          !(
            named &&
            ts.isNamedExports(named) &&
            named.elements.length &&
            named.elements.every((item) => item.isTypeOnly)
          )
        )
          add(node.moduleSpecifier.text);
      }
      if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
        const argument = node.arguments[0];
        if (
          argument &&
          (ts.isStringLiteral(argument) || ts.isNoSubstitutionTemplateLiteral(argument))
        )
          add(argument.text);
        else computedImports.push(file);
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
    const executable = source.statements.some((node) => {
      if (ts.getCombinedModifierFlags(node) & ts.ModifierFlags.Ambient) return false;
      if (ts.isFunctionDeclaration(node)) return Boolean(node.body);
      return !(
        ts.isImportDeclaration(node) ||
        ts.isExportDeclaration(node) ||
        ts.isInterfaceDeclaration(node) ||
        ts.isTypeAliasDeclaration(node) ||
        ts.isEmptyStatement(node)
      );
    });
    modules.set(file, { dependencies, executable: executable || hasRuntimeLink });
  }
  const reachable = new Set();
  const visit = (file) => {
    if (reachable.has(file)) return;
    reachable.add(file);
    for (const dependency of modules.get(file)?.dependencies || []) visit(dependency);
  };
  visit(entry);
  return {
    unreachable: [...modules]
      .filter(([file, value]) => value.executable && !reachable.has(file))
      .map(([file]) => file)
      .sort(),
    computedImports: [...new Set(computedImports)].sort(),
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const configPath = resolve(root, "tsconfig.app.json");
  const config = ts.readConfigFile(configPath, ts.sys.readFile);
  if (config.error)
    throw new Error(ts.flattenDiagnosticMessageText(config.error.messageText, "\n"));
  const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, root);
  if (parsed.errors.length)
    throw new Error(ts.flattenDiagnosticMessageText(parsed.errors[0].messageText, "\n"));
  const files = parsed.fileNames.map((file) => resolve(file));
  const owned = new Set(files);
  const result = analyzeRuntimeModules({
    files,
    entry: resolve(root, "src/main.tsx"),
    readFile: (file) => readFileSync(file, "utf8"),
    resolveModule: (specifier, from) => {
      const target = ts.resolveModuleName(specifier, from, parsed.options, ts.sys).resolvedModule
        ?.resolvedFileName;
      return target && owned.has(resolve(target)) ? resolve(target) : null;
    },
  });
  if (result.computedImports.length)
    console.warn("Computed imports need manual reachability review:", result.computedImports);
  if (result.unreachable.length) {
    console.error(
      "Executable source modules unreachable from main.tsx:\n" + result.unreachable.join("\n"),
    );
    process.exitCode = 1;
  } else
    console.log(
      `Runtime module graph checked: ${files.length} source files, no orphan executable modules.`,
    );
}
