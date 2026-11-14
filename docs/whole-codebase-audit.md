# Полный аудит действующей кодовой базы

Проверка 14 ноября 2026 года. Отчёт объединяет независимые проходы backend,
frontend и ops после `f342a25`. Исходный код этой группы зафиксирован в
`587cb9bd799efef0adb639b7b879e55ca11822f7`; исправление post-migration проверки
CI — в `50e1afa`. Изменения документации выпускаются отдельно и не меняют
runtime. Production-данные и внешние операции не изменялись.

Это карта покрытия и решений, а не утверждение, что любое слово `legacy`
обозначает лишний код. Текущий функционал описан в
[current-system.md](current-system.md), домены — в
[backend-runtime-audit.md](backend-runtime-audit.md), контракты — в
[openapi.yaml](openapi.yaml). Предыдущие этапы сохраняются в
[post-price-cleanup-audit.md](post-price-cleanup-audit.md) как история.

## Объём и способ проверки

| Область | Проверено | Метод и результат |
|---|---:|---|
| Backend | 31 пакет `internal` | 28 доменных пакетов плюс `httpapi`, `integration`, `config`; сборка от `cmd/api`, типизированные ссылки, SQL/JSON/XML, интерфейсы, фоновые consumers и PostgreSQL. |
| Frontend | 177 source files | 176 TS/TSX и ambient `vite-env.d.ts`; entry/import graph, type-only и dynamic imports, exports, JSX callers, property reads/spreads, старые журналы черновиков. Подтверждённых orphan executable modules нет. |
| HTTP | 236 операций / 91 схема | Go AST → OpenAPI; проверка маршрутов, строгих запросов, ролей и действительных frontend callers. |
| Стили | 42 CSS-файла | Все каталоги `frontend/src`, включая care/product; PostCSS selectors и runtime consumers. После очистки guard проверил 1270 классов; мёртвых по guard нет. |
| Scripts | 29 файлов | Workflow calls, Python imports, unittest discovery, CLI inputs, генераторы и источники; каждый путь классифицирован ниже. |
| Frontend guards | 3 | Runtime module graph, CSS usage и bundle budget; исполняемые regression fixtures отдельно от enforcement. |
| Workflows | 9 / 17 job definitions | Triggers, conditions, action inputs, commands и результаты. Все 32 пары Go package/selector имеют тесты. |
| Package roots | 3 | `frontend/package.json`, `e2e/package.json`, `backend/go.mod`; прямые зависимости и фактические consumers. |
| Конфигурация | 61 ключ `.env.example` | Дубликатов и ключей без config-reader нет; отсутствие provider key не считается снятием функции. |
| SQL | 220 миграций | Полная упорядоченная история, зависимости генераторов, схема и safety guards. В этой группе SQL не изменён, новая миграция не добавлена. |
| Локальная БД | 111 relations в `public` | Схема/зависимости и исполняемые проверки; это не количество ассетов и не production-инвентарь. |
| `public/` | 119 файлов | Literal и computed URL, HTML/manifest/SW/mail/SEO, данные и старые установленные приложения. 11 неподтверждённых кандидатов оставлены. |
| Документация и источники | Профильные документы и 14 JSON | Текущие инструкции отделены от исторических решений, исходных данных и migration provenance. |

Перед анализом учитывались метаданные файлов: cloud stub не принимался за пустой
исходник. Совпадение строки, одиночный TypeScript symbol или отсутствие прямого
Go-вызова не использовались как самостоятельное доказательство ненужности.
Потребителем может быть SQL, сериализатор, интерфейсный callback, шаблон URL,
GitHub Actions либо сохранённая пользовательская запись.

## Покрытие backend

Все 28 доменов перечислены с consumers и условиями сохранения в
[backend-runtime-audit.md](backend-runtime-audit.md):

| Группа | Пакеты и действующие функции |
|---|---|
| Витрина и управление каталогом | `admin`, `catalog`, `catalogeditor`, `photos`: PIM, категории, варианты/SKU, галерея, фильтры, подборки, публикация контента и отдельно подтверждённых цен. |
| Покупатель и коммуникации | `auth`, `cart`, `chat`, `consent`, `reviews`, `notify`, `webpush`, `mail`: звонок, сессии, корзины, согласия, отзывы, вложения и уведомления. |
| Торговля | `order`, `orderworkflow`, `payment`, `procurement`, `avito`, `analytics`: заказы, упаковка, доставка, деньги/чеки, инвойсы, себестоимость, продажи и внешние площадки. |
| Инфраструктура | `saby`, `settings`, `operations`, `store`, `migrate`, `staticassets`, `money`, `perf`, `fileassets`, `testsupport`: склад, настройки, диагностика, PostgreSQL/TLS, миграции, файлы и арифметика. |
| HTTP | `httpapi`: router, роли, same-origin boundary, публичный/административный API, DTO, webhook, SPA/SEO и кэш. |
| Provider adapters | `integration`: СДЭК, ЮKassa, WB/Ozon/СБИС и mock transport; protocol payload, provider IDs, readback и неизвестные результаты. |
| Окружение | `config`: production/staging/development, TLS, обязательность ключей, live-publication gate и изоляция staging. |

Удалены три неиспользуемых `Repository` из `admin`, `catalog`, `order`:
обработчики уже использовали собственные реальные контракты.

У единственного production `procurement.PostgresStore` обязательны все текущие
операции. Удалены девять partial-store fallback веток: first Saby-price export,
folders, plan preview, unit-price preview, pricing example, supplier update,
document PDF, product page и receipt recheck. Draft stores, channel cache и
sync coordinator включены в compile-time контракт. Неполная тестовая реализация
не определяет дополнительный режим production.

В HTTP платежей и СДЭК убрана поддержка неполных адаптеров. Реальная
конфигурация provider, ошибки сети, анонимность при отсутствии auth dependency,
проверка ПВЗ и восстановление неопределённых операций сохранены.

### Приём уведомления ЮKassa

Обработчик принимает настоящий envelope `type: notification`, `event` и полный
`object`, а не тестовый объект только с ID. Внутренние поля объекта читаются как
raw JSON; из уведомления извлекается только непустой строковый ID. Сумма, валюта,
metadata, `paid` и присланный статус не объявляют заказ оплаченным: выполняется
GET к провайдеру и действующая проверка платежа. Невалидные/посторонние события
не обращаются к провайдеру; ошибка сверки допускает повтор доставки.
Формат и актуальность состояния сверены с
[официальной документацией входящих уведомлений ЮKassa](https://yookassa.ru/developers/using-api/webhooks).

HTTP-тесты исполняют полный provider object, дополнительные вложенные поля,
невалидный ID/envelope, посторонний event, provider failure и выключенную
интеграцию. Это не запросы реальных платежей.

## Покрытие frontend

| Область | Files | Проверенная граница |
|---|---:|---|
| Витрина, поиск, PDP, категории, подборки | 25 | Полное дерево категорий, сортировка/поиск, точный вариант, computed media. |
| Корзина и оформление | 5 | `/cart` и `/checkout`, серверная корзина, отдельное монтирование checkout, подтверждение после очистки корзины. |
| Админская оболочка, аналитика, общие компоненты | 26 | Навигация/permissions, текущие DTO и форматирование. |
| Кабинет, авторизация, информация, care | 11 | Профиль, заказы, возврат к оплате, legal/care routes и изображения статей. |
| Чат, уведомления, PWA | 20 | Вложения, viewport hooks, unread/push, SW/manifest/install prompt. |
| Каталог CRM, PIM, редактор, публикации, черновики | 34 | SKU identity, schema, channel queues, media и версии сохранённых journals. |
| Маршрутизация и entry | 2 | Eager/lazy routes, redirects, query aliases и неверные URL. |
| Закупки, цены, рекомендации | 16 | URL-состояние, расчёт `cost`/`retail`, настройки, планы и история. |
| Возвраты и приёмка | 8 | Autosave/readback, оценка, фото и связанное поступление. |
| Админские заказы, упаковка, доставка | 6 | SKU search, состав/суммы, коробки, отправления и payment recovery. |
| Библиотеки, сеть и деньги | 23 | Subscriptions, retries/abort, persisted state, формат/округление сумм. |
| Ambient declarations | 1 | `vite-env.d.ts` нужен компилятору, runtime export не ожидается. |

Дополнительно 248 прямых frontend API calls сопоставлены с router/OpenAPI:
244 автоматически, четыре переменных семейства — вручную. Полностью переменные
base URLs проверялись по flow; они не объявляются покрытыми простым regex.

Подтверждённые удаления:

- Четыре пути расчёта старого plant menu и `plantMenuItems`/`fallbackMenus.plants`:
  фактические callers уже строили отображаемое полное дерево `catalog`.
- Пять нечитавшихся props: `SectionHeading.eyebrow`, `LegalPage.eyebrow`,
  `AdminOrderPacking.email`, `AdminShipmentOffers.cdekTariffCode`,
  `ProcurementUploadDialog.onError`. Поля запросов/БД этим не удалены.
- Пустой всегда скрытый announcement и write-only `choicesOf.freeText`.
- 69 CSS rules / 71 selector arms в девяти файлах. Оставшиеся selectors,
  declarations и at-rule ancestry совпали с исходным состоянием.
- Устаревшие отрицательные assertions про floating cart и hidden modal drawer;
  сами cart/menu/search сценарии и рабочие проверки сохранены.

CSS guard больше не считает тестовые локаторы потребителями, не пропускает
целые доменные префиксы и не принимает SVG URL за CSS-класс. Он проверяет все
42 CSS-файла; четыре meaningful fixtures исполняются в `npm test`, enforcement
остаётся в `npm run lint:css`.

## Scripts, утилиты и входные данные

| Файлы | Текущий consumer / решение |
|---|---|
| `check-openapi.mjs` | CI api-contract; AST router/DTO → OpenAPI, offline. |
| `check-staging.py`, `require-staging.py` | Exact-candidate staging verification и gate PR по SHA/workflow/deployment. |
| `notify-production-telegram.py` | Production monitor incident/recovery; транспорт проверяется без отправки реального сообщения. |
| `saby-probe.py`, `saby-receipt-probe.py` | Ручная read-only диагностика номенклатуры/существующего документа. |
| `sync-saby.py`, `saby_catalog_merge.py` | Ручной OIDC fallback; второй модуль реально импортируется producer и его тестами. Raw имя/фото/цена нужны явному импорту нового draft, хотя обратный обмен существующих карточек stock-only. |
| `verify-commerce-image.sh`, `verify-commerce-load.mjs` | Реальный локальный image/HTTP checkout и read-only load/latency gate CI. |
| `verify-backup-restore.sh` | Сам делает dump/restore в пустую `ficusin_restore_*`; не принимает готовый файл бэкапа. |
| `seed-crm-acceptance.sql`, `test-plant-characteristics-migration.sql` | Disposable CRM и исполняемые SQL-инварианты; исторические receipt/reservation fixtures сохранены намеренно. |
| `generate_curated_plant_content.py`, `_second.py`, `_third.py`, `_fourth.py` | Генераторы исторических SQL, зависимости между модулями и независимые source snapshots. |
| `generate_reviewed_plant_passports.py`, `generate_agreed_measurements.py` | Проверенные JSON → guarded SQL/source ledger; операторские инструменты, не фоновые publishers. |
| Два `catalog_curated_content*_preview.sql` | Воспроизводимость соответствующей исторической миграции и read-only сверка. Не инструкция повторной записи production. |
| Восемь `test_*.py` | Unittest discovery в CI; provider merge, source identity, SQL reproduction и миграционные инварианты. Всего 43 случая. |

Отсутствие буквального имени `.py` в caller не считается отсутствием импорта.
Четыре curated генератора образуют цепочку imports; все восемь Python test
modules загружает discovery. Manual workflows сохраняются независимо от наличия
cron. Все семь флагов двух операторских Go CLI, три входа passport generator,
четыре workflow inputs и используемые shell/env overrides имеют потребителей.

Прежний тест четвёртого пакета зависел от абсолютного пути чужой рабочей папки
и тихо пропускался в CI. Теперь обязательный
[snapshot](catalog-curated-content-fourth-identity-snapshot-2026-10-12.json)
содержит только 15 code/name из независимого CSV, source filename/date и SHA-256.
Названия не получены из `PROFILES`. Изменение имени генератора вызывает failure,
отсутствие fixture — error, а не skip. Остальные 13 JSON-источников и все
220 миграций сохранены; опубликованный SQL не переписывался.

У frontend теперь один `npm test` с discovery всех 21 `*.test.mjs`: он включает
ранее забытые CI-тесты черновика и отображения цен. Старые money/phone/payment
wrappers не имели других callers и удалены; guard self-tests не дублируются в
lint. Тесты сами сохранены.

## CI, контейнер и конфигурация

| Workflow | Trigger / назначение |
|---|---|
| Verify | PR/manual; frontend, API contract, layout matrix/aggregator, backend, migrations, Python, dependencies, image. |
| CodeQL | Main/PR/weekly, с условием доступности для публичного репозитория. Это не безусловно мёртвая job. |
| CRM acceptance | PR/manual; настоящие Go/PostgreSQL/HTTP, роли, закупки, возвраты и чат. |
| Production monitor | Каждые пять минут/manual; critical reads, инцидент/восстановление и оповещения. |
| Production smoke | Main/manual; конкретный backend/frontend release и public browser suite. |
| Saby catalog sync | Только manual emergency fallback; обычное расписание в Go workers. |
| Saby probe | Только manual, оба вида входов используются. |
| Staging gate | PR; успешный exact head SHA deployment плюс проверка workflow. |
| Staging verification | Staging/manual; exact candidate, изоляция и browser/admin проверки. |

Прямые script paths существуют; пустых Go selectors не найдено. У npm unit
тестов больше нет ручного CI-списка, который забывает новые файлы. Основные,
production и staging Playwright configs также проверены по их действительным
testDir/projects; screen tags не оставляют ни один текущий сценарий без проекта.

Первый CI run выявил пропущенный inline SQL в post-migration assertion:
`normalized_plants` ещё читал снятую typed-колонку SKU. Проверка переведена на
канонический `variant_numeric_attribute(variant.id, 'pot_diameter_cm')` с
сохранением точного ожидаемого результата. Локальное выполнение на всех
220 миграциях прошло; повторный поиск в scripts/workflows других таких чтений
не выявил. Это исправление проверки, а не возврат старой колонки.

Два npm package roots и Go module сохраняют действующие runtime/build/test
dependencies. `postcss` объявлен прямой dev-зависимостью используемого parser;
версии транзитивных пакетов ради этого не обновлялись. Vite выбирает явно
`vite.config.ts`, TypeScript node project использует `noEmit`.

Docker имеет production API, экспортируемый frontend target и runtime с
`pdftotext` и PDF-шрифтами: все используются. Исключены root `docs/`, `scripts/`,
`e2e/`, `frontend/tests/` из build context, включая локальные browser sessions и
reports. `frontend/scripts/` сохранён. Backend test sources сохранены: они
участвуют в текущем build-version hash.

Неиспользуемый `timeweb/ca.crt` удалён: он не попадал в image и не читался
runtime. Доверие БД настраивается provider PEM в `DATABASE_SSL_CA`, проверка
TLS не выключалась. Для локальных PDF нужен доступный TTF в
`PROCUREMENT_PDF_FONT`; macOS-пример и Docker default приведены в README.

## Что намеренно осталось

| Путь / механизм | Реальный consumer и условие возможного удаления |
|---|---|
| Неизвестные платежи, возвраты, СДЭК, публикации | Frozen payload, provider ID и reconciliation. Сначала подтвердить результат; удаление очереди не заменяет сверку. |
| Старые реальные резервы/`stock_movements` | Отмена, выдача, корректировки и диагностика истории. Новые заказы не создают резерв; историю удалять нельзя без проверки данных. |
| Старые bytea и public review media | Авторизованный read path существующих объектов. Нужны полный verified copy/readback и отсутствие зависимых строк. |
| Legacy внешние ID, slug/route aliases и journals | Продажи, ссылки и восстановление пользовательских правок. Удаление требует миграции/наблюдения потребителей. |
| `economics.unified.enabled=false`, `trolleyCostCurrency` | Текущий расчёт и замороженные расходы/черновики; частичный frontend no-read не доказывает ненужность. |
| `published`/`barcodes[].codeType` Saby input | Действующий Python producer и строгий wire decoder. Для удаления нужна согласованная версия контракта. |
| SQL triggers/callbacks и `testsupport` | Триггерные записи, pgx tracing, проверяемый file-storage stub. Прямой вызов не обязателен для интерфейсного consumer. |
| Корневые PWA aliases и computed care/brand URL | Установленные приложения, `BrandLogo tone=ivory`, topic/slug URL. Literal-only scan даёт ложные кандидаты. |

Оставлены 11 ассетов без доказательства отсутствия production URL:

- `assets/brand/graphite-v1/avatar-{graphite,ivory}.{png,svg}` — четыре файла;
- `assets/brand/graphite-v1/mark-ivory.svg`, `wordmark-ivory.png`;
- `assets/redesign/filters/easy.webp`, `pets.webp`;
- `images/care/humidity.webp`, `repotting.webp`, `watering.webp`.

В исходниках их runtime consumers не подтверждены; локальный read-only scan
137 public/archive relations не нашёл соответствующих URL. Это не доказывает
состояние production, старых media и внешних опубликованных ссылок. Файлы не
удалены. `wordmark-ivory.svg`, admin `app.svg`, SEO `og.png`, topic/slug care
изображения и CRM `images/care/light.webp` имеют подтверждённых consumers.

## Проверки и границы результата

Финальные локальные проверки текущего source batch:

- Go `test ./...`, `vet ./...`, build; race для `httpapi`, `payment`, `procurement`.
- Пять publisher suites, PostgreSQL file/chat и 12 history-guard сценариев;
  HTTP recovery/edit сценарии возвратов и реальный chat на desktop/mobile.
- OpenAPI: 236 операций / 91 схема.
- Frontend build/lint/format/budget; `npm test` **79/79**, без skip.
- Python **43/43**, без skip; immutable SQL без diff.
- Полный browser run **888/888**, без skip/retry. Ранние диагностические
  красные прогоны разобраны и не подменяют этот окончательный результат.
- `git diff --check`.

Проверки выполнялись локально, с изолированной PostgreSQL и provider mocks;
они не подтверждают успешные реальные списания/публикации. Docker локально
недоступен; фактический image build проверен в GitHub Actions. Первый
[run 43574128950](https://github.com/avpavlo8/ficusin-store/actions/runs/43574128950)
упал на описанном выше устаревшем post-migration assertion. После исправления
успешно завершён [run 43574411017](https://github.com/avpavlo8/ficusin-store/actions/runs/43574411017)
для `50e1afa9b7968f26b96bf799051cbc20465cd557`: frontend, backend, OpenAPI,
Python, все миграции/PG-проверки, обе browser-части и общий layout gate.
Image job собрала и запустила production-образ, проверила runtime tools,
commerce, нагрузку и dump/restore. Условный dependency-review предназначен для
PR и при manual dispatch не запускался. Последующий коммит меняет только
документацию; runtime соответствует проверенному SHA. Production deploy не
выполнялся.

Окончательное снятие функции включает код, API/DTO, настройки, jobs/queues,
проверки, scripts, ассеты и актуализацию документации. Сохранённая переходная
граница должна иметь consumer и условие удаления. Будущее отсутствие ключа,
отключённая публикация или старое имя файла не являются таким доказательством.
