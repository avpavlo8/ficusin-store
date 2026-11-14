# Дополнение к текущей карте системы: Flowwow

Ветка `feature/flowwow-integration` расширяет действующую систему отдельным
разделом Flowwow (`AdminFlowwow`), настройками канала в общем редакторе товара
(`ProductFlowwow`), адаптером Seller API (`integration/flowwow*`) и службой
`flowwow.Service`.

Публикации используют существующие `catalog_editor_plans`,
`catalog_editor_publications`, leases, pending snapshots и remote links.
Служба `Run` дополнительно сверяет изменения цен и свежих складских остатков
каждые 30 секунд. Режим публикации и пустые ключи контролируют реальную отправку.

Сайт — источник контента и цены; складской источник и правила свежести совпадают
с витриной. Импорт заказов Flowwow и общий резерв каналов не добавлены.

Подробный протокол, настройка, ограничения и выполненные проверки описаны в
[flowwow.md](flowwow.md). Полная действующая архитектура остальных разделов:
[business-logic.md](business-logic.md), [catalog-editor-channels.md](catalog-editor-channels.md),
[procurement.md](procurement.md), [commerce-audit-remediation.md](commerce-audit-remediation.md).
