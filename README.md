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
Frontend и backend запускаются в двух терминалах из корня репозитория.

```bash
# frontend, http://localhost:5173
cd frontend
npm ci
npm run dev

# backend, http://localhost:8080
cd backend
DATABASE_URL='postgresql://...' \
APP_ENV=development \
MIGRATIONS_DIR='../timeweb/migrations' \
AUTH_COOKIE_SECURE=false \
PROCUREMENT_PDF_FONT='/System/Library/Fonts/Supplemental/Arial.ttf' \
go run ./cmd/api
```

Vite проксирует запросы `/api` на Go по адресу `http://localhost:8080`.
Путь шрифта выше — пример для macOS. Для локальных PDF и Go-проверок укажите
существующий TTF через `PROCUREMENT_PDF_FONT`; на Linux выберите установленный
шрифт. Docker уже включает DejaVu и использует `/app/fonts/DejaVuSans.ttf`.

## Проверка

```bash
(cd frontend && npm test && npm run lint && npm run build)
(cd backend && PROCUREMENT_PDF_FONT='/System/Library/Fonts/Supplemental/Arial.ttf' go test ./... && go vet ./...)
(cd e2e && npm ci && npx playwright install && npm test)
docker build -t ficusin-store .
```

Архитектурные правила и порядок переноса описаны в
[`docs/split-architecture.md`](docs/split-architecture.md).

В репозитории нет второго серверного runtime: production-сборка и локальная
разработка используют один и тот же Go API и один React-клиент.

## Как разобраться в системе

Цены внешних площадок готовятся и подтверждаются в общем редакторе товара;
[описание публикации цен и восстановления заданий](docs/editor-price-publications.md).
Закупочные расчёты и предпросмотр цен с явными режимами `cost`/`retail`
описаны в [документации закупок](docs/procurement.md).
Фоновые закупочные задания используют сохранённые очереди PostgreSQL;
отдельного планировщика внешних продаж в памяти процесса нет.

Начните с [актуальной карты функций и процессов](docs/current-system.md),
затем прочитайте [правила работы с репозиторием](AGENTS.md).
Карта отделяет рабочие функции, переходную совместимость и исторические документы.
HTTP-контракт находится в [OpenAPI](docs/openapi.yaml).
Оплата запускается через расчёт текущей задолженности заказа; прежние внутренние
`payment.Start`/`Sync` удалены. Редактирование карточки и фотографий из PDP ведёт
в общий редактор товара. Оставшиеся пути чтения исторических данных и условия
их снятия описаны в [границах совместимости](docs/remaining-compatibility.md).
Локально подготовлен [перенос оставшихся данных](docs/legacy-data-transition.md):
архивы с проверкой точности, V2-черновики с backup, отдельные артикулы площадок
и барьер нулевых резервов. Неперенесённые данные блокируют миграцию;
неизвестные внешние результаты не сбрасываются.
Полное покрытие последней проверки и сохранённые границы — в
[аудите всей кодовой базы](docs/whole-codebase-audit.md).

При окончательном отключении функции удаляются её код, настройки, фоновые
задания, устаревшие тесты и проверки, скрипты и ассеты; документация обновляется
в том же изменении. Правило и требования к сохранению данных закреплены в AGENTS.md.

Работы по очистке ведутся в отдельной ветке. Слияние в `main` запускает деплой
и автоматическое применение новых миграций; локальная проверка не означает,
что production уже обновлён.
