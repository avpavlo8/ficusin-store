-- Описания товаров ехали из СБИС вместе с редакторской разметкой. Очистку
-- добавили в синхронизацию (plainDescription), но она срабатывает только при
-- следующем обмене, а описание у этих карточек принадлежит магазину и из
-- СБИС больше не обновляется. В итоге покупатель читал на карточке
-- «<p>Аглаонема Вайт Ленс (Aglaonema White Lance) Описание …</p>» — теги
-- выводились текстом. Чистим то, что уже лежит в базе.
--
-- Порядок повторяет plainDescription: сначала сущности, потом переносы из
-- закрывающих тегов, потом остальные теги. Повторный запуск ничего не
-- испортит: у чистого текста совпадений нет.

CREATE OR REPLACE FUNCTION ficusin_plain_markup(value TEXT) RETURNS TEXT AS $$
    SELECT btrim(
        regexp_replace(
            regexp_replace(
                regexp_replace(
                    regexp_replace(
                        replace(
                            replace(
                                replace(
                                    replace(
                                        replace(value, '&nbsp;', ' '),
                                        '&quot;', '"'),
                                    '&lt;', '<'),
                                '&gt;', '>'),
                            '&amp;', '&'),
                        '<\s*(br\s*/?|/p|/div|/li)\s*>', E'\n', 'gi'),
                    '<[^>]*>', ' ', 'g'),
                '[\t\r ]+', ' ', 'g'),
            E'\n{3,}', E'\n\n', 'g')
    );
$$ LANGUAGE SQL IMMUTABLE;

UPDATE products
SET description = ficusin_plain_markup(description)
WHERE description ~ '<[^>]+>' OR description ~ '&(nbsp|lt|gt|amp|quot);';

UPDATE products
SET short_description = ficusin_plain_markup(short_description)
WHERE short_description ~ '<[^>]+>' OR short_description ~ '&(nbsp|lt|gt|amp|quot);';

UPDATE products
SET care_instructions = ficusin_plain_markup(care_instructions)
WHERE care_instructions ~ '<[^>]+>' OR care_instructions ~ '&(nbsp|lt|gt|amp|quot);';

-- Одна карточка приехала с латинским названием «latin» — след ручной
-- проверки. На витрине это выводилось подписью под именем товара.
UPDATE products
SET latin_name = ''
WHERE lower(btrim(latin_name)) IN ('latin', 'latin name', 'нет', 'null');

DROP FUNCTION IF EXISTS ficusin_plain_markup(TEXT);
