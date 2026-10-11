-- The owner classifies the supplied Saby flower list by plant name. Keep
-- existing product choices unless they point to a generic plant category.
WITH parent AS (SELECT id FROM categories WHERE slug = 'indoor-plants'),
     kinds(name, slug, sort_order) AS (VALUES
       ('Цитрусовые', 'citrus', 155),
       ('Амариллис', 'amaryllis', 35),
       ('Композиции из растений', 'plant-compositions', 205),
       ('Азалия', 'azalea', 15),
       ('Адениум', 'adenium', 16),
       ('Араукария', 'araucaria', 36),
       ('Аспарагус', 'asparagus', 46),
       ('Аспидистра', 'aspidistra', 47),
       ('Венерина мухоловка', 'venus-flytrap', 85),
       ('Глоксиния', 'gloxinia', 95),
       ('Каладиум', 'caladium', 165),
       ('Калла', 'calla', 166),
       ('Кофе', 'coffee', 195),
       ('Мимоза', 'mimosa', 305),
       ('Мирт', 'myrtle', 315),
       ('Пальмы', 'palms', 365),
       ('Панданус', 'pandanus', 375),
       ('Педилантус', 'pedilanthus', 385),
       ('Петуния', 'petunia', 395),
       ('Пуансеттия', 'poinsettia', 405),
       ('Синнингия', 'sinningia', 445),
       ('Соланум', 'solanum', 446),
       ('Туя', 'thuja', 485),
       ('Фиалка', 'violet', 525),
       ('Флорариумы', 'florariums', 526),
       ('Шлюмбергера', 'schlumbergera', 595)
     )
INSERT INTO categories(parent_id, name, slug, sort_order)
SELECT parent.id, kinds.name, kinds.slug, kinds.sort_order FROM parent CROSS JOIN kinds
ON CONFLICT (slug) DO UPDATE SET parent_id = EXCLUDED.parent_id,
    name = EXCLUDED.name, sort_order = EXCLUDED.sort_order;

-- Genus-level citrus filters now roll up into the requested Citrus category.
UPDATE categories child SET parent_id = citrus.id
FROM categories citrus
WHERE citrus.slug = 'citrus'
  AND child.slug IN ('calamondin', 'kumquat', 'limequat', 'limetta', 'lemon')
  AND child.parent_id IS DISTINCT FROM citrus.id;

WITH citrus AS (SELECT id FROM categories WHERE slug = 'citrus'),
     kinds(name, slug, sort_order) AS (VALUES
       ('Лайм', 'lime', 255), ('Лимонелла', 'lemonella', 265)
     )
INSERT INTO categories(parent_id, name, slug, sort_order)
SELECT citrus.id, kinds.name, kinds.slug, kinds.sort_order FROM citrus CROSS JOIN kinds
ON CONFLICT (slug) DO UPDATE SET parent_id = EXCLUDED.parent_id,
    name = EXCLUDED.name, sort_order = EXCLUDED.sort_order;

WITH classified AS (
  SELECT p.id,
    CASE
      WHEN lower(p.name) LIKE 'бонсай%' THEN 'bonsai'
      WHEN lower(p.name) LIKE 'композици%' OR lower(p.name) LIKE 'лес большой%' THEN 'plant-compositions'
      WHEN lower(p.name) LIKE 'флорариум%' THEN 'florariums'
      WHEN lower(p.name) LIKE 'пальма%' OR lower(p.name) LIKE 'пальмы%' THEN 'palms'
      WHEN lower(p.name) LIKE 'венерина%' THEN 'venus-flytrap'
      WHEN lower(p.name) LIKE 'декабрист%' THEN 'schlumbergera'
      WHEN lower(p.name) LIKE 'кодиум%' THEN 'croton'
      WHEN lower(p.name) LIKE 'замиокулькус%' THEN 'zamioculcas'
      WHEN lower(p.name) LIKE 'диффенбахи%' THEN 'dieffenbachia'
      WHEN lower(p.name) LIKE 'пуансеттия%' THEN 'poinsettia'
      WHEN lower(p.name) LIKE 'азалия%' THEN 'azalea'
      WHEN lower(p.name) LIKE 'адениум%' THEN 'adenium'
      WHEN lower(p.name) LIKE 'амариллис%' THEN 'amaryllis'
      WHEN lower(p.name) LIKE 'араукария%' THEN 'araucaria'
      WHEN lower(p.name) LIKE 'аспарагус%' THEN 'asparagus'
      WHEN lower(p.name) LIKE 'аспидистра%' THEN 'aspidistra'
      WHEN lower(p.name) LIKE 'глоксиния%' THEN 'gloxinia'
      WHEN lower(p.name) LIKE 'каладиум%' THEN 'caladium'
      WHEN lower(p.name) LIKE 'калла%' THEN 'calla'
      WHEN lower(p.name) LIKE 'кофе%' THEN 'coffee'
      WHEN lower(p.name) LIKE 'мимоза%' THEN 'mimosa'
      WHEN lower(p.name) LIKE 'мирт%' THEN 'myrtle'
      WHEN lower(p.name) LIKE 'панданус%' THEN 'pandanus'
      WHEN lower(p.name) LIKE 'педилантус%' THEN 'pedilanthus'
      WHEN lower(p.name) LIKE 'петуния%' THEN 'petunia'
      WHEN lower(p.name) LIKE 'синнингия%' THEN 'sinningia'
      WHEN lower(p.name) LIKE 'соланум%' THEN 'solanum'
      WHEN lower(p.name) LIKE 'туя%' THEN 'thuja'
      WHEN lower(p.name) LIKE 'фиалка%' THEN 'violet'
      WHEN lower(p.name) LIKE 'шлюмбергера%' THEN 'schlumbergera'
      WHEN lower(p.name) LIKE 'лаймкват%' THEN 'limequat'
      WHEN lower(p.name) LIKE 'лайм%' THEN 'lime'
      WHEN lower(p.name) LIKE 'лимоннелла%' THEN 'lemonella'
      WHEN lower(p.name) LIKE 'лиметта%' THEN 'limetta'
      WHEN lower(p.name) LIKE 'лимон%' THEN 'lemon'
      WHEN lower(p.name) LIKE 'каламондин%' THEN 'calamondin'
      WHEN lower(p.name) LIKE 'кумкват%' THEN 'kumquat'
      WHEN lower(p.name) LIKE 'аглаонема%' THEN 'aglaonema'
      WHEN lower(p.name) LIKE 'алоказия%' THEN 'alocasia'
      WHEN lower(p.name) LIKE 'ананас%' THEN 'pineapple'
      WHEN lower(p.name) LIKE 'антуриум%' THEN 'anthurium'
      WHEN lower(p.name) LIKE 'асплениум%' THEN 'asplenium'
      WHEN lower(p.name) LIKE 'бамбук%' THEN 'bamboo'
      WHEN lower(p.name) LIKE 'бокарнея%' THEN 'beaucarnea'
      WHEN lower(p.name) LIKE 'гибискус%' THEN 'hibiscus'
      WHEN lower(p.name) LIKE 'даваллия%' THEN 'davallia'
      WHEN lower(p.name) LIKE 'дипсис%' THEN 'dypsis'
      WHEN lower(p.name) LIKE 'драцена%' THEN 'dracaena'
      WHEN lower(p.name) LIKE 'замиокулькас%' THEN 'zamioculcas'
      WHEN lower(p.name) LIKE 'кактус%' THEN 'cactus'
      WHEN lower(p.name) LIKE 'калатея%' THEN 'calathea'
      WHEN lower(p.name) LIKE 'клузия%' THEN 'clusia'
      WHEN lower(p.name) LIKE 'кордилина%' THEN 'cordyline'
      WHEN lower(p.name) LIKE 'крассула%' THEN 'crassula'
      WHEN lower(p.name) LIKE 'крестовник%' THEN 'senecio'
      WHEN lower(p.name) LIKE 'кротон%' THEN 'croton'
      WHEN lower(p.name) LIKE 'лавр%' THEN 'laurel'
      WHEN lower(p.name) LIKE 'ливистона%' THEN 'livistona'
      WHEN lower(p.name) LIKE 'маранта%' THEN 'maranta'
      WHEN lower(p.name) LIKE 'микросорум%' THEN 'microsorum'
      WHEN lower(p.name) LIKE 'мирсина%' THEN 'myrsine'
      WHEN lower(p.name) LIKE 'монстера%' THEN 'monstera'
      WHEN lower(p.name) LIKE 'муса%' THEN 'musa'
      WHEN lower(p.name) LIKE 'непентес%' THEN 'nepenthes'
      WHEN lower(p.name) LIKE 'нефролепис%' THEN 'nephrolepis'
      WHEN lower(p.name) LIKE 'олива%' THEN 'olive'
      WHEN lower(p.name) LIKE 'орхидея%' THEN 'orchid'
      WHEN lower(p.name) LIKE 'пахира%' THEN 'pachira'
      WHEN lower(p.name) LIKE 'пеперомия%' THEN 'peperomia'
      WHEN lower(p.name) LIKE 'платицериум%' THEN 'platycerium'
      WHEN lower(p.name) LIKE 'рипсалис%' THEN 'rhipsalis'
      WHEN lower(p.name) LIKE 'роза%' THEN 'rose'
      WHEN lower(p.name) LIKE 'сансевиерия%' THEN 'sansevieria'
      WHEN lower(p.name) LIKE 'сингониум%' THEN 'syngonium'
      WHEN lower(p.name) LIKE 'спатифиллум%' THEN 'spathiphyllum'
      WHEN lower(p.name) LIKE 'стрелиция%' THEN 'strelitzia'
      WHEN lower(p.name) LIKE 'суккулент%' THEN 'succulents'
      WHEN lower(p.name) LIKE 'тилландсия%' THEN 'tillandsia'
      WHEN lower(p.name) LIKE 'традесканция%' THEN 'tradescantia'
      WHEN lower(p.name) LIKE 'фикус%' THEN 'ficus'
      WHEN lower(p.name) LIKE 'филодендрон%' THEN 'philodendron'
      WHEN lower(p.name) LIKE 'фиттония%' THEN 'fittonia'
      WHEN lower(p.name) LIKE 'хамедорея%' THEN 'chamaedorea'
      WHEN lower(p.name) LIKE 'хедера%' THEN 'hedera'
      WHEN lower(p.name) LIKE 'хлорофитум%' THEN 'chlorophytum'
      WHEN lower(p.name) LIKE 'хойя%' THEN 'hoya'
      WHEN lower(p.name) LIKE 'цикас%' THEN 'cycas'
      WHEN lower(p.name) LIKE 'циртомиум%' THEN 'cyrtomium'
      WHEN lower(p.name) LIKE 'шеффлера%' THEN 'schefflera'
      WHEN lower(p.name) LIKE 'эпипремнум%' THEN 'epipremnum'
      WHEN lower(p.name) LIKE 'юкка%' THEN 'yucca'
      ELSE NULL
    END slug
  FROM products p
  LEFT JOIN categories current_category ON current_category.id = p.category_id
  WHERE p.category_id IS NULL
     OR current_category.slug IN ('plants', 'indoor-plants', 'other-indoor-plants')
)
UPDATE products p SET category_id = target.id
FROM classified, categories target
WHERE p.id = classified.id AND target.slug = classified.slug
  AND p.category_id IS DISTINCT FROM target.id;
