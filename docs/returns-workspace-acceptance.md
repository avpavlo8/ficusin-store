# Returns workspace acceptance — 2026-10-14

Scope: the 25-point live design review, except point 3 explicitly excluded by the owner. Site price/cost ownership and Saby posting rules stay unchanged. Thermal labels remain 58×40 mm, black and white, with embedded regular and bold fonts.

| Review | Problem | Final implementation |
| --- | --- | --- |
| 1 | Изменения в карточке исчезают без предупреждения | Serialized autosave; debounce, status, retry, awaited navigation and lost-response read-back. |
| 2 | На телефоне нажатие не открывает карточку | Standalone mobile card; focus, first-screen heading and restored list position. |
| 3 | Себестоимость на сайте и в СБИС расходится | Excluded by owner: site remains cost source of truth; no Saby cost changes. |
| 4 | Готовность растения смешана с готовностью учёта | Separate plant state and receipt badge with next step. |
| 5 | Удаление всего раздела выглядит обычным рабочим действием | Whole-section action in menu, exact scope and soft-delete archive with restoration. |
| 6 | «Продать» обещает другой результат | Explicit preparation workflow with Saby posting and manual cash-register discount. |
| 7 | Печать неожиданно завершает приёмку | Draft button explicitly saves and downloads; saved document only downloads. |
| 8 | У трёх цен нет ясного различия | One live final price, original price and discount; unsaved marker. |
| 9 | Два разных объекта называются «Возврат №3» | Intake number and RET article use distinct names; source-document navigation. |
| 10 | Сохранённый документ выглядит сломанной формой | Immutable saved-composition table plus current units and instance navigation. |
| 11 | Автосохранение работает по разным правилам | Consistent automatic saving with visible status and retry. |
| 12 | Массовый выбор не объясняет состав поступления | Eligible ready units only; exclusion counts, explicit batch preview and idempotent API. |
| 13 | Финансовое предупреждение не ведёт к действию | Neutral manual-intake explanation; owner finance route, manager escalation by RET article. |
| 14 | Счётчик считает пустые строки растениями | Selected unit count and separate blank-row count. |
| 15 | Ошибка пустой формы слишком общая | Inline field validation, aria-invalid and focus on first invalid field. |
| 16 | Длинная форма прячет основные действия | Scrollable intake body and persistent footer; phone row cards. |
| 17 | Мобильная шапка занимает слишком много места | Compact mobile navigation menu and workspace header. |
| 18 | Вкладки на телефоне обрезаются | Short wrapped state buttons and separate journal tabs. |
| 19 | Журнал документов не рассчитан на накопление | Document search, channel, date and receipt filters. |
| 20 | Фото не образуют полноценный сценарий осмотра | Upload progress, zoom, atomic replacement, soft delete and undo. |
| 21 | Текст и флажки слишком мелкие | Larger text, checkbox hit areas, contrast and visible keyboard focus. |
| 22 | Высоты и акценты не согласованы | Aligned controls, operation hierarchy and consistent spacing. |
| 23 | Заголовки и служебные обозначения мешают сканированию | Separate size badge, unified outlined icons and operational palette. |
| 24 | Тексты не выдержаны в одной системе | State/action vocabulary, Russian plurals and filter reset. |
| 25 | Связь восстановления с публичной витриной недостаточно объяснена | Publication status, instance preview, independent public condition notes, photo source and cache-busting photo URL. |

## Validation

- `e2e/tests/admin-returns*.spec.ts`: real bundle with explicit API contracts; desktop, iPhone/WebKit, Android/Chromium; keyboard and WCAG AA checks, server draft recovery, validation, saved documents, lost/slow responses, batch eligibility and in-dialog retry.
- `e2e/verify-return-recovery.mjs`: real HTTP API + isolated PostgreSQL, 1440/390 px; saved composition, immediate navigation autosave, photos, labels, public description, preparation for sale, batch idempotency, manager restrictions, owner archive/restore preserving receipt identity.
- `TestReturnIntakeRecoveryDocumentsOnLiveDatabase`: atomic invalid replacement, deleted photo inaccessible, photo undo, public/private separation, immutable document, concurrent receipt deduplication and restore without requeueing an existing Saby draft.
- Full browser regression suite, frontend lint/build/budgets, Go tests/vet/build, all CRM live stages. Release validation also requires exact served backend revision/assets, readiness/operations health and authenticated production desktop/mobile reads.
- No real Saby posting or cash-register sale is exercised by acceptance tests; CI uses synthetic identities without external credentials. Physical printer output must use actual size (100%, no fit-to-page); PDF page geometry and text bounds are checked before delivery.

## Rollback

Revert the application commit and allow the normal main deployment. The migration is additive; preserve `public_comment` and photo `deleted_at`, rather than dropping evidence. The previous UI does not expose archive/photo restoration. Existing Saby identifiers and journal records are never deleted or re-created by restoration.
