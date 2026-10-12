#!/usr/bin/env python3
"""Guarded second content batch for independently identified plants.

Product names were captured from the live admin. The source documentation lists
primary horticultural references. Existing copy and commerce fields are preserved.
"""
from __future__ import annotations
import json
from pathlib import Path
from generate_curated_plant_content import PROFILES as PRIOR_PROFILES, ASSIGNMENTS as PRIOR_ASSIGNMENTS, q

ROOT = Path(__file__).resolve().parents[1]
SNAPSHOT = ROOT / 'docs/catalog-curated-content-second-snapshot-2026-10-12.json'
MIGRATION = ROOT / 'timeweb/migrations/20261012_zz_curated_plant_content_second.sql'
PREVIEW = ROOT / 'scripts/catalog_curated_content_second_preview.sql'

# Source, Latin taxon, short copy, description, care, light enum, watering enum.
PROFILES = {
    'anthurium_bloom': PRIOR_PROFILES['anthurium_flowering'],
    'araucaria': PRIOR_PROFILES['araucaria'],
    'zamioculcas': PRIOR_PROFILES['zamioculcas'],
    'aglaonema': PRIOR_PROFILES['aglaonema'],
    'alocasia': PRIOR_PROFILES['alocasia'],
    'anthurium_foliage': (
        'https://www.rhs.org.uk/plants/anthuriums/how-to-grow-anthuriums', 'Anthurium',
        'Декоративнолистный антуриум с выразительной формой и фактурой листьев.',
        'Антуриумы различаются по форме и фактуре листвы: у декоративнолистных форм листья становятся главным акцентом. Точный вид и рисунок конкретного экземпляра следует уточнять по поставке.',
        'Поставьте в яркий рассеянный свет, защищая от прямого полуденного солнца. Поливайте после начала подсыхания верхнего слоя рыхлого грунта; сливайте воду из поддона. Поддерживайте тепло и защищайте от холодных сквозняков.',
        'diffused', 'moderate'),
    'aspidistra': (
        'https://www.rhs.org.uk/plants/aspidistra/how-to-grow-aspidistras', 'Aspidistra elatior',
        'Аспидистра с длинными плотными листьями, поднимающимися прямо от корневища.',
        'Аспидистра образует устойчивый куст из отдельных листьев на длинных черешках. Пёстрая окраска зависит от сорта; это растение хорошо подходит для места без палящего солнца.',
        'Поставьте в рассеянный свет или полутень. Поливайте после подсыхания верхней части грунта и не оставляйте горшок в воде. Пёстрым формам дайте больше мягкого света; зимой поливайте реже.',
        'diffused', 'moderate'),
    'beaucarnea': (
        'https://plants.ces.ncsu.edu/plants/beaucarnea-recurvata/', 'Beaucarnea recurvata',
        'Бокарнея с утолщённым основанием ствола и фонтаном узких листьев.',
        'У бокарнеи вода запасается в утолщённом основании ствола, а узкие листья дугой расходятся от верхушки. Несмотря на прозвище «нолина», это не настоящая пальма.',
        'Выберите очень светлое место и хорошо дренированный грунт. Летом поливайте обильно, но лишь после просыхания субстрата; зимой полив сильно сократите. Не оставляйте воду в кашпо.',
        'sunny', 'rare'),
    'areca': (
        'https://plants.ces.ncsu.edu/plants/chrysalidocarpus-lutescens/', 'Chrysalidocarpus lutescens',
        'Пальма арека с несколькими стеблями и дуговидными перистыми листьями.',
        'Арека, также известная как дипсис, растёт группой стеблей и формирует воздушную крону из перистых листьев. Таксономическое название Chrysalidocarpus lutescens включает прежнее Dypsis lutescens.',
        'Держите в тёплом месте с ярким рассеянным светом. Поливайте после лёгкого подсыхания поверхности грунта, без длительной пересушки и застоя воды. Берегите от горячего сухого воздуха и холодных сквозняков.',
        'diffused', 'moderate'),
    'codiaeum': (
        'https://plants.ces.ncsu.edu/plants/codiaeum-variegatum/', 'Codiaeum variegatum',
        'Кодиеум с яркой пёстрой листвой, которую часто называют кротоном.',
        'Комнатный «кротон» — это кодиеум пёстрый: его листья могут отличаться формой и сочетанием зелёного, жёлтого, красного и оранжевого в зависимости от сорта. Хороший свет помогает сохранить окраску.',
        'Поставьте в очень светлое место, защищая от резкого полуденного солнца после перестановки. В период роста поливайте после лёгкого подсыхания верха грунта; зимой реже. Избегайте переохлаждения и длительного застоя воды.',
        'diffused', 'moderate'),
    'olive': (
        'https://www.rhs.org.uk/fruit/olives/grow-your-own', 'Olea europaea',
        'Олива европейская с узкими серебристо-зелёными вечнозелёными листьями.',
        'Олива ценится за фактуру ствола и серебристый оттенок листвы. В контейнере она остаётся декоративным деревцем; плодоношение зависит от условий и не обещается для комнатного экземпляра.',
        'Дайте максимально светлое солнечное место и дренированный грунт. В тёплый сезон поливайте после подсыхания верхнего слоя; зимой держите прохладнее и сокращайте полив. Защитите горшок от сильного мороза.',
        'sunny', 'moderate'),
    'ficus_lyrata': (
        'https://plants.ces.ncsu.edu/plants/ficus-lyrata/', 'Ficus lyrata',
        'Фикус лирата с крупными кожистыми листьями характерной лировидной формы.',
        'Широкие листья фикуса лирата с заметными жилками создают скульптурный силуэт. Компактная форма «Бамбино» указана только в тех карточках, где она прямо названа.',
        'Поставьте в яркий рассеянный свет, защищая от жёсткого полуденного солнца. Поливайте после подсыхания верхнего слоя грунта и сливайте воду из поддона. Избегайте резких перепадов температуры и холодных сквозняков.',
        'diffused', 'moderate'),
    'ficus_microcarpa': (
        'https://plants.ces.ncsu.edu/plants/ficus-microcarpa/common-name/chinese-banyan/', 'Ficus microcarpa',
        'Фикус микрокарпа с плотной кроной из небольших глянцевых листьев.',
        'Фикус микрокарпа хорошо переносит формирующую стрижку и поэтому часто продаётся в виде комнатного деревца или бонсай. У формы «Гинсенг» декоративные утолщённые корни становятся частью силуэта.',
        'Держите в ярком рассеянном свете и тепле. Поливайте после частичного подсыхания грунта, не допуская застоя воды в поддоне. При формировании кроны удаляйте только отдельные вытянувшиеся побеги.',
        'diffused', 'moderate'),
    'ficus_benghalensis': (
        'https://plants.ces.ncsu.edu/plants/ficus-benghalensis/common-name/strangler-fig/', 'Ficus benghalensis',
        'Фикус бенгальский «Одри» с бархатистыми крупными листьями и светлыми жилками.',
        '«Одри» — комнатная форма фикуса бенгальского. Его широкие матовые листья и выраженные жилки создают спокойный графичный силуэт; крупному экземпляру потребуется место для роста кроны.',
        'Выберите яркий рассеянный свет и постоянное место без холодных сквозняков. Поливайте после подсыхания верхнего слоя грунта, сливая лишнюю воду. Протирайте листья от пыли мягкой влажной тканью.',
        'diffused', 'moderate'),
    'philodendron': (
        'https://www.rhs.org.uk/plants/philodendron/growing-guide', 'Philodendron',
        'Филодендрон с выразительной тропической листвой.',
        'Филодендроны бывают прямостоячими и вьющимися, а рисунок и окраска листьев зависят от сорта. Для этой группы важны тёплое место, мягкий свет и рыхлый субстрат; точную форму роста сверяйте с конкретной карточкой.',
        'Поставьте в яркий рассеянный свет и держите при комнатной температуре без холода. Поливайте после лёгкого подсыхания грунта, не оставляя корни в воде. Вьющимся формам предоставьте опору.',
        'diffused', 'moderate'),
    'yucca': (
        'https://www.rhs.org.uk/plants/yucca/growing-guide', 'Yucca',
        'Юкка с жёсткими мечевидными листьями, собранными в розетки на стволе.',
        'Комнатная юкка выглядит как небольшое архитектурное дерево: над прямым стволом располагаются розетки узких листьев. Количество стволов зависит от конкретного экземпляра.',
        'Поставьте в очень светлое место, летом защищая от резкого полуденного солнца. Используйте хорошо дренированный грунт; во время роста поливайте после подсыхания верхнего слоя, зимой гораздо реже.',
        'sunny', 'rare'),
    'dieffenbachia': (
        'https://plants.ces.ncsu.edu/plants/dieffenbachia-seguine/', 'Dieffenbachia',
        'Диффенбахия с крупными листьями и пёстрым рисунком.',
        'Диффенбахии выращивают ради крупных листьев с пятнами и штрихами разных оттенков. Конкретный рисунок зависит от сорта; растение не следует размещать там, где листья могут жевать дети или питомцы.',
        'Дайте яркий рассеянный свет без прямого солнца. Поливайте после подсыхания верхних нескольких сантиметров грунта и сливайте воду из поддона. Держите в тепле; при обрезке избегайте контакта с соком.',
        'diffused', 'moderate'),
    'dracaena': (
        'https://www.rhs.org.uk/plants/dracaena/how-to-grow-dracaena', 'Dracaena',
        'Драцена с розеткой длинных листьев на прямом древеснеющем стебле.',
        'Драцены постепенно вытягивают ствол и сохраняют листву ближе к его верхушке. Ширина и окраска листьев различаются у видов и сортов; точный вид по одному торговому названию не устанавливается.',
        'Поставьте в яркий рассеянный свет и берегите от холодных сквозняков. Поливайте, когда верхние 5 см грунта подсохнут; сливайте воду из поддона. Если кончики листьев буреют от водопроводной воды, используйте фильтрованную.',
        'diffused', 'moderate'),
    'azalea': (
        'https://www.rhs.org.uk/plants/rhododendron/indoor-azaleas', 'Rhododendron',
        'Комнатная азалия с плотной листвой и многочисленными цветками.',
        'Горшечные азалии относятся к роду рододендронов. Их ценят за обильное сезонное цветение; цвет и форма венчиков зависят от сорта, а штамбовая форма указана в названии соответствующей карточки.',
        'Держите в прохладном светлом месте без палящего солнца. Поддерживайте кислый субстрат равномерно влажным мягкой водой, не позволяя корневому кому полностью пересохнуть. После полива сливайте лишнюю воду.',
        'diffused', 'frequent'),
}

ASSIGNMENTS = {
    'anthurium_bloom': [575, 33, 34, 36, 37, 38, 39],
    'anthurium_foliage': [532, 644, 35],
    'aspidistra': [817, 805, 41, 42],
    'beaucarnea': [698, 48],
    'areca': [565, 91, 92],
    'zamioculcas': [798, 520, 106, 107, 108, 109, 110, 112, 113],
    'codiaeum': [475, 803, 490, 126],
    'olive': [516, 159, 160],
    'ficus_lyrata': [752, 689, 205],
    'ficus_microcarpa': [494, 207, 208, 209, 84],
    'ficus_benghalensis': [692, 523, 196],
    'philodendron': [579, 218, 219, 220, 221, 222, 223, 224, 225, 226, 769],
    'yucca': [541, 245],
    'araucaria': [40, 54, 545],
    'dieffenbachia': [93, 94, 95, 96, 97, 98, 99, 737],
    'dracaena': [100, 101, 102, 103, 105],
    'azalea': [13, 14, 15, 51, 53],
    'aglaonema': [1],
    'alocasia': [27],
}

def passport_advice(profile):
    """Lift source-backed light and water sentences from the curated care copy."""
    care = profile[4]
    sentences = [s.strip() + '.' for s in care.split('.') if s.strip()]
    light = next((s for s in sentences if any(x in s.lower() for x in ('свет', 'солнц', 'светл'))), None)
    water = next((s for s in sentences if any(x in s.lower() for x in ('полив', 'полива', 'влажн'))), None)
    if not light or not water:
        raise ValueError(f'Passport advice incomplete: {care}')
    return light, water

def passport_rows():
    names = {int(k): v for k, v in json.loads((ROOT / 'docs/catalog-curated-content-snapshot-2026-10-12.json').read_text()).items()}
    for key, codes in PRIOR_ASSIGNMENTS.items():
        for code in codes:
            yield code, names[code], *passport_advice(PRIOR_PROFILES[key])
    for code, name, _, _, _, _, _, _, key, _ in rows_for_snapshot():
        if code not in names:
            yield code, name, *passport_advice(PROFILES[key])

def rows_for_snapshot():
    names = {int(k): v for k, v in json.loads(SNAPSHOT.read_text()).items()}
    ids = [code for codes in ASSIGNMENTS.values() for code in codes]
    if len(ids) != len(set(ids)) or set(ids) != set(names):
        raise SystemExit('Duplicate or missing product code in second curated snapshot')
    for key, codes in ASSIGNMENTS.items():
        if key not in PROFILES:
            raise SystemExit(f'Missing source-backed profile: {key}')
        source, latin, short, description, care, light, watering = PROFILES[key]
        for code in codes:
            yield code, names[code], latin, short, description, care, light, watering, key, source

def main():
    rows = list(rows_for_snapshot())
    sql = [
        '-- Second batch: exact live product_code/name and plant section; blank fields only.',
        "-- Normalize only the verified trailing NBSP on product #27, then fill its previously skipped blanks.",
        "UPDATE products SET name = 'Алоказия Ред Секрет', updated_at = CURRENT_TIMESTAMP WHERE product_code = 27 AND catalog_section = 'plants' AND name = 'Алоказия Ред Секрет' || U&'\\00A0';",
        'CREATE TEMP TABLE curated_plant_content_second (product_code bigint PRIMARY KEY, expected_name text NOT NULL, latin text NOT NULL, short_text text NOT NULL, full_text text NOT NULL, care_text text NOT NULL, light text NOT NULL, watering text NOT NULL) ON COMMIT DROP;',
        'INSERT INTO curated_plant_content_second(product_code,expected_name,latin,short_text,full_text,care_text,light,watering) VALUES',
    ]
    sql.append(',\n'.join('  (' + ','.join([str(code), q(name), q(latin), q(short), q(description), q(care), q(light), q(watering)]) + ')' for code,name,latin,short,description,care,light,watering,key,source in rows) + ';')
    sql.extend([
        '',
        'UPDATE products AS p SET',
        "  latin_name = CASE WHEN BTRIM(p.latin_name) = '' THEN c.latin ELSE p.latin_name END,",
        "  short_description = CASE WHEN BTRIM(p.short_description) = '' THEN c.short_text ELSE p.short_description END,",
        "  description = CASE WHEN BTRIM(p.description) = '' THEN c.full_text ELSE p.description END,",
        "  care_instructions = CASE WHEN BTRIM(p.care_instructions) = '' THEN c.care_text ELSE p.care_instructions END,",
        "  light_level = CASE WHEN COALESCE(BTRIM(p.light_level),'') = '' THEN c.light ELSE p.light_level END,",
        "  watering = CASE WHEN COALESCE(BTRIM(p.watering),'') = '' THEN c.watering ELSE p.watering END,",
        '  updated_at = CURRENT_TIMESTAMP',
        'FROM curated_plant_content_second AS c',
        "WHERE p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",
        "  AND (BTRIM(p.latin_name) = '' OR BTRIM(p.short_description) = '' OR BTRIM(p.description) = '' OR BTRIM(p.care_instructions) = ''",
        "       OR COALESCE(BTRIM(p.light_level),'') = '' OR COALESCE(BTRIM(p.watering),'') = '');",
    ])
    passport = list(passport_rows())
    sql.extend([
        '',
        '-- Populate the public care guide passport without replacing any existing passport key.',
        'CREATE TEMP TABLE curated_plant_passport (product_code bigint PRIMARY KEY, expected_name text NOT NULL, lighting text NOT NULL, watering text NOT NULL) ON COMMIT DROP;',
        'INSERT INTO curated_plant_passport(product_code,expected_name,lighting,watering) VALUES',
        ',\n'.join('  (' + ','.join([str(code), q(name), q(light), q(water)]) + ')' for code,name,light,water in passport) + ';',
        'UPDATE products AS p SET',
        "  plant_passport = COALESCE(p.plant_passport, '{}'::jsonb)",
        "    || CASE WHEN BTRIM(COALESCE(p.plant_passport->>'lighting','')) = '' THEN jsonb_build_object('lighting', c.lighting) ELSE '{}'::jsonb END",
        "    || CASE WHEN BTRIM(COALESCE(p.plant_passport->>'watering','')) = '' THEN jsonb_build_object('watering', c.watering) ELSE '{}'::jsonb END,",
        '  updated_at = CURRENT_TIMESTAMP',
        'FROM curated_plant_passport AS c',
        "WHERE p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",
        "  AND (BTRIM(COALESCE(p.plant_passport->>'lighting','')) = '' OR BTRIM(COALESCE(p.plant_passport->>'watering','')) = '');",
    ])
    MIGRATION.write_text('\n'.join(sql) + '\n')
    preview = ['-- Read-only, run on target DB immediately before deploy.', 'WITH target(product_code,expected_name,profile) AS (VALUES']
    preview.append(',\n'.join(f'  ({code},{q(name)},{q(key)})' for code,name,latin,short,description,care,light,watering,key,source in rows))
    preview.extend([
        ') SELECT target.product_code,target.expected_name,target.profile,',
        "  CASE WHEN product.id IS NULL THEN 'guard_skip'",
        "       WHEN BTRIM(product.latin_name) = '' OR BTRIM(product.short_description) = ''",
        "         OR BTRIM(product.description) = '' OR BTRIM(product.care_instructions) = ''",
        "         OR COALESCE(BTRIM(product.light_level),'') = '' OR COALESCE(BTRIM(product.watering),'') = ''",
        "       THEN 'will_update' ELSE 'already_complete' END AS result",
        'FROM target LEFT JOIN products AS product',
        "  ON product.product_code = target.product_code AND product.name = target.expected_name AND product.catalog_section = 'plants'",
        'ORDER BY target.product_code;',
    ])
    PREVIEW.write_text('\n'.join(preview) + '\n')
    print(f'Generated second batch: {len(rows)} content cards, {len(passport)} guarded passport cards')

if __name__ == '__main__':
    main()
