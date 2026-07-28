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
