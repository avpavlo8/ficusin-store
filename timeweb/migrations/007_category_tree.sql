CREATE TABLE IF NOT EXISTS categories (
    id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT REFERENCES categories(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    active SMALLINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE products ADD COLUMN IF NOT EXISTS category_id BIGINT REFERENCES categories(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS categories_parent_idx ON categories(parent_id, sort_order, name);
CREATE INDEX IF NOT EXISTS products_category_idx ON products(category_id);

INSERT INTO categories(name, slug, sort_order) VALUES
 ('Растения','plants',10),('Грунт','soil',20),('Удобрения','fertilizer',30),
 ('Кашпо и горшки','pots',40),('Аксессуары','accessories',50)
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name, sort_order=EXCLUDED.sort_order;

INSERT INTO categories(parent_id,name,slug,sort_order)
SELECT id,'Комнатные растения','indoor-plants',10 FROM categories WHERE slug='plants'
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,parent_id=EXCLUDED.parent_id;

WITH parent AS (SELECT id FROM categories WHERE slug='indoor-plants'), kinds(name,slug,sort_order) AS (VALUES
 ('Аглаонема','aglaonema',10),('Алоказия','alocasia',20),('Ананас','pineapple',30),('Антуриум','anthurium',40),
 ('Асплениум','asplenium',50),('Бамбук','bamboo',60),('Бокарнея','beaucarnea',70),('Бонсай','bonsai',80),
 ('Гибискус','hibiscus',90),('Даваллия','davallia',100),('Дипсис','dypsis',110),('Диффенбахия','dieffenbachia',120),
 ('Драцена','dracaena',130),('Замиокулькас','zamioculcas',140),('Кактус','cactus',150),('Каламондин','calamondin',160),
 ('Калатея','calathea',170),('Клузия','clusia',180),('Кордилина','cordyline',190),('Крассула','crassula',200),
 ('Крестовник','senecio',210),('Кротон','croton',220),('Кумкват','kumquat',230),('Лавр','laurel',240),
 ('Лаймкват','limequat',250),('Ливистона','livistona',260),('Лиметта','limetta',270),('Лимон','lemon',280),
 ('Маранта','maranta',290),('Микросорум','microsorum',300),('Мирсина','myrsine',310),('Монстера','monstera',320),
 ('Муса','musa',330),('Непентес','nepenthes',340),('Нефролепис','nephrolepis',350),('Олива','olive',360),
 ('Орхидея','orchid',370),('Пахира','pachira',380),('Пеперомия','peperomia',390),('Платицериум','platycerium',400),
 ('Рипсалис','rhipsalis',410),('Роза','rose',420),('Сансевиерия','sansevieria',430),('Сингониум','syngonium',440),
 ('Спатифиллум','spathiphyllum',450),('Стрелиция','strelitzia',460),('Суккуленты','succulents',470),('Тилландсия','tillandsia',480),
 ('Традесканция','tradescantia',490),('Фикус','ficus',500),('Филодендрон','philodendron',510),('Фиттония','fittonia',520),
 ('Хамедорея','chamaedorea',530),('Хедера','hedera',540),('Хлорофитум','chlorophytum',550),('Хойя','hoya',560),
 ('Цикас','cycas',570),('Циртомиум','cyrtomium',580),('Шеффлера','schefflera',590),('Эпипремнум','epipremnum',600),
 ('Юкка','yucca',610),('Другие растения','other-indoor-plants',999)
)
INSERT INTO categories(parent_id,name,slug,sort_order)
SELECT parent.id,kinds.name,kinds.slug,kinds.sort_order FROM parent CROSS JOIN kinds
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,parent_id=EXCLUDED.parent_id,sort_order=EXCLUDED.sort_order;

UPDATE products p SET category_id=c.id
FROM categories c WHERE c.slug = CASE
 WHEN LOWER(p.name) LIKE 'аглаонема%' THEN 'aglaonema' WHEN LOWER(p.name) LIKE 'алоказия%' THEN 'alocasia'
 WHEN LOWER(p.name) LIKE 'ананас%' THEN 'pineapple' WHEN LOWER(p.name) LIKE 'антуриум%' THEN 'anthurium'
 WHEN LOWER(p.name) LIKE 'асплениум%' THEN 'asplenium' WHEN LOWER(p.name) LIKE 'бамбук%' THEN 'bamboo'
 WHEN LOWER(p.name) LIKE 'бокарнея%' THEN 'beaucarnea' WHEN LOWER(p.name) LIKE 'бонсай%' THEN 'bonsai'
 WHEN LOWER(p.name) LIKE 'гибискус%' THEN 'hibiscus' WHEN LOWER(p.name) LIKE 'даваллия%' THEN 'davallia'
 WHEN LOWER(p.name) LIKE 'дипсис%' THEN 'dypsis' WHEN LOWER(p.name) LIKE 'диффенбахи%' THEN 'dieffenbachia'
 WHEN LOWER(p.name) LIKE 'драцена%' THEN 'dracaena' WHEN LOWER(p.name) LIKE 'замиокулькас%' THEN 'zamioculcas'
 WHEN LOWER(p.name) LIKE 'кактус%' THEN 'cactus' WHEN LOWER(p.name) LIKE 'каламондин%' THEN 'calamondin'
 WHEN LOWER(p.name) LIKE 'калатея%' THEN 'calathea' WHEN LOWER(p.name) LIKE 'клузия%' THEN 'clusia'
 WHEN LOWER(p.name) LIKE 'кордилина%' THEN 'cordyline' WHEN LOWER(p.name) LIKE 'крассула%' THEN 'crassula'
 WHEN LOWER(p.name) LIKE 'крестовник%' THEN 'senecio' WHEN LOWER(p.name) LIKE 'кротон%' THEN 'croton'
 WHEN LOWER(p.name) LIKE 'кумкват%' THEN 'kumquat' WHEN LOWER(p.name) LIKE 'лавр%' THEN 'laurel'
 WHEN LOWER(p.name) LIKE 'лаймкват%' THEN 'limequat' WHEN LOWER(p.name) LIKE 'ливистона%' THEN 'livistona'
 WHEN LOWER(p.name) LIKE 'лиметта%' THEN 'limetta' WHEN LOWER(p.name) LIKE 'лимон%' THEN 'lemon'
 WHEN LOWER(p.name) LIKE 'маранта%' THEN 'maranta' WHEN LOWER(p.name) LIKE 'микросорум%' THEN 'microsorum'
 WHEN LOWER(p.name) LIKE 'мирсина%' THEN 'myrsine' WHEN LOWER(p.name) LIKE 'монстера%' THEN 'monstera'
 WHEN LOWER(p.name) LIKE 'муса%' THEN 'musa' WHEN LOWER(p.name) LIKE 'непентес%' THEN 'nepenthes'
 WHEN LOWER(p.name) LIKE 'нефролепис%' THEN 'nephrolepis' WHEN LOWER(p.name) LIKE 'олива%' THEN 'olive'
 WHEN LOWER(p.name) LIKE 'орхидея%' THEN 'orchid' WHEN LOWER(p.name) LIKE 'пахира%' THEN 'pachira'
 WHEN LOWER(p.name) LIKE 'пеперомия%' THEN 'peperomia' WHEN LOWER(p.name) LIKE 'платицериум%' THEN 'platycerium'
 WHEN LOWER(p.name) LIKE 'рипсалис%' THEN 'rhipsalis' WHEN LOWER(p.name) LIKE 'роза%' THEN 'rose'
 WHEN LOWER(p.name) LIKE 'сансевиерия%' THEN 'sansevieria' WHEN LOWER(p.name) LIKE 'сингониум%' THEN 'syngonium'
 WHEN LOWER(p.name) LIKE 'спатифиллум%' THEN 'spathiphyllum' WHEN LOWER(p.name) LIKE 'стрелиция%' THEN 'strelitzia'
 WHEN LOWER(p.name) LIKE 'суккулент%' THEN 'succulents' WHEN LOWER(p.name) LIKE 'тилландсия%' THEN 'tillandsia'
 WHEN LOWER(p.name) LIKE 'традесканция%' THEN 'tradescantia' WHEN LOWER(p.name) LIKE 'фикус%' THEN 'ficus'
 WHEN LOWER(p.name) LIKE 'филодендрон%' THEN 'philodendron' WHEN LOWER(p.name) LIKE 'фиттония%' THEN 'fittonia'
 WHEN LOWER(p.name) LIKE 'хамедорея%' THEN 'chamaedorea' WHEN LOWER(p.name) LIKE 'хедера%' THEN 'hedera'
 WHEN LOWER(p.name) LIKE 'хлорофитум%' THEN 'chlorophytum' WHEN LOWER(p.name) LIKE 'хойя%' THEN 'hoya'
 WHEN LOWER(p.name) LIKE 'цикас%' THEN 'cycas' WHEN LOWER(p.name) LIKE 'циртомиум%' THEN 'cyrtomium'
 WHEN LOWER(p.name) LIKE 'шеффлера%' THEN 'schefflera' WHEN LOWER(p.name) LIKE 'эпипремнум%' THEN 'epipremnum'
 WHEN LOWER(p.name) LIKE 'юкка%' THEN 'yucca' WHEN LOWER(p.name) LIKE 'статуэтка%' THEN 'accessories'
 ELSE 'other-indoor-plants' END
WHERE p.category_id IS NULL;
