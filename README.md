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
cd frontend && npm run lint && npm run build
cd backend && go test ./... && go vet ./...
docker build -t ficusin-store .
```

## Документация

Перед изменением проекта прочитайте [`AGENTS.md`](AGENTS.md).

- [Продукт и бизнес-правила](docs/PRODUCT.md)
- [Архитектура](docs/ARCHITECTURE.md)
- [Модель данных](docs/DATA_MODEL.md)
- [Контракт API](docs/openapi.yaml)
- [Интеграции](docs/INTEGRATIONS.md)
- [Развёртывание](docs/DEPLOYMENT.md)
- [Дорожная карта](docs/ROADMAP.md)
- [Журнал решений](docs/DECISIONS.md)
- [Нерешённые вопросы](docs/OPEN_QUESTIONS.md)
- [История разделения frontend/backend](docs/split-architecture.md)

В репозитории нет второго server runtime: production-сборка и локальная
разработка используют один и тот же Go API и один React-клиент.
