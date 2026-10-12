# Проверенные тексты растений, 12 октября 2026

Миграция `20261012_z_curated_plant_content.sql` заполняет пустые поля 42 карточек. Коды и названия взяты из живой таблицы админки 12 октября. Из 17 новых карточек в пакет входят 14 с уверенной идентификацией хотя бы до рода; остальные 3 («амариллис» без разграничения с гиппеаструмом, бонсай фисташка без вида, смешанная композиция «Лес») остаются на ручной проверке.

Также в этом пакете удаляется устаревшая категория «Для пальм» (`paml`) под «Грунты», только если у неё по-прежнему нет товаров, дочерних категорий и отдельного фильтра. Старый адрес `/catalog/paml` отдаёт постоянное перенаправление на `/catalog/soil`.

Названия, размеры, фото, цены, остатки, статус публикации и связи с СБИС/маркетплейсами миграция не меняет. Она сверяет точный `product_code`, сохранённое название и `catalog_section='plants'`. Заполняет только пустые `latin_name`, `short_description`, `description`, `care_instructions`, `light_level`, `watering`. Существующий контент менеджера остаётся приоритетным. При расхождении названия карточка пропускается.

## Источники и группы

| Группа | Коды сайта | Профильный источник |
|---|---|---|
| Цветущие антуриумы, без утверждения вида/цвета конкретного сорта | 851, 852, 855 | [RHS: антуриумы](https://www.rhs.org.uk/plants/anthuriums/how-to-grow-anthuriums) |
| Араукария, только уровень рода | 853 | [RHS: Araucaria heterophylla](https://www.rhs.org.uk/plants/1461/araucaria-heterophylla/details) |
| Диффенбахия «Рефлектор» | 856 | [RHS: Dieffenbachia 'Reflector'](https://www.rhs.org.uk/plants/372531/dieffenbachia-reflector-v/details) |
| Замиокулькас, без утверждения сорта Roundleaf | 857, 566 | [NC State Extension: Zamioculcas zamiifolia](https://plants.ces.ncsu.edu/plants/zamioculcas-zamiifolia/common-name/zz-plant/) |
| Кордилина «Киви» | 865 | [RHS: Cordyline fruticosa 'Kiwi'](https://www.rhs.org.uk/plants/43794/cordyline-fruticosa-kiwi/details) |
| Нефролепис, без утверждения точного вида сортов | 859, 860 | [RHS: Nephrolepis exaltata](https://www.rhs.org.uk/plants/11508/nephrolepis-exaltata/details) |
| Традесканция зебрина | 867 | [NC State Extension: Tradescantia zebrina](https://plants.ces.ncsu.edu/plants/tradescantia-zebrina/common-name/inch-plant/) |
| Хлорофитум, без утверждения сорта «кудрявый» | 861 | [NC State Extension: Chlorophytum comosum](https://plants.ces.ncsu.edu/plants/chlorophytum-comosum/common-name/spider-ivy/) |
| Аглаонемы | 2, 12 | [NC State Extension: Aglaonema](https://plants.ces.ncsu.edu/plants/aglaonema/) |
| Монстера делициоза | 149, 815 | [NC State Extension: Monstera deliciosa](https://plants.ces.ncsu.edu/plants/monstera-deliciosa/) |
| Фикус эластика | 740, 685 | [RHS: декоративные фикусы](https://www.rhs.org.uk/plants/ornamental-figs/growing-guide) |
| Спатифиллум | 738 | [RHS: спатифиллум](https://www.rhs.org.uk/plants/peace-lilies/how-to-grow-peace-lilies) |
| Фаленопсис | 161 | [RHS: фаленопсис](https://www.rhs.org.uk/plants/phalaenopsis/growing-guide) |
| Венерина мухоловка | 87 | [RHS: мухоловка](https://www.rhs.org.uk/plants/venus-fly-trap) |
| Непентес | 154 | [RHS: хищные растения](https://www.rhs.org.uk/plants/types/carnivorous/growing-guide) |
| Пуансеттия | 168 | [RHS: пуансеттия](https://www.rhs.org.uk/plants/euphorbia/poinsettia) |
| Адениум «Пустынная роза» | 854 | [NC State Extension: Adenium obesum](https://plants.ces.ncsu.edu/plants/adenium-obesum/) |
| Алоказии без утверждения сорта | 862, 847, 16 | [RHS: алоказии](https://www.rhs.org.uk/plants/alocasia/growing-guide) |
| Сансевиерии с названием только до рода | 174, 175, 177, 178, 749 | [RHS: сансевиерии](https://www.rhs.org.uk/plants/sansevieria/growing-guide) |
| Фикус Бенджамина, торговые сорта в названиях сохранены | 486, 755, 198, 199, 200, 201, 202, 203, 197 | [NC State Extension: Ficus benjamina](https://plants.ces.ncsu.edu/plants/ficus-benjamina/common-name/small-leaved-rubber-plant/) |
| Лиметта Пурша, только род Citrus | 866 | [RHS: цитрусовые](https://www.rhs.org.uk/fruit/citrus/grow-your-own) |

## Проверка перед выпуском

1. Запустить `python3 scripts/test_curated_plant_content.py` и `python3 scripts/generate_curated_plant_content.py`; `git diff --exit-code` после генерации должен быть пустым.
2. PR и CI должны подтвердить миграцию, сборку и тесты. Мерж в `main` автоматически выкатывает миграцию при запуске сервиса.
3. После деплоя открыть новые карточки в админке, проверить заполнение текста и сохранность цены/остатка/размера. Пропущенные из-за изменившегося названия карточки исправлять отдельным точечным пакетом.

Указанные источники описывают биологию вида или рода; они не доказывают, что конкретная поставка соответствует торговому названию. Для неуточнённых сортов латинское имя ограничено родом или подтверждённым видом без выдумывания сорта.
