# Фикусин

Интернет-магазин растений с отдельными клиентским приложением и API.

```text
frontend/  React + TypeScript + Vite
backend/   Go HTTP API
timeweb/   SQL-миграции PostgreSQL
```

В production используется один контейнер: Go обслуживает `/api/v1/*` и
собранные Vite статические файлы. Это сохраняет один тариф Timeweb, но код,
зависимости и секреты frontend/backend полностью разделены.

## Локальный запуск

Требуются Node.js 24, Go 1.26 и PostgreSQL.

```bash
# frontend, http://localhost:5173
cd frontend
npm ci
npm run dev

# backend, http://localhost:8080
cd backend
DATABASE_URL='postgresql://...' \
MIGRATIONS_DIR='../timeweb/migrations' \
AUTH_COOKIE_SECURE=false \
go run ./cmd/api
```

Vite проксирует запросы `/api` на Go по адресу `http://localhost:8080`.

## Проверка

```bash
cd frontend && npm run lint && npm run lint:css && npm run build
cd backend && go test ./... && go vet ./...
cd e2e && npm install && npx playwright install && npm test
docker build -t ficusin-store .
```

Архитектурные правила и порядок переноса описаны в
[`docs/split-architecture.md`](docs/split-architecture.md).

В репозитории нет второго серверного runtime: production-сборка и локальная
разработка используют один и тот же Go API и один React-клиент.
