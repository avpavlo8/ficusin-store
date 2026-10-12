# Второй пакет контента растений — 12 октября 2026

После первого выпуска в live админке было 319 растений: 156 без краткого описания, 68 без латинского имени, 180 с отметкой о неполном уходе. Отметка об уходе требует также заполненных полей `plant_passport.lighting` и `plant_passport.watering`.

Миграция заполняет только пустые поля 82 точно указанных карточек. Одна из них (#27) была пропущена в первом пакете из-за NBSP в конце названия; специальный guard убирает только этот символ. Для 181 карточки двух пакетов отдельно заполняются только пустые поля паспорта. Прочие ключи паспорта, готовые тексты, цены, фото, размеры, остатки и статус не меняются. Уже заполненная карточка #185 Dracaena sanderiana не затрагивается.

## Источники по профилям

| Профиль | Карточек | Первичный источник |
|---|---:|---|
| Цветущий антуриум | 7 | https://www.rhs.org.uk/plants/anthuriums/how-to-grow-anthuriums |
| Декоративнолистный антуриум | 3 | https://www.rhs.org.uk/plants/anthuriums/how-to-grow-anthuriums |
| Аспидистра | 4 | https://www.rhs.org.uk/plants/aspidistra/how-to-grow-aspidistras |
| Бокарнея | 2 | https://plants.ces.ncsu.edu/plants/beaucarnea-recurvata/ |
| Арека | 3 | https://plants.ces.ncsu.edu/plants/chrysalidocarpus-lutescens/ |
| Замиокулькас | 9 | https://plants.ces.ncsu.edu/plants/zamioculcas-zamiifolia/common-name/zz-plant/ |
| Кодиеум | 4 | https://plants.ces.ncsu.edu/plants/codiaeum-variegatum/ |
| Олива | 3 | https://www.rhs.org.uk/fruit/olives/grow-your-own |
| Фикус лирата | 3 | https://plants.ces.ncsu.edu/plants/ficus-lyrata/ |
| Фикус микрокарпа | 5 | https://plants.ces.ncsu.edu/plants/ficus-microcarpa/ |
| Фикус бенгальский | 3 | https://plants.ces.ncsu.edu/plants/ficus-benghalensis/ |
| Филодендрон | 11 | https://www.rhs.org.uk/plants/philodendron/growing-guide |
| Юкка | 2 | https://www.rhs.org.uk/plants/yucca/growing-guide |
| Араукария | 3 | https://www.rhs.org.uk/plants/1461/araucaria-heterophylla/details |
| Диффенбахия | 8 | https://plants.ces.ncsu.edu/plants/dieffenbachia-seguine/ |
| Драцена | 5 | https://www.rhs.org.uk/plants/dracaena/how-to-grow-dracaena |
| Азалия | 5 | https://www.rhs.org.uk/plants/rhododendron/indoor-azaleas |
| Аглаонема | 1 | https://plants.ces.ncsu.edu/plants/aglaonema/ |
| Алоказия #27 | 1 | https://www.rhs.org.uk/plants/alocasia/growing-guide |

Для неустановленного вида используется родовое латинское имя. Спорные сорта исключены. Источники первого пакета приведены в `catalog-curated-content-2026-10-12.md`.

Проверка: запустить `python3 scripts/test_curated_plant_content_second.py`, затем прочитать `scripts/catalog_curated_content_second_preview.sql` на целевой базе. После выпуска проверить публичные карточки #27 и #494, включая блок ухода.
