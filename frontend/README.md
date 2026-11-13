# Ficusin Web

The browser application has no direct access to PostgreSQL, Saby, CDEK,
Telegram, or payment credentials. It communicates only with the versioned Go
API under `/api/v1`.

## Local commands

```bash
npm install
npm run dev
npm run build
```

During development, Vite proxies `/api` requests to `http://localhost:8080`.

### Runtime module cleanup guard

`npm run lint:modules` verifies executable source modules are reachable from
`src/main.tsx`. It follows runtime imports, reexports and literal dynamic imports,
while excluding type-only references and pure type modules. This prevents a
retired UI component from surviving solely because another file imports its type.
The guard covers this Vite entry only, not external packages, CSS selectors or
individual unused exports. Computed import targets are reported for manual review.
Its virtual-source regression fixtures run as part of ordinary frontend lint.
