#!/usr/bin/env python3
"""Generate guarded, blank-only content for identifiable remaining plants."""
from __future__ import annotations

from pathlib import Path

from generate_curated_plant_content import q
from generate_curated_plant_content_third import passport_advice

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'timeweb/migrations/20261012_zzzzzz_curated_plant_content_fourth.sql'

# code: exact site name, primary source, Latin name (genus where SKU lacks species),
# short description, full description, care, light enum, watering enum.
PROFILES = {
  50: ('Бонсай Citrus Myrtifolia(апельсин)', 'https://www.rhs.org.uk/plants/212951/citrus-myrtifolia/details', 'Citrus × aurantium',
       'Миниатюрное цитрусовое дерево, сформированное как бонсай.',
       'Чинотто, известный в продаже как Citrus myrtifolia, выращивают ради плотной вечнозелёной кроны, ароматных цветков и небольших плодов. Появление плодов зависит от условий содержания.',
       'Поставьте в очень светлое место, защищая от жгучего полуденного солнца. В сезон роста поливайте по мере подсыхания поверхности грунта, без застоя воды; зимой полив сократите. Обеспечьте цитрусовому прохладную светлую зимовку.', 'sunny', 'moderate'),
  55: ('Бонсай Атропурпуреум (Клен японский)', 'https://www.rhs.org.uk/plants/acer/japanese-maples/growing-guide', "Acer palmatum 'Atropurpureum'",
       'Японский клён с пурпурной листвой в форме бонсай.',
       'Листопадный японский клён меняет окраску листвы в течение сезона. На зиму листья опадают; это нормальный сезонный цикл, а не признак гибели растения.',
       'Держите на открытом воздухе в светлой полутени, защищая листья от палящего полуденного солнца. В сезон роста поливайте, когда верхний слой начинает подсыхать; не пересушивайте маленький горшок. Зимой защищайте корни в контейнере от сильного промерзания.', 'diffused', 'frequent'),
  57: ('Бонсай Зантоксилум', 'https://www.rhs.org.uk/plants/19195/zanthoxylum-piperitum/details', 'Zanthoxylum',
       'Бонсай из зантоксилума с мелкой ароматной листвой.',
       'Зантоксилумы — древесные растения с ароматной листвой. Точный вид этого экземпляра по торговому названию не определён, поэтому листопадность и плодоношение не обещаем.',
       'Поставьте в светлое место с несколькими часами мягкого солнца. Поливайте после начала подсыхания поверхности хорошо дренированного субстрата; малый объём бонсайного горшка проверяйте регулярно. Режим зимовки зависит от вида.', 'sunny', 'moderate'),
  62: ('Бонсай Кипарис (Chamaecyparis)', 'https://www.rhs.org.uk/plants/types/conifers/growing-guide', 'Chamaecyparis',
       'Вечнозелёный хвойный бонсай из кипарисовика.',
       'Кипарисовики имеют мелкую чешуевидную хвою и формируются обрезкой. Вид и сорт этого растения в карточке не указаны.',
       'Поставьте на открытом воздухе в светлое место; в комнате постоянное содержание не планируйте. Поливайте после лёгкого подсыхания поверхности субстрата, не допуская полного пересыхания корневого кома. Зимой защитите небольшой контейнер от сильного промерзания.', 'sunny', 'moderate'),
  63: ('Бонсай Котонеастер (Кизильник)', 'https://www.rhs.org.uk/plants/cotoneaster/growing-guide', 'Cotoneaster',
       'Кизильник, сформированный в миниатюрное дерево.',
       'Кизильники ценят за мелкую листву и способность переносить формировку. У разных видов листва бывает вечнозелёной или листопадной; цветение и ягоды у конкретного экземпляра не гарантированы.',
       'Выберите для бонсая место на открытом воздухе с солнцем или лёгкой полутенью. Поливайте после подсыхания поверхности грунта, не оставляя корни в воде; зимой защищайте корни в маленьком контейнере от сильного промерзания.', 'sunny', 'moderate'),
  64: ('Бонсай Лжелиственница', 'https://powo.science.kew.org/taxon/urn:lsid:ipni.org:names:677125-1', 'Pseudolarix amabilis',
       'Листопадный хвойный бонсай из псевдолиственницы.',
       'Псевдолиственница имеет мягкую хвою, которая осенью желтеет и опадает. Kew принимает название Pseudolarix amabilis; лжелиственница нуждается в сезонном покое.',
       'Поставьте на открытом воздухе в светлое место. Поливайте, когда поверхность субстрата начинает подсыхать, не допуская длительного пересыхания маленького горшка; зимой поливайте реже и защищайте контейнер от сильного промерзания.', 'sunny', 'frequent'),
  70: ('Бонсай Можжевельник', 'https://www.rhs.org.uk/plants/types/conifers/growing-guide', 'Juniperus',
       'Вечнозелёный можжевельник, сформированный как бонсай.',
       'Можжевельник сохраняет хвою круглый год. Его точный вид и сорт без маркировки не определяются, поэтому цвет и форма хвои могут отличаться от иллюстраций.',
       'Держите на открытом воздухе на солнце. Поливайте после подсыхания поверхности хорошо дренированного субстрата, не оставляя воду в поддоне. Зимой защищайте корни небольшого контейнера от сильного промерзания.', 'sunny', 'moderate'),
  75: ('Бонсай Пираканта', 'https://www.rhs.org.uk/plants/pyracantha/growing-guide', 'Pyracantha',
       'Пираканта в форме бонсая с плотной мелкой листвой.',
       'Пираканта — колючий кустарник, который может цвести и образовывать яркие плоды. У этого экземпляра вид и сорт не указаны; плодоношение зависит от условий и формировки.',
       'Держите на открытом воздухе на солнце или в лёгкой полутени. Поливайте после начала подсыхания поверхности грунта, не заливая корни; маленький горшок проверяйте чаще в жару. Зимой защищайте корни в контейнере.', 'sunny', 'moderate'),
  76: ('Бонсай Подокарпус', 'https://www.rhs.org.uk/plants/13357/i-podocarpus-macrophyllus-i/details', 'Podocarpus',
       'Вечнозелёный бонсай из подокарпуса.',
       'Подокарпусы образуют плотную крону с узкими кожистыми листьями. Точный вид по названию этого товара не установлен.',
       'Поставьте в яркий рассеянный свет, летом можно вынести в защищённое место. Поливайте после лёгкого подсыхания поверхности грунта без застоя воды; зимой берегите от холодного ветра и промерзания контейнера.', 'diffused', 'moderate'),
  79: ('Бонсай Сагереция чайная', 'https://www.rhs.org.uk/plants/83073/sageretia-thea/details', 'Sageretia thea',
       'Сагереция чайная — миниатюрное деревце с мелкими листьями.',
       'Сагереция чайная, также известная как Sageretia theezans, выращивается как бонсай с тонкими побегами и мелкой листвой. Название вида подтверждено RHS.',
       'Выберите яркий рассеянный свет и тепло, защищайте от холодных сквозняков. Поливайте после лёгкого подсыхания поверхности субстрата, не пересушивая весь маленький горшок; обеспечьте свободный сток воды.', 'diffused', 'frequent'),
  81: ('Бонсай Самшит прямой', 'https://www.rhs.org.uk/plants/box/growing-guide', 'Buxus',
       'Самшит в форме прямостоячего бонсая.',
       'Самшиты — вечнозелёные кустарники с мелкими листьями, хорошо переносящие стрижку. «Прямой» описывает форму бонсая, а не отдельный ботанический вид.',
       'Держите на открытом воздухе на солнце или в лёгкой полутени. Поливайте после подсыхания поверхности субстрата, не оставляя корни в воде; зимой защитите маленький контейнер от сильного промерзания.', 'sunny', 'moderate'),
  82: ('Бонсай Самшит Харланда', 'https://www.rhs.org.uk/plants/205379/buxus-harlandii-hance/details', 'Buxus harlandii',
       'Самшит Харланда, сформированный как бонсай.',
       'Buxus harlandii — вечнозелёный самшит с небольшой плотной листвой. В форме бонсая его крону поддерживают аккуратной регулярной обрезкой.',
       'Выберите светлое место с защитой от резкого полуденного солнца. Поливайте после начала подсыхания поверхности субстрата, не позволяя маленькому горшку надолго пересохнуть; зимой берегите корни от промерзания.', 'diffused', 'moderate'),
  134: ('Ливистона', 'https://www.rhs.org.uk/plants/10385/livistona-chinensis/details', 'Livistona',
        'Пальма ливистона с веерными листьями.',
        'Ливистоны — пальмы с крупными рассечёнными веерными листьями. Точный вид растения в названии товара не указан.',
        'Поставьте в яркий рассеянный свет и берегите от холодных сквозняков. Во время роста поливайте после лёгкого подсыхания поверхности грунта; зимой полив сократите и не оставляйте воду в поддоне.', 'diffused', 'moderate'),
  140: ('Микросорум Крокодилус', 'https://www.rhs.org.uk/plants/251695/microsorum-musifolium-crocodyllus/details', "Microsorum musifolium 'Crocodyllus'",
        'Папоротник с кожистыми листьями и рисунком жилок, напоминающим кожу крокодила.',
        'Микросорум Crocodyllus — эпифитный папоротник с длинными цельными вайями. Молодые листья ярко-зелёные, а рельеф жилок становится заметнее с возрастом.',
        'Поставьте в яркий рассеянный свет без прямого солнца и холодных сквозняков. Поливайте после лёгкого подсыхания поверхности рыхлого субстрата, поддерживайте влажность воздуха и не оставляйте горшок в воде.', 'diffused', 'moderate'),
  188: ('Тилландсия', 'https://www.rhs.org.uk/plants/types/houseplants/air-plants', 'Tillandsia',
        'Воздушное растение, которое обычно выращивают без почвы.',
        'Тилландсии закрепляются на опоре корнями и получают влагу через листья. Вид конкретного экземпляра не указан; потребность в воде может различаться.',
        'Дайте яркий рассеянный свет и хорошее движение воздуха. Поливайте мягкой водой: кратко погружайте растение, затем дайте полностью просохнуть и не оставляйте воду в розетке. В прохладе поливайте реже.', 'diffused', 'frequent'),
}


def build() -> str:
    if len(PROFILES) != 15:
        raise ValueError('Unexpected profile count')
    rows = []
    for code, (name, source, latin, short, description, care, light, watering) in sorted(PROFILES.items()):
        if not source.startswith('https://') or not all((name, latin, short, description, care, light, watering)):
            raise ValueError(f'Incomplete profile {code}')
        guide_light, guide_water = passport_advice((source, latin, short, description, care, light, watering))
        rows.append('  (' + ','.join([str(code)] + [q(v) for v in (name, latin, short, description, care, light, watering, guide_light, guide_water)]) + ')')
    return '\n'.join([
        '-- Fourth source-backed exact-name plant content batch; blank fields only.',
        'CREATE TEMP TABLE curated_plant_content_fourth (product_code bigint PRIMARY KEY, expected_name text NOT NULL, latin text NOT NULL, short_text text NOT NULL, full_text text NOT NULL, care_text text NOT NULL, light text NOT NULL, watering text NOT NULL, guide_light text NOT NULL, guide_water text NOT NULL) ON COMMIT DROP;',
        'INSERT INTO curated_plant_content_fourth(product_code,expected_name,latin,short_text,full_text,care_text,light,watering,guide_light,guide_water) VALUES',
        ',\n'.join(rows) + ';',
        'UPDATE products AS p SET',
        " latin_name = CASE WHEN BTRIM(COALESCE(p.latin_name,'')) = '' THEN c.latin ELSE p.latin_name END,",
        " short_description = CASE WHEN BTRIM(COALESCE(p.short_description,'')) = '' THEN c.short_text ELSE p.short_description END,",
        " description = CASE WHEN BTRIM(COALESCE(p.description,'')) = '' THEN c.full_text ELSE p.description END,",
        " care_instructions = CASE WHEN BTRIM(COALESCE(p.care_instructions,'')) = '' THEN c.care_text ELSE p.care_instructions END,",
        " light_level = CASE WHEN BTRIM(COALESCE(p.light_level,'')) = '' THEN c.light ELSE p.light_level END,",
        " watering = CASE WHEN BTRIM(COALESCE(p.watering,'')) = '' THEN c.watering ELSE p.watering END,",
        " plant_passport = COALESCE(p.plant_passport,'{}'::jsonb)",
        "  || CASE WHEN BTRIM(COALESCE(p.plant_passport->>'lighting','')) = '' THEN jsonb_build_object('lighting',c.guide_light) ELSE '{}'::jsonb END",
        "  || CASE WHEN BTRIM(COALESCE(p.plant_passport->>'watering','')) = '' THEN jsonb_build_object('watering',c.guide_water) ELSE '{}'::jsonb END,",
        ' updated_at = CURRENT_TIMESTAMP',
        'FROM curated_plant_content_fourth AS c',
        "WHERE p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",
        " AND (BTRIM(COALESCE(p.latin_name,''))='' OR BTRIM(COALESCE(p.short_description,''))='' OR BTRIM(COALESCE(p.description,''))='' OR BTRIM(COALESCE(p.care_instructions,''))=''",
        "  OR BTRIM(COALESCE(p.light_level,''))='' OR BTRIM(COALESCE(p.watering,''))='' OR BTRIM(COALESCE(p.plant_passport->>'lighting',''))='' OR BTRIM(COALESCE(p.plant_passport->>'watering',''))='');",
        '',
    ])


if __name__ == '__main__':
    OUT.write_text(build())
    print(f'Generated {len(PROFILES)} guarded profiles in {OUT}')
