# Краткое описание и уход: оставшиеся определимые растения

Снимок публичного API 13 октября: из 294 карточек после объединения Raven у 19 были пусты одновременно `shortDescription` и `careInstructions`. Миграция `20261013_a_remaining_identified_plant_care.sql` заполняет 17 карточек по точному коду и старому названию, только если соответствующее поле пусто. Уже написанное владельцем не заменяется. Для ассортимента и торговых имён текст не утверждает неподтверждённый вид или сорт.

Остаются две карточки, которым нужен состав или этикетка:

- #858 «Лес большой + кашпо Д39»: неизвестно, какие растения входят в композицию; пусты также полное описание и латинское название. Нужны фото и перечень растений.
- #863 «Амариллис D12»: торговое название не позволяет отличить Amaryllis от Hippeastrum. Нужна этикетка или фото цветка/луковицы до видового ухода и латинского названия.

Уход основан на первичных ботанических рекомендациях RHS, без дословного копирования:

- [Кактусы и суккуленты](https://www.rhs.org.uk/plants/types/cacti-succulents/houseplants/growing-guide) — учитывается исключение для лесных кактусов.
- [Калатеи](https://www.rhs.org.uk/plants/calathea/growing-guide), [пеперомии](https://www.rhs.org.uk/plants/peperomia/how-to-grow-peperomia), [монстера серебристая как отдельный вид](https://www.rhs.org.uk/plants/388862/monstera-siltepecana/details) — для торгового названия «Медиум Сильвер» вид не присваивался.
- [Хамедорея](https://www.rhs.org.uk/plants/chamaedorea/growing-guide), [спатифиллум](https://www.rhs.org.uk/plants/peace-lilies/how-to-grow-peace-lilies), [тилландсии](https://www.rhs.org.uk/plants/types/houseplants/air-plants), [африканские фиалки](https://www.rhs.org.uk/plants/african-violets/growing-guide).
- [Комнатные фикусы](https://www.rhs.org.uk/plants/ornamental-figs/growing-guide/), [бонсай](https://www.rhs.org.uk/plants/types/trees/bonsai), [Pistacia](https://www.rhs.org.uk/plants/25153/pistacia-lentiscus/details). Для фисташки конкретный вид не присваивался.
- [Solanum pseudocapsicum](https://www.rhs.org.uk/plants/66889/solanum-pseudocapsicum/details) показывает токсичность декоративных плодов; конкретный вид карточки #181 не установлен, поэтому предупреждение сформулировано консервативно.

Эта правка закрывает пустые краткие описания и инструкции, но не заменяет отдельную редактуру старых длинных описаний СБИС, фото, замеры размеров и прямую сверку ссылок маркетплейсов.
