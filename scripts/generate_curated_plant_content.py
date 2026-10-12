#!/usr/bin/env python3
"""Generate a guarded, deliberately small plant-content migration.

Every profile is backed by a primary horticultural source. Source URLs are
recorded in docs/catalog-curated-content-2026-10-12.md. Names are frozen from
live admin snapshot; matching names and product_code are required at deploy.
"""
from __future__ import annotations
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LIVE = ROOT / 'docs/catalog-curated-content-snapshot-2026-10-12.json'
MIGRATION = ROOT / 'timeweb/migrations/20261012_z_curated_plant_content.sql'
PREVIEW = ROOT / 'scripts/catalog_curated_content_preview.sql'

# source, Latin name (empty = preserve existing), short, description, care,
# light enum, watering enum. Do not claim cultivars not verified by the label.
PROFILES = {
    'anthurium_flowering': (
        'https://www.rhs.org.uk/plants/anthuriums/how-to-grow-anthuriums', 'Anthurium',
        'Цветущий антуриум с глянцевыми листьями и соцветием из покрывала и початка.',
        'У антуриума яркое покрывало окружает початок с мелкими цветками. Он подходит для тёплого светлого места без прямых полуденных лучей; окраска и размер соцветий зависят от конкретного сорта.',
        'Поставьте в яркий рассеянный свет. Поливайте, когда верхний слой субстрата начинает подсыхать, и сливайте лишнюю воду. Держите в тепле, вдали от холодных сквозняков; для корней нужен рыхлый дренированный субстрат.',
        'diffused', 'moderate'),
    'araucaria': (
        'https://www.rhs.org.uk/plants/1461/araucaria-heterophylla/details', 'Araucaria',
        'Араукария с симметричными ярусами мягких хвойных ветвей.',
        'Араукария напоминает небольшое комнатное хвойное дерево. Её аккуратный ярусный силуэт лучше сохраняется при равномерном освещении и достаточном пространстве вокруг кроны.',
        'Выберите светлое место и защищайте растение от холодных сквозняков. В период роста поливайте после лёгкого подсыхания поверхности грунта; зимой при замедлении роста полив сокращайте. Не оставляйте воду в поддоне.',
        'diffused', 'moderate'),
    'dieffenbachia_reflector': (
        'https://www.rhs.org.uk/plants/372531/dieffenbachia-reflector-v/details', "Dieffenbachia 'Reflector'",
        'Диффенбахия «Рефлектор» с крупными пёстрыми листьями.',
        'У диффенбахии «Рефлектор» декоративные овальные листья с пятнистым рисунком. Главный акцент этой карточки — листва; редкое комнатное цветение не используется как обещание.',
        'Держите в ярком рассеянном свете без прямого полуденного солнца. Поливайте после подсыхания верхнего слоя субстрата и сливайте воду из поддона. Берегите от холода и сквозняков.',
        'diffused', 'moderate'),
    'zamioculcas': (
        'https://plants.ces.ncsu.edu/plants/zamioculcas-zamiifolia/common-name/zz-plant/', 'Zamioculcas zamiifolia',
        'Замиокулькас с плотными глянцевыми листьями и запасом влаги в корневищах.',
        'Замиокулькас образует прямостоячие побеги с плотными блестящими листочками. Мясистые корневища запасают влагу, поэтому для растения особенно опасен застой воды в горшке.',
        'Лучше всего растёт при ярком рассеянном свете, переносит полутень. Давайте грунту хорошо просохнуть между поливами; после полива сливайте воду из кашпо. Зимой поливайте реже.',
        'diffused', 'rare'),
    'cordyline_kiwi': (
        'https://www.rhs.org.uk/plants/43794/cordyline-fruticosa-kiwi/details', "Cordyline fruticosa 'Kiwi'",
        'Кордилина «Киви» с зелёными листьями, жёлтыми полосами и розовой каймой.',
        'Сорт «Киви» узнаваем по глянцевым ремневидным листьям: зелёный фон пересекают жёлтые штрихи, а края окрашены в розовый. Это декоративно-лиственное растение для тёплого светлого места.',
        'Держите в ярком рассеянном свете. Поливайте умеренно в период роста, после лёгкого подсыхания верхнего слоя; зимой реже. Избегайте застоя воды и холодных сквозняков.',
        'diffused', 'moderate'),
    'nephrolepis': (
        'https://www.rhs.org.uk/plants/11508/nephrolepis-exaltata/details', 'Nephrolepis',
        'Папоротник нефролепис с дуговидными ажурными вайями.',
        'Нефролепис образует густую зелёную массу из перистых вай. Он хорошо смотрится там, где листве достаточно места для свободного роста; конкретная форма вай зависит от сорта.',
        'Поставьте в яркий фильтрованный свет. Поддерживайте грунт умеренно влажным, не пересушивая ком и не оставляя корни в воде. Берегите от сухого горячего воздуха; зимой поливайте осторожнее.',
        'diffused', 'moderate'),
    'tradescantia_zebrina': (
        'https://plants.ces.ncsu.edu/plants/tradescantia-zebrina/common-name/inch-plant/', 'Tradescantia zebrina',
        'Традесканция зебрина с полосатыми листьями и свисающими побегами.',
        'Серебристые полосы на верхней стороне листьев контрастируют с пурпурной нижней стороной. Гибкие побеги можно пустить по краю полки или подвесного кашпо.',
        'Дайте яркий рассеянный свет для сохранения рисунка листьев. Поливайте после подсыхания верхнего слоя грунта; зимой сократите полив. Прищипывайте вытянувшиеся побеги для более густой формы.',
        'diffused', 'moderate'),
    'chlorophytum': (
        'https://plants.ces.ncsu.edu/plants/chlorophytum-comosum/common-name/spider-ivy/', 'Chlorophytum',
        'Хлорофитум с дуговидными листьями и возможными дочерними розетками на побегах.',
        'Хлорофитум образует пучок узких листьев; у взрослых растений могут появляться длинные побеги с молодыми розетками. Поэтому растение удобно поставить на полку или в подвесное кашпо.',
        'Выберите рассеянный свет без сильного прямого солнца. Поливайте после подсыхания верхнего слоя грунта и не оставляйте воду в поддоне. Зимой поливайте реже.',
        'diffused', 'moderate'),
    'aglaonema': (
        'https://plants.ces.ncsu.edu/plants/aglaonema/', 'Aglaonema',
        'Аглаонема с декоративным рисунком на плотных листьях.',
        'Аглаонемы выращивают ради листвы: окраска и рисунок заметно различаются между сортами. Растение образует компактный куст и подходит для места с мягким рассеянным светом.',
        'Поставьте в рассеянный свет; прямое солнце может обжечь листья. Поливайте после частичного подсыхания субстрата и не используйте холодную воду. Зимой поливайте реже.',
        'diffused', 'moderate'),
    'monstera_deliciosa': (
        'https://plants.ces.ncsu.edu/plants/monstera-deliciosa/', 'Monstera deliciosa',
        'Монстера делициоза с крупными листьями, которые со временем приобретают разрезы.',
        'По мере взросления у монстеры появляются характерные разрезы и отверстия в листовой пластине. Растение растёт как лиана, поэтому ему пригодится опора и место для крупных листьев.',
        'Поставьте в яркий рассеянный свет. Поливайте после подсыхания верхней четверти или трети грунта и сливайте воду из поддона. Подвяжите побеги к опоре по мере роста.',
        'diffused', 'moderate'),
    'ficus_elastica': (
        'https://www.rhs.org.uk/plants/ornamental-figs/growing-guide', 'Ficus elastica',
        'Фикус эластика с крупными плотными кожистыми листьями.',
        'Фикус эластика создаёт выразительный зелёный вертикальный акцент. Его крупные листья лучше выглядят при достаточном освещении и регулярном удалении пыли.',
        'Держите в ярком рассеянном свете; пёстрым формам особенно важен свет. Поливайте после подсыхания верхнего слоя грунта и не оставляйте воду в поддоне. Избегайте резкой смены места и холодных сквозняков.',
        'diffused', 'moderate'),
    'spathiphyllum': (
        'https://www.rhs.org.uk/plants/peace-lilies/how-to-grow-peace-lilies', 'Spathiphyllum',
        'Спатифиллум с глянцевой листвой и белыми покрывалами соцветий.',
        'Над густой зелёной листвой спатифиллума поднимаются белые покрывала с початком. Цветение зависит от света и условий ухода; само растение остаётся декоративным благодаря листьям.',
        'Выберите светлое место с рассеянным светом. Поливайте умеренно после подсыхания поверхности субстрата, не допуская длительной засухи и застоя воды. Берегите от сухого горячего воздуха.',
        'diffused', 'moderate'),
    'phalaenopsis': (
        'https://www.rhs.org.uk/plants/phalaenopsis/growing-guide', 'Phalaenopsis',
        'Фаленопсис с дуговидными цветоносами и длительно сохраняющимися цветками.',
        'Фаленопсис — эпифитная орхидея; её корням нужен воздухопроницаемый субстрат из коры. Если в названии указано число цветоносов, это характеристика текущей партии, а не обещание будущего цветения.',
        'Поставьте в яркий рассеянный свет без сильного солнца. Поливайте тёплой водой, когда субстрат приближается к сухому состоянию; дайте воде полностью стечь. Не оставляйте воду в центре розетки или кашпо.',
        'diffused', 'moderate'),
    'dionaea': (
        'https://www.rhs.org.uk/plants/venus-fly-trap', 'Dionaea muscipula',
        'Венерина мухоловка с ловчими листьями для насекомых.',
        'Листья мухоловки заканчиваются парными ловушками, которые закрываются при прикосновении добычи. Это болотное хищное растение требует яркого света и особого полива.',
        'Держите субстрат постоянно влажным, используя дождевую или дистиллированную воду. Поставьте в очень светлое место и обеспечьте прохладный период покоя зимой. Не подкармливайте удобрениями и не закрывайте ловушки ради игры.',
        'sunny', 'frequent'),
    'nepenthes': (
        'https://www.rhs.org.uk/plants/types/carnivorous/growing-guide', 'Nepenthes',
        'Непентес с кувшинчиками-ловушками на концах листьев.',
        'Непентес образует свисающие ловчие кувшинчики. В отличие от мухоловки, это тропическая лиана: ей нужны тепло, влажный воздух и яркий свет без палящих лучей.',
        'Держите в ярком рассеянном свете и тепле. Поливайте мягкой дождевой или дистиллированной водой, поддерживая субстрат влажным, но не оставляя горшок постоянно стоять в воде. Не применяйте обычные удобрения без видовой рекомендации.',
        'diffused', 'frequent'),
    'poinsettia': (
        'https://www.rhs.org.uk/plants/euphorbia/poinsettia', 'Euphorbia pulcherrima',
        'Пуансеттия с яркими прицветниками вокруг небольших цветков.',
        'Главный цветной акцент пуансеттии создают прицветники, окружающие маленькие настоящие цветки. Окраска конкретного экземпляра зависит от сорта и партии.',
        'Поставьте в яркий рассеянный свет, подальше от батареи и холодного окна. Поливайте, когда поверхность субстрата подсохла: грунт должен оставаться слегка влажным, но не заболоченным.',
        'diffused', 'moderate'),
    'adenium': (
        'https://plants.ces.ncsu.edu/plants/adenium-obesum/', 'Adenium obesum',
        'Адениум с утолщённым стволом-каудексом и яркими воронковидными цветками.',
        'Адениум, или пустынная роза, запасает воду в утолщённом основании ствола. В тёплый светлый сезон он наращивает листья и может цвести; окраска цветков зависит от конкретного растения.',
        'Дайте максимум света с защитой от резкого полуденного солнца. Используйте хорошо дренированный субстрат для суккулентов и горшок с отверстиями. Во время роста поливайте после просыхания грунта; в прохладный период покоя заметно сократите полив.',
        'sunny', 'rare'),
    'alocasia': (
        'https://www.rhs.org.uk/plants/alocasia/growing-guide', 'Alocasia',
        'Алоказия с выразительной листвой на длинных черешках.',
        'Алоказии выращивают ради листьев: рисунок, фактура и форма пластинки различаются между видами и сортами. Это тропическое растение особенно чувствительно к холодному воздуху и сырому грунту.',
        'Поставьте в яркий рассеянный свет и тепло. В период роста поливайте, когда верхний слой грунта слегка подсохнет; зимой полив сокращайте по состоянию растения. Не держите корни в воде и защищайте листья от холодных сквозняков.',
        'diffused', 'moderate'),
    'sansevieria': (
        'https://www.rhs.org.uk/plants/sansevieria/growing-guide', 'Sansevieria',
        'Сансевиерия с плотными вертикальными листьями, запасающими влагу.',
        'Сансевиерии образуют прочные розетки мясистых листьев. Они переносят менее яркое освещение, но избыток воды опаснее кратковременной сухости.',
        'Держите в светлом месте или лёгкой полутени. Посадите в рыхлый дренированный субстрат; поливайте лишь после хорошего просыхания грунта и сливайте воду из поддона. Зимой поливайте очень редко.',
        'diffused', 'rare'),
    'ficus_benjamina': (
        'https://plants.ces.ncsu.edu/plants/ficus-benjamina/common-name/small-leaved-rubber-plant/', 'Ficus benjamina',
        'Фикус Бенджамина с многочисленными небольшими листьями и разветвлённой кроной.',
        'Фикус Бенджамина формирует густую крону из тонких ветвей и небольших листьев. Цвет и рисунок листвы зависят от сорта; растение лучше чувствует себя на постоянном светлом месте.',
        'Обеспечьте яркий рассеянный свет, особенно пёстрым формам. Поливайте после подсыхания верхнего слоя грунта, не оставляя воду в поддоне. Берегите от холодных сквозняков и резкой смены условий: из-за стресса возможен листопад.',
        'diffused', 'moderate'),
    'citrus': (
        'https://www.rhs.org.uk/fruit/citrus/grow-your-own', 'Citrus',
        'Контейнерное цитрусовое дерево с душистой листвой и возможным цветением.',
        'Цитрусовые выращивают в горшках ради глянцевых вечнозелёных листьев, ароматных цветков и, при подходящих условиях, плодов. Название «Пурша» сохранено как торговое: точный вид или гибрид без данных поставщика не утверждается.',
        'Поставьте в максимально светлое место. В тёплый сезон поливайте по мере подсыхания верхнего слоя грунта; зимой при прохладном содержании полив сокращайте. Берегите от мороза, сухого горячего воздуха и застоя воды.',
        'sunny', 'moderate'),
    'citrus_other': (
        'https://www.rhs.org.uk/fruit/citrus/grow-your-own', 'Citrus',
        'Горшечное цитрусовое дерево с вечнозелёной листвой и возможными ароматными цветками.',
        'Цитрусовые выращивают в контейнерах ради глянцевой листвы, душистого цветения и плодов. Урожай зависит от освещения, зимовки и возраста растения; сортовые особенности не следует выводить только из торгового названия.',
        'Дайте как можно больше света и защитите от мороза. В тёплый сезон поливайте после лёгкого подсыхания поверхности грунта, не оставляя воду в поддоне. Для зимовки выберите светлое прохладное место и сократите полив.',
        'sunny', 'moderate'),
    'schlumbergera': (
        'https://www.rhs.org.uk/plants/christmas-cactus/how-to-grow', 'Schlumbergera',
        'Шлюмбергера с плоскими членистыми побегами и яркими цветками на концах сегментов.',
        '«Декабрист» — эпифитный лесной кактус: его побеги состоят из плоских сегментов, а цветки появляются на их концах. Для закладки бутонов важны сезонный ритм света и прохлады; окраска цветков зависит от экземпляра.',
        'Разместите в ярком рассеянном свете. Поливайте после лёгкого подсыхания верхнего слоя, сливая лишнюю воду. После цветения уменьшите полив; при появлении бутонов не переставляйте горшок резко и берегите от холодных сквозняков.',
        'diffused', 'moderate'),
}

# Exact product codes from the live admin, 12 Oct 2026. Other products stay untouched.
ASSIGNMENTS = {
    'anthurium_flowering': [851, 852, 855],
    'araucaria': [853],
    'dieffenbachia_reflector': [856],
    'zamioculcas': [857, 566],
    'cordyline_kiwi': [865],
    'nephrolepis': [859, 860, 506, 155, 156, 157, 158],
    'tradescantia_zebrina': [867],
    'chlorophytum': [861, 807, 233],
    'aglaonema': [2, 12, 753, 3, 4, 5, 6, 7, 8, 9, 10, 11],
    'monstera_deliciosa': [149, 815],
    'ficus_elastica': [740, 685, 215, 216, 684, 217],
    'spathiphyllum': [738, 183, 184],
    'phalaenopsis': [161, 162, 163],
    'dionaea': [87],
    'nepenthes': [154],
    'poinsettia': [168],
    'adenium': [854],
    'alocasia': [862, 847, 16, 736, 598, 19, 20, 21, 22, 23, 24, 25, 535, 17, 483, 26, 27, 28, 29, 30, 31],
    'sansevieria': [174, 175, 177, 178, 749],
    'ficus_benjamina': [486, 755, 198, 199, 200, 201, 202, 203, 197],
    'citrus': [866],
    'citrus_other': [117, 118, 127, 129, 128, 133, 132, 246, 135, 137, 136, 648, 138],
    'schlumbergera': [90, 610],
}

def q(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"

def main() -> None:
    live = json.loads(LIVE.read_text())
    names = {int(code): name for code, name in live.items()}
    product_codes = [code for codes in ASSIGNMENTS.values() for code in codes]
    if len(product_codes) != len(set(product_codes)):
        raise SystemExit('Duplicate product code in curated content assignments')
    if not set(product_codes) <= set(names):
        raise SystemExit('Assignment missing from live catalog')
    rows = []
    for profile_key, codes in ASSIGNMENTS.items():
        source, latin, short, description, care, light, watering = PROFILES[profile_key]
        for code in codes:
            rows.append((code, names[code], latin, short, description, care, light, watering, profile_key, source))
    lines = [
        '-- Curated content for exactly identified plant cards. Sources in docs/catalog-curated-content-2026-10-12.md.',
        '-- The migration is idempotent: it fills only blank fields of same-named plant product_codes.',
        '-- Prices, stock, status, names, categories, media, size and external mappings are untouched.',
        'CREATE TEMP TABLE curated_plant_content (product_code bigint PRIMARY KEY, expected_name text NOT NULL, latin text NOT NULL, short_text text NOT NULL, full_text text NOT NULL, care_text text NOT NULL, light text NOT NULL, watering text NOT NULL) ON COMMIT DROP;',
        'INSERT INTO curated_plant_content(product_code,expected_name,latin,short_text,full_text,care_text,light,watering) VALUES',
    ]
    values = []
    for code, name, latin, short, description, care, light, watering, key, source in rows:
        values.append('  (' + ','.join([str(code), q(name), q(latin), q(short), q(description), q(care), q(light), q(watering)]) + ')')
    lines.append(',\n'.join(values) + ';')
    lines.extend([
        '',
        '-- An edited title or a non-plant section causes a safe skip. Existing manager text wins.',
        'UPDATE products AS p SET',
        "  latin_name = CASE WHEN BTRIM(p.latin_name) = '' THEN c.latin ELSE p.latin_name END,",
        "  short_description = CASE WHEN BTRIM(p.short_description) = '' THEN c.short_text ELSE p.short_description END,",
        "  description = CASE WHEN BTRIM(p.description) = '' THEN c.full_text ELSE p.description END,",
        "  care_instructions = CASE WHEN BTRIM(p.care_instructions) = '' THEN c.care_text ELSE p.care_instructions END,",
        "  light_level = CASE WHEN COALESCE(BTRIM(p.light_level),'') = '' THEN c.light ELSE p.light_level END,",
        "  watering = CASE WHEN COALESCE(BTRIM(p.watering),'') = '' THEN c.watering ELSE p.watering END,",
        '  updated_at = CURRENT_TIMESTAMP',
        'FROM curated_plant_content AS c',
        "WHERE p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",
        "  AND (BTRIM(p.latin_name) = '' OR BTRIM(p.short_description) = '' OR BTRIM(p.description) = '' OR BTRIM(p.care_instructions) = ''",
        "       OR COALESCE(BTRIM(p.light_level),'') = '' OR COALESCE(BTRIM(p.watering),'') = '');",
    ])
    lines.extend([
        '',
        '-- Remove obsolete empty "Для пальм" soil leaf. Old public URL has a 301 redirect.',
        'DELETE FROM categories AS category USING categories AS parent',
        "WHERE category.slug = 'paml' AND category.parent_id = parent.id AND parent.slug = 'soil'",
        '  AND NOT EXISTS (SELECT 1 FROM products AS product WHERE product.category_id = category.id)',
        '  AND NOT EXISTS (SELECT 1 FROM categories AS child WHERE child.parent_id = category.id)',
        '  AND NOT EXISTS (SELECT 1 FROM catalog_filters AS filter WHERE filter.category_id = category.id);',
    ])
    MIGRATION.write_text('\n'.join(lines) + '\n')
    preview_rows = ',\n'.join(f'  ({code},{q(name)},{q(key)})' for code, name, latin, short, description, care, light, watering, key, source in rows)
    PREVIEW.write_text(
        '-- Read-only preview against the target database immediately before release.\n'
        'WITH target(product_code,expected_name,profile) AS (VALUES\n'
        + preview_rows + '\n)\n'
        'SELECT target.product_code, target.expected_name, target.profile,\n'
        '  CASE WHEN product.id IS NULL THEN \'guard_skip\'\n'
        "       WHEN BTRIM(product.latin_name) = '' OR BTRIM(product.short_description) = ''\n"
        "         OR BTRIM(product.description) = '' OR BTRIM(product.care_instructions) = ''\n"
        "         OR COALESCE(BTRIM(product.light_level),'') = '' OR COALESCE(BTRIM(product.watering),'') = ''\n"
        "       THEN 'will_update' ELSE 'already_complete' END AS result\n"
        'FROM target LEFT JOIN products AS product\n'
        "  ON product.product_code = target.product_code AND product.name = target.expected_name AND product.catalog_section = 'plants'\n"
        'ORDER BY target.product_code;\n'
    )
    print(f'Wrote {MIGRATION}: {len(rows)} curated cards')

if __name__ == '__main__':
    main()
