# Развёртывание и эксплуатация

## Production

- Репозиторий: `avpavlo8/ficusin-store`.
- Ветка production: `main`.
- Домен: `ficusin.ru` и `www.ficusin.ru`.
- Платформа: Timeweb Cloud App.
- База: управляемый PostgreSQL Timeweb.
- Образ: multi-stage Dockerfile, один Go-процесс и Vite static.
- Health check: `GET /api/v1/health`.

Timeweb должен разворачивать только commit, прошедший GitHub Actions.

## Обязательные environment

- `DATABASE_URL` — PostgreSQL URL.
- `DATABASE_SSL_CA` — полный PEM сертификат, включая BEGIN/END.
- `DATABASE_SSL_VERIFY=true` — штатный production режим.
- `AUTH_COOKIE_SECURE=true`.
- `AUTH_SESSION_DAYS` — 1–90, по умолчанию 30.
- `SMSRU_API_KEY`.
- `INTEGRATION_SECRETS_PRIVATE_KEY`.
- `TELEGRAM_BOT_TOKEN`.
- `TELEGRAM_ORDER_CHAT_ID`.
- `ADMIN_EMAILS` — список через запятую.

Container задаёт `PORT`, `STATIC_DIR` и `MIGRATIONS_DIR`. Значения секретов не должны попадать в issue, PR, логи или документацию.

## Локальная разработка

Frontend: перейти в `frontend`, выполнить `npm ci` и `npm run dev`.

Backend: перейти в `backend`, задать локальные `DATABASE_URL`, `MIGRATIONS_DIR=../timeweb/migrations`, `AUTH_COOKIE_SECURE=false`, затем `go run ./cmd/api`.

Без `SMSRU_API_KEY` production-сценарий звонка не проверяется. Не использовать production database для разработки.

## Release

1. Обновить ветку от `main`.
2. Создать feature branch.
3. Выполнить проверки из `AGENTS.md`.
4. Открыть PR и дождаться зелёного CI.
5. Проверить migrations и список environment changes.
6. Слить только по явной команде владельца.
7. Дождаться Timeweb deployment.
8. Проверить health, главную, каталог, login/register и безопасные API.
9. Реальные заказ, звонок и платёж выполнять только как согласованный ручной тест.

## Rollback

- Для кода развернуть последний заведомо исправный commit.
- SQL migrations не откатывать редактированием старых файлов.
- При несовместимой migration создать forward-fix.
- Восстановление БД из backup — крайняя операция с подтверждением владельца и оценкой потери данных.

## Наблюдаемость и backup

Go пишет структурированные JSON-логи. Следует добавить correlation ID, метрики ошибок API, задержки внешних интеграций и уведомление о провале Saby sync.

Ежедневный backup PostgreSQL необходим. Факт включения, срок хранения и процедура тестового восстановления должны проверяться в Timeweb отдельно; наличие оплаченной опции нельзя считать доказательством рабочего восстановления.
