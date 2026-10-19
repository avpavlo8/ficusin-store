-- Reviewed storefront collections and approved artwork, 19 October 2026.

-- Sources, exact identities and editorial decisions: docs/storefront-collections-2026-10-19.json.

-- No product names, prices, stock, measurements or optimal-light values are changed.

ALTER TABLE collections ADD COLUMN IF NOT EXISTS cover_position TEXT NOT NULL DEFAULT 'center';

ALTER TABLE collections ADD COLUMN IF NOT EXISTS show_on_home BOOLEAN NOT NULL DEFAULT TRUE;

INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_active,is_global,description) VALUES ('storefront_traits','Проверенные признаки для подборок','multi_enum','','technical','product',TRUE,FALSE,'Редакционно проверенные признаки: переносимость условий и форма растения. Источники и точные карточки: аудит подборок 19.10.2026. Не заменяет оптимальное освещение, высоту варианта или паспорт ухода.') ON CONFLICT(code) DO NOTHING;

INSERT INTO attribute_options(attribute_id,code,label,sort_order) SELECT definition.id,seed.code,seed.label,seed.sort_order FROM attribute_definitions definition CROSS JOIN (VALUES
 ('partial_shade','Переносит полутень',10),
 ('sunny_window','Для солнечного окна',20),
 ('ornamental_bloom','Декоративное цветение',30),
 ('trailing','Ампельная форма',40),
 ('support_climber','Для выращивания на опоре',50),
 ('patterned_foliage','Пёстрые или узорчатые листья',60),
 ('tree_form','Древесная форма для интерьера',70),
 ('fruiting','Плодовое растение',80),
 ('bonsai','Сформировано как бонсай',90),
 ('warm_bonsai','Бонсай без обязательной холодной зимовки',100),
 ('pet_safe_reviewed','Нетоксичность для кошек и собак проверена',110)
) seed(code,label,sort_order) WHERE definition.code='storefront_traits' ON CONFLICT(attribute_id,code) DO NOTHING;

INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,show_in_characteristics,is_excluded) SELECT category.id,definition.id,FALSE,FALSE,FALSE,FALSE,190,FALSE,FALSE,FALSE FROM categories category CROSS JOIN attribute_definitions definition WHERE category.slug='plants' AND definition.code='storefront_traits' ON CONFLICT(category_id,attribute_id) DO NOTHING;

CREATE TEMP TABLE reviewed_storefront_traits(product_code BIGINT PRIMARY KEY,expected_name TEXT,expected_latin TEXT,features JSONB) ON COMMIT DROP;

INSERT INTO reviewed_storefront_traits VALUES
 (2,'Аглаонема Вайт Ленс','Aglaonema ''White Lance''','["partial_shade", "patterned_foliage"]'::jsonb),
 (3,'Аглаонема Даймонд Бей','Aglaonema ''Diamond Bay''','["partial_shade", "patterned_foliage"]'::jsonb),
 (4,'Аглаонема Клед Моракот','Aglaonema ''Kled Morakot''','["patterned_foliage"]'::jsonb),
 (5,'Аглаонема Мария','Aglaonema ''Maria''','["partial_shade", "patterned_foliage"]'::jsonb),
 (6,'Аглаонема Мария Кристина','Aglaonema ''Maria Christina''','["partial_shade", "patterned_foliage"]'::jsonb),
 (7,'Аглаонема Пинк Аврора','Aglaonema ''Pink Aurora''','["patterned_foliage"]'::jsonb),
 (8,'Аглаонема Призрак','Aglaonema ''Ghost''','["patterned_foliage"]'::jsonb),
 (9,'Аглаонема Сиам Аврора','Aglaonema ''Siam Aurora''','["patterned_foliage"]'::jsonb),
 (10,'Аглаонема Сильвер Квин','Aglaonema ''Silver Queen''','["partial_shade", "patterned_foliage"]'::jsonb),
 (11,'Аглаонема Ферст Даймонд','Aglaonema ''First Diamond''','["patterned_foliage"]'::jsonb),
 (12,'Аглаонема Чайна Ред','Aglaonema ''China Red''','["patterned_foliage"]'::jsonb),
 (13,'Азалия','Rhododendron hybrids','["ornamental_bloom"]'::jsonb),
 (14,'Азалия на штамбе','Rhododendron hybrids','["ornamental_bloom"]'::jsonb),
 (32,'Ананас Корона','Ananas comosus (Corona)','["fruiting", "sunny_window"]'::jsonb),
 (33,'Антуриум Андре','Anthurium andraeanum','["ornamental_bloom"]'::jsonb),
 (34,'Антуриум Аляска','Anthurium ''Anthfraxk'' (Alaska)','["ornamental_bloom"]'::jsonb),
 (36,'Антуриум Колорадо','Anthurium ''Antheubp'' (Colorado)','["ornamental_bloom"]'::jsonb),
 (37,'Антуриум Лилли','Anthurium ''Anthdosdoh'' (Lilli)','["ornamental_bloom"]'::jsonb),
 (38,'Антуриум Луизиана','Anthurium ''AN2891370'' (Louisiana)','["ornamental_bloom"]'::jsonb),
 (39,'Антуриум Оранж Чемпион','Anthurium andraeanum ''Champion Orange''','["ornamental_bloom"]'::jsonb),
 (40,'Араукария разнолистная','Araucaria heterophylla','["pet_safe_reviewed", "tree_form"]'::jsonb),
 (41,'Аспидистра Милки Вэй','Aspidistra ''Milky Way''','["partial_shade", "patterned_foliage"]'::jsonb),
 (42,'Аспидистра высокая','Aspidistra elatior','["partial_shade", "pet_safe_reviewed"]'::jsonb),
 (43,'Асплениум Криспи Вейв','Asplenium nidus ''Crispy Wave''','["pet_safe_reviewed"]'::jsonb),
 (46,'Драцена Сандера «Лаки-бамбук»','Dracaena sanderiana','["partial_shade"]'::jsonb),
 (48,'Бокарнея (нолина)','Beaucarnea','["sunny_window"]'::jsonb),
 (50,'Бонсай Померанец миртолистный Миртифолия','Citrus × aurantium','["bonsai", "fruiting"]'::jsonb),
 (51,'Бонсай Азалия Сацуки','Rhododendron indicum','["bonsai", "ornamental_bloom"]'::jsonb),
 (53,'Бонсай Азалия Сацуки мелколистная','Rhododendron indicum','["bonsai", "ornamental_bloom"]'::jsonb),
 (54,'Бонсай Араукария разнолистная','Araucaria heterophylla','["bonsai", "pet_safe_reviewed"]'::jsonb),
 (55,'Бонсай Клён японский Атропурпуреум','Acer palmatum ''Atropurpureum''','["bonsai"]'::jsonb),
 (56,'Бонсай Гранат','Punica granatum','["bonsai", "fruiting"]'::jsonb),
 (57,'Бонсай Зантоксилум перечный','Zanthoxylum piperitum','["bonsai"]'::jsonb),
 (58,'Бонсай Вяз мелколистный','Ulmus parvifolia','["bonsai"]'::jsonb),
 (60,'Бонсай Кармона','Ehretia microphylla','["bonsai", "warm_bonsai"]'::jsonb),
 (62,'Бонсай Кипарисовик хиноки','Chamaecyparis obtusa','["bonsai"]'::jsonb),
 (63,'Бонсай Кизильник','Cotoneaster','["bonsai"]'::jsonb),
 (64,'Бонсай Лжелиственница прекрасная','Pseudolarix amabilis','["bonsai"]'::jsonb),
 (65,'Бонсай Бирючина китайская','Ligustrum sinense','["bonsai"]'::jsonb),
 (67,'Бонсай Метасеквойя','Metasequoia glyptostroboides','["bonsai"]'::jsonb),
 (68,'Бонсай Метасеквойя «Лес»','Metasequoia glyptostroboides','["bonsai"]'::jsonb),
 (70,'Бонсай Можжевельник','Juniperus','["bonsai"]'::jsonb),
 (72,'Бонсай Олива Сильвестрис','Olea europaea','["bonsai"]'::jsonb),
 (74,'Бонсай Падуб городчатый','Ilex crenata','["bonsai"]'::jsonb),
 (75,'Бонсай Пираканта','Pyracantha','["bonsai"]'::jsonb),
 (76,'Бонсай Подокарпус','Podocarpus','["bonsai"]'::jsonb),
 (77,'Бонсай Портулакария африканская','Portulacaria afra','["bonsai", "sunny_window", "warm_bonsai"]'::jsonb),
 (78,'Бонсай Радермахера китайская','Radermachera sinica','["bonsai", "warm_bonsai"]'::jsonb),
 (79,'Бонсай Сагереция чайная','Sageretia thea','["bonsai"]'::jsonb),
 (81,'Бонсай Самшит вечнозелёный, прямой ствол','Buxus sempervirens','["bonsai"]'::jsonb),
 (82,'Бонсай Самшит Харланда','Buxus harlandii','["bonsai"]'::jsonb),
 (83,'Бонсай Сосна горная','Pinus mugo','["bonsai"]'::jsonb),
 (84,'Бонсай Фикус микрокарпа','Ficus microcarpa','["bonsai", "warm_bonsai"]'::jsonb),
 (86,'Бонсай Яблоня домашняя','Malus domestica','["bonsai", "fruiting"]'::jsonb),
 (87,'Венерина мухоловка','Dionaea muscipula','["pet_safe_reviewed", "sunny_window"]'::jsonb),
 (88,'Гибискус (китайская роза)','Hibiscus × rosa-sinensis','["ornamental_bloom", "sunny_window"]'::jsonb),
 (89,'Даваллия','Davallia','["trailing"]'::jsonb),
 (90,'Декабрист (шлюмбергера)','Schlumbergera hybrids','["ornamental_bloom", "pet_safe_reviewed", "trailing"]'::jsonb),
 (91,'Арека (дипсис желтеющий)','Chrysalidocarpus lutescens','["pet_safe_reviewed"]'::jsonb),
 (93,'Диффенбахия Ами','Dieffenbachia ''Amy''','["patterned_foliage"]'::jsonb),
 (94,'Диффенбахия Бертина','Dieffenbachia ''Bertina''','["patterned_foliage"]'::jsonb),
 (96,'Диффенбахия Камилла','Dieffenbachia ''Camilla''','["patterned_foliage"]'::jsonb),
 (97,'Диффенбахия Компакта','Dieffenbachia ''Compacta''','["patterned_foliage"]'::jsonb),
 (98,'Диффенбахия Саммер Стайл','Dieffenbachia ''Summer Style''','["patterned_foliage"]'::jsonb),
 (99,'Диффенбахия Удом Сап','Dieffenbachia ''Udom Sap''','["patterned_foliage"]'::jsonb),
 (100,'Драцена Маргината','Dracaena marginata','["partial_shade"]'::jsonb),
 (102,'Драцена Маргината Маджента','Dracaena marginata ''Magenta''','["partial_shade", "patterned_foliage"]'::jsonb),
 (104,'Драцена Сандера, переплетённые стволы','Dracaena sanderiana','["partial_shade"]'::jsonb),
 (105,'Драцена душистая','Dracaena fragrans','["partial_shade"]'::jsonb),
 (106,'Замиокулькас','Zamioculcas zamiifolia','["partial_shade"]'::jsonb),
 (110,'Замиокулькас Зензи','Zamioculcas zamiifolia (Zenzi)','["partial_shade"]'::jsonb),
 (112,'Замиокулькас Равен','Zamioculcas zamiifolia ''Dowon'' (Raven)','["partial_shade"]'::jsonb),
 (117,'Каламондин','Citrus × microcarpa','["fruiting", "sunny_window"]'::jsonb),
 (118,'Каламондин на штамбе','Citrus × microcarpa','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (119,'Калатея, микс сортов','Goeppertia spp. (Calathea)','["patterned_foliage", "pet_safe_reviewed"]'::jsonb),
 (120,'Калла, микс','Zantedeschia spp.','["ornamental_bloom"]'::jsonb),
 (121,'Клузия','Clusia','["tree_form"]'::jsonb),
 (123,'Кофе Арабика','Coffea arabica','["fruiting", "tree_form"]'::jsonb),
 (124,'Крассула','Crassula','["sunny_window"]'::jsonb),
 (125,'Крестовник Роули','Curio rowleyanus (syn. Senecio rowleyanus)','["trailing"]'::jsonb),
 (126,'Кодиеум пёстрый (кротон)','Codiaeum variegatum','["patterned_foliage"]'::jsonb),
 (127,'Кумкват Нагами','Citrus japonica ''Nagami''','["fruiting", "sunny_window"]'::jsonb),
 (128,'Кумкват на штамбе','Citrus spp. (кумкват)','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (129,'Кумкват Полиандра','Citrus polyandra','["fruiting", "sunny_window"]'::jsonb),
 (130,'Лавр благородный','Laurus nobilis','["sunny_window", "tree_form"]'::jsonb),
 (131,'Лавр благородный, куст','Laurus nobilis','["sunny_window"]'::jsonb),
 (132,'Лаймкват Лейкленд','Citrus × floridana ''Lakeland''','["fruiting", "sunny_window"]'::jsonb),
 (133,'Лайм Рангпур','Citrus × limonia','["fruiting", "sunny_window"]'::jsonb),
 (135,'Лиметта Росса на штамбе','Citrus (Limetta Rossa)','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (136,'Лимон вариегатный','Citrus × limon (вариегатная форма)','["fruiting", "patterned_foliage", "sunny_window"]'::jsonb),
 (137,'Лимон Мейера','Citrus × limon ''Meyer''','["fruiting", "sunny_window"]'::jsonb),
 (138,'Лимоннелла','Citrus (Limonella)','["fruiting", "sunny_window"]'::jsonb),
 (139,'Маранта','Maranta','["patterned_foliage", "pet_safe_reviewed", "trailing"]'::jsonb),
 (144,'Мирсина на штамбе','Myrsine','["tree_form"]'::jsonb),
 (145,'Мирт','Myrtus communis','["sunny_window"]'::jsonb),
 (146,'Мирт на штамбе','Myrtus communis','["sunny_window", "tree_form"]'::jsonb),
 (147,'Мирт спиральный','Myrtus communis','["sunny_window", "tree_form"]'::jsonb),
 (148,'Монстера Манки Лиф','Monstera adansonii (Monkey Leaf)','["support_climber", "trailing"]'::jsonb),
 (149,'Монстера делициоза','Monstera deliciosa','["support_climber"]'::jsonb),
 (152,'Монстера Тайское созвездие','Monstera deliciosa ''Thai Constellation''','["patterned_foliage", "support_climber"]'::jsonb),
 (153,'Банан','Musa','["pet_safe_reviewed", "sunny_window"]'::jsonb),
 (154,'Непентес, микс','Nepenthes spp.','["trailing"]'::jsonb),
 (155,'Нефролепис Бостон Блонди','Nephrolepis exaltata (Boston Blond)','["pet_safe_reviewed"]'::jsonb),
 (158,'Нефролепис Тайгер','Nephrolepis exaltata (Tiger)','["patterned_foliage", "pet_safe_reviewed"]'::jsonb),
 (159,'Олива европейская','Olea europaea','["fruiting", "sunny_window"]'::jsonb),
 (160,'Олива европейская, штамб','Olea europaea','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (161,'Фаленопсис, 3 цветоноса','Phalaenopsis (гибриды)','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (162,'Фаленопсис Элегант, каскад, 2 цветоноса','Phalaenopsis (гибриды)','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (163,'Фаленопсис Элегант, каскад, микс','Phalaenopsis (гибриды)','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (164,'Набор пальм: арека и хамедорея, 2 горшка','Chrysalidocarpus (Dypsis) + Chamaedorea','["pet_safe_reviewed"]'::jsonb),
 (165,'Пахира акватика, плетёная','Pachira aquatica','["tree_form"]'::jsonb),
 (166,'Пеперомия, микс видов и сортов','Peperomia spp.','["pet_safe_reviewed"]'::jsonb),
 (168,'Пуансеттия — Рождественская звезда','Euphorbia pulcherrima','["ornamental_bloom"]'::jsonb),
 (170,'Рипсалис','Rhipsalis','["partial_shade", "trailing"]'::jsonb),
 (172,'Роза Кордана','Rosa (Kordana)','["ornamental_bloom", "pet_safe_reviewed", "sunny_window"]'::jsonb),
 (173,'Роза Патио','Rosa (Patio Group)','["ornamental_bloom", "pet_safe_reviewed", "sunny_window"]'::jsonb),
 (174,'Сансевиерия, микс видов и сортов','Dracaena (группа Sansevieria)','["partial_shade"]'::jsonb),
 (175,'Сансевиерия цилиндрическая Твист','Dracaena angolensis (Twist)','["partial_shade"]'::jsonb),
 (177,'Сансевиерия Дигиторум','Dracaena (Sansevieria, Digitorum)','["partial_shade"]'::jsonb),
 (178,'Сансевиерия Спагетти','Dracaena (Sansevieria, Spaghetti)','["partial_shade"]'::jsonb),
 (179,'Сингониум, микс видов и сортов','Syngonium spp.','["support_climber", "trailing"]'::jsonb),
 (180,'Глоксиния Соната','Sinningia speciosa (Sonata)','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (183,'Спатифиллум Чико','Spathiphyllum (Chico)','["ornamental_bloom", "partial_shade"]'::jsonb),
 (184,'Спатифиллум Шопен','Spathiphyllum ''Chopin''','["ornamental_bloom", "partial_shade"]'::jsonb),
 (185,'Драцена Сандера «Лаки-бамбук», композиция в воде','Dracaena sanderiana','["partial_shade"]'::jsonb),
 (186,'Стрелиция Николая','Strelitzia nicolai','["sunny_window"]'::jsonb),
 (188,'Тилландсия атмосферная, свисающая форма','Tillandsia sp.','["trailing"]'::jsonb),
 (190,'Традесканция зебрина','Tradescantia zebrina','["trailing"]'::jsonb),
 (192,'Фиалка узамбарская, микс','Streptocarpus sect. Saintpaulia (гибриды)','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (193,'Фикус Али','Ficus binnendijkii ''Alii''','["tree_form"]'::jsonb),
 (194,'Фикус Алтиссима','Ficus altissima','["tree_form"]'::jsonb),
 (195,'Фикус Амстел, несколько стволов','Ficus binnendijkii (Amstel)','["tree_form"]'::jsonb),
 (196,'Фикус бенгальский Одри','Ficus benghalensis ''Audrey''','["tree_form"]'::jsonb),
 (197,'Фикус Бенджамина Банди','Ficus benjamina ''Bundy''','["patterned_foliage", "tree_form"]'::jsonb),
 (198,'Фикус Бенджамина Грин Кинки','Ficus benjamina ''Green Kinky''','["tree_form"]'::jsonb),
 (199,'Фикус Бенджамина Кинки','Ficus benjamina ''Kinky''','["patterned_foliage", "tree_form"]'::jsonb),
 (200,'Фикус Бенджамина Кумани','Ficus benjamina','["tree_form"]'::jsonb),
 (201,'Фикус Бенджамина Лакки','Ficus benjamina','["patterned_foliage", "tree_form"]'::jsonb),
 (202,'Фикус Бенджамина Сафрана','Ficus benjamina','["patterned_foliage", "tree_form"]'::jsonb),
 (203,'Фикус Бенджамина Тарана','Ficus benjamina ''Tarana''','["tree_form"]'::jsonb),
 (204,'Фикус Бенджамина Голден Кинг','Ficus benjamina ''Golden King''','["patterned_foliage", "tree_form"]'::jsonb),
 (206,'Фикус микрокарпа Майя','Ficus microcarpa ''Maya''','["tree_form"]'::jsonb),
 (207,'Фикус микрокарпа, тонкие стволы','Ficus microcarpa','["tree_form"]'::jsonb),
 (208,'Фикус микрокарпа Гинсенг','Ficus microcarpa (Ginseng)','["bonsai", "warm_bonsai"]'::jsonb),
 (209,'Фикус микрокарпа Мокламе','Ficus microcarpa ''Moclame''','["tree_form"]'::jsonb),
 (210,'Фикус Бенджамина Наташа','Ficus benjamina ''Natasja''','["tree_form"]'::jsonb),
 (211,'Фикус микрокарпа Панда','Ficus microcarpa ''Panda''','["tree_form"]'::jsonb),
 (212,'Фикус эластика Руби','Ficus elastica ''Ruby''','["patterned_foliage", "tree_form"]'::jsonb),
 (213,'Фикус Бенджамина Твилайт','Ficus benjamina ''Twilight''','["patterned_foliage", "tree_form"]'::jsonb),
 (214,'Фикус эластика Шивериана','Ficus elastica ''Shivereana''','["patterned_foliage", "tree_form"]'::jsonb),
 (215,'Фикус эластика Абиджан','Ficus elastica ''Abidjan''','["tree_form"]'::jsonb),
 (216,'Фикус эластика Белиз','Ficus elastica ''Belize''','["patterned_foliage", "tree_form"]'::jsonb),
 (217,'Фикус эластика Тинеке','Ficus elastica ''Tineke''','["patterned_foliage", "tree_form"]'::jsonb),
 (218,'Филодендрон Биркин','Philodendron ''Birkin''','["patterned_foliage"]'::jsonb),
 (219,'Филодендрон Вайт Принцесс','Philodendron ''White Princess''','["patterned_foliage", "support_climber"]'::jsonb),
 (224,'Филодендрон Ринг оф Файер','Philodendron ''Ring of Fire''','["patterned_foliage"]'::jsonb),
 (225,'Филодендрон Пинк Принцесс','Philodendron ''Pink Princess''','["patterned_foliage", "support_climber"]'::jsonb),
 (226,'Филодендрон Бразил','Philodendron hederaceum ''Brasil''','["patterned_foliage", "support_climber", "trailing"]'::jsonb),
 (228,'Фиттония, микс сортов','Fittonia spp.','["patterned_foliage", "pet_safe_reviewed"]'::jsonb),
 (229,'Хамедорея изящная — Элеганс','Chamaedorea elegans','["partial_shade", "pet_safe_reviewed"]'::jsonb),
 (230,'Хедера — комнатный плющ','Hedera spp.','["partial_shade", "trailing"]'::jsonb),
 (233,'Хлорофитум хохлатый, полосатый','Chlorophytum comosum','["partial_shade", "patterned_foliage", "pet_safe_reviewed", "trailing"]'::jsonb),
 (234,'Хойя, микс видов и сортов','Hoya spp.','["ornamental_bloom", "support_climber", "trailing"]'::jsonb),
 (238,'Цикас — саговник отогнутый','Cycas revoluta','["sunny_window"]'::jsonb),
 (241,'Шеффлера, микс сортов','Heptapleurum spp. (Schefflera)','["tree_form"]'::jsonb),
 (242,'Шеффлера Герда','Heptapleurum arboricola ''Gerda''','["tree_form"]'::jsonb),
 (243,'Шеффлера Луизиана','Heptapleurum sp. (Schefflera)','["tree_form"]'::jsonb),
 (244,'Эпипремнум, микс сортов','Epipremnum spp.','["support_climber", "trailing"]'::jsonb),
 (245,'Юкка комнатная','Yucca sp.','["sunny_window"]'::jsonb),
 (246,'Лиметта Росса','Citrus (Limetta Rossa)','["fruiting", "sunny_window"]'::jsonb),
 (475,'Кодиеум пёстрый (кротон)','Codiaeum variegatum','["patterned_foliage"]'::jsonb),
 (482,'Сингониум, микс видов и сортов','Syngonium spp.','["support_climber", "trailing"]'::jsonb),
 (494,'Фикус микрокарпа Нитида','Ficus microcarpa ''Nitida''','["tree_form"]'::jsonb),
 (506,'Нефролепис Грин Леди','Nephrolepis exaltata ''Green Lady''','["pet_safe_reviewed"]'::jsonb),
 (510,'Глоксиния (синнингия)','Sinningia speciosa','["ornamental_bloom", "pet_safe_reviewed"]'::jsonb),
 (520,'Замиокулькас Хамелеон','Zamioculcas zamiifolia ''Chameleon''','["partial_shade"]'::jsonb),
 (522,'Фикус Амстел на круглой опоре','Ficus binnendijkii (Amstel)','["tree_form"]'::jsonb),
 (523,'Фикус бенгальский Одри, спиральный ствол','Ficus benghalensis ''Audrey''','["tree_form"]'::jsonb),
 (541,'Юкка комнатная, 3 ствола','Yucca sp.','["sunny_window"]'::jsonb),
 (549,'Бонсай Можжевельник','Juniperus','["bonsai"]'::jsonb),
 (554,'Бонсай Муррайя метельчатая','Murraya paniculata','["bonsai", "ornamental_bloom", "sunny_window", "warm_bonsai"]'::jsonb),
 (610,'Шлюмбергера — декабрист','Schlumbergera, комнатные гибриды','["ornamental_bloom", "pet_safe_reviewed", "trailing"]'::jsonb),
 (648,'Лимон на штамбе','Citrus × limon','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (685,'Фикус эластика Робуста','Ficus elastica ''Robusta''','["tree_form"]'::jsonb),
 (689,'Фикус лирата Бамбино','Ficus lyrata ''Bambino''','["tree_form"]'::jsonb),
 (698,'Бокарнея (нолина)','Beaucarnea recurvata','["pet_safe_reviewed", "sunny_window"]'::jsonb),
 (722,'Петуния ампельная','Petunia × atkinsiana (ампельные гибриды)','["ornamental_bloom", "pet_safe_reviewed", "trailing"]'::jsonb),
 (738,'Спатифиллум','Spathiphyllum','["ornamental_bloom", "partial_shade"]'::jsonb),
 (752,'Фикус лирата','Ficus lyrata','["tree_form"]'::jsonb),
 (753,'Аглаонема Сильвер Бей','Aglaonema ''Silver Bay''','["partial_shade", "patterned_foliage"]'::jsonb),
 (755,'Фикус Бенджамина Экзотика','Ficus benjamina ''Exotica''','["tree_form"]'::jsonb),
 (761,'Монстера Альба','Monstera deliciosa (Albo-Variegata)','["patterned_foliage", "support_climber"]'::jsonb),
 (767,'Бонсай Дуб пробковый','Quercus suber','["bonsai"]'::jsonb),
 (803,'Кодиеум пёстрый (кротон)','Codiaeum variegatum','["patterned_foliage"]'::jsonb),
 (805,'Аспидистра высокая Вариегата','Aspidistra elatior ''Variegata''','["partial_shade", "patterned_foliage", "pet_safe_reviewed"]'::jsonb),
 (851,'Антуриум Блэк Бьюти','Anthurium ''Black Beauty''','["ornamental_bloom"]'::jsonb),
 (852,'Антуриум Блэк Лав','Anthurium (Andraeanum Group) ''Rijn200643'' (Black Love)','["ornamental_bloom"]'::jsonb),
 (854,'Адениум — пустынная роза','Adenium obesum','["ornamental_bloom", "sunny_window"]'::jsonb),
 (855,'Антуриум Мелодия Ибис','Anthurium ''AN2143048'' (Melodia Ibis)','["ornamental_bloom"]'::jsonb),
 (856,'Диффенбахия Рефлектор','Dieffenbachia ''Reflector''','["patterned_foliage"]'::jsonb),
 (857,'Замиокулькас Раундлиф','Zamioculcas zamiifolia (Roundleaf)','["partial_shade"]'::jsonb),
 (858,'Лес из метасеквойи с кашпо','Metasequoia glyptostroboides','["bonsai"]'::jsonb),
 (859,'Нефролепис Даффи','Nephrolepis cordifolia ''Duffii''','["pet_safe_reviewed"]'::jsonb),
 (860,'Нефролепис Тедди Джуниор','Nephrolepis exaltata ''Teddy Junior''','["pet_safe_reviewed"]'::jsonb),
 (861,'Хлорофитум кудрявый','Chlorophytum comosum, кудрявые формы','["partial_shade", "patterned_foliage", "pet_safe_reviewed", "trailing"]'::jsonb),
 (863,'Гиппеаструм (амариллис)','Hippeastrum hybrids','["ornamental_bloom"]'::jsonb),
 (864,'Бонсай Фисташка китайская','Pistacia chinensis','["bonsai"]'::jsonb),
 (865,'Кордилина Киви','Cordyline fruticosa ''Kiwi''','["patterned_foliage"]'::jsonb),
 (866,'Лиметта Пурша на штамбе','Citrus limetta (Pursha)','["fruiting", "sunny_window", "tree_form"]'::jsonb),
 (867,'Традесканция зебрина','Tradescantia zebrina','["trailing"]'::jsonb);

INSERT INTO product_attribute_values AS stored(product_id,attribute_id,value,source) SELECT product.id,definition.id,review.features,'local' FROM reviewed_storefront_traits review JOIN products product ON product.product_code=review.product_code AND product.name=review.expected_name AND product.latin_name=review.expected_latin AND product.catalog_section='plants' AND product.status='published' CROSS JOIN attribute_definitions definition WHERE definition.code='storefront_traits' ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=(SELECT jsonb_agg(DISTINCT tag) FROM jsonb_array_elements((CASE WHEN jsonb_typeof(stored.value)='array' THEN stored.value ELSE '[]'::jsonb END)||EXCLUDED.value) tag),source='local',updated_at=CURRENT_TIMESTAMP;

INSERT INTO collections(slug,title,note,cover_url,cover_position,sort_order,is_active,show_on_home,mode,rules) VALUES
 ('easy','Неприхотливые','Простой уход в обычной квартире.','/assets/redesign/collection-easy-4k.webp','center',10,1,TRUE,'dynamic','[{"attribute": "care_level", "operator": "eq", "value": "easy"}]'::jsonb),
 ('dark','Переносят полутень','Переносят умеренное затенение при наличии дневного света. Для места без окна нужна подсветка; рост и цветение в полутени могут замедляться.','/assets/collections/2026-10-19/partial-shade.webp','center',20,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "partial_shade"}]'::jsonb),
 ('rare','Редкий полив','Переносят просыхание грунта. Проверяйте земляной ком: единого расписания полива для всех растений нет.','/assets/redesign/filters/rare-water-wall-v2.webp','center',30,1,TRUE,'dynamic','[{"attribute": "watering", "operator": "eq", "value": "rare"}]'::jsonb),
 ('pets','Безопасно для питомцев','Проверенные нетоксичные для кошек и собак растения. Листья и грунт всё равно не предназначены для еды.','/assets/collections/2026-10-19/pet-safe.webp','center',40,1,TRUE,'dynamic','[{"attribute": "pet_safety", "operator": "eq", "value": "safe"}, {"attribute": "storefront_traits", "operator": "contains", "value": "pet_safe_reviewed"}]'::jsonb),
 ('flowering','Цветущие растения','Виды и сорта с декоративным цветением. Наличие цветков или бутонов у отправляемого экземпляра уточняйте перед заказом.','/assets/collections/2026-10-19/flowering.webp','center top',50,1,TRUE,'dynamic','[{"attribute": "flowering", "operator": "eq", "value": true}, {"attribute": "storefront_traits", "operator": "contains", "value": "ornamental_bloom"}]'::jsonb),
 ('sunny-window','Для солнечного окна','Для светлого места у окна. К прямым лучам приучайте постепенно; сезонные ограничения смотрите в уходе за выбранным растением.','/assets/collections/2026-10-19/sunny-window.webp','center top',60,1,TRUE,'dynamic','[{"attribute": "light_level", "operator": "eq", "value": "sunny"}, {"attribute": "storefront_traits", "operator": "contains", "value": "sunny_window"}]'::jsonb),
 ('trailing','Ампельные — для полок и подвесов','Растения со свисающими побегами и розетками. Полке или подвесу нужен подходящий дневной свет.','/assets/collections/2026-10-19/trailing.webp','center',70,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "trailing"}]'::jsonb),
 ('bathroom','Для ванной комнаты','Для ванной с окном и достаточным рассеянным светом. В ванной без окна нужна постоянная подходящая подсветка; одной влажности недостаточно.','/assets/redesign/filters/bathroom-wall-v2.webp','center',80,1,TRUE,'dynamic','[{"attribute": "placement", "operator": "contains", "value": "bathroom"}, {"attribute": "humidity", "operator": "eq", "value": "high"}, {"attribute": "light_level", "operator": "eq", "value": "diffused"}]'::jsonb),
 ('tall','Крупные — от 80 см','Есть вариант с указанной высотой от 80 см. Выбирайте соответствующий размер в карточке; размеры взрослого растения здесь не используются.','/assets/redesign/filters/tall-wall-v2.webp','center',90,1,TRUE,'dynamic','[{"attribute": "height_cm", "operator": "gte", "value": 80}]'::jsonb),
 ('compact','Небольшие — до 40 см','Есть вариант с указанной высотой до 40 см. Выбирайте соответствующий размер в карточке; другие варианты могут быть выше.','/assets/redesign/filters/compact-wall-v2.webp','center',100,1,TRUE,'dynamic','[{"attribute": "height_cm", "operator": "gte", "value": 1}, {"attribute": "height_cm", "operator": "lte", "value": 40}]'::jsonb),
 ('indoor-bonsai','Бонсай без зимовки','Без обязательной холодной зимовки: подходят для тёплого содержания при достаточном свете. Зимой всё равно нужны корректировка полива и уход.','/assets/collections/2026-10-19/indoor-bonsai.webp','center top',110,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "bonsai"}, {"attribute": "storefront_traits", "operator": "contains", "value": "warm_bonsai"}]'::jsonb),
 ('desk','На рабочий стол','Небольшие формы для рабочего места с дневным светом. Выбирайте компактный размер и учитывайте место для будущего роста.','/assets/collections/2026-10-19/desk.webp','center',120,1,TRUE,'manual','[]'::jsonb),
 ('gifts','В подарок','Растения для первого знакомства и подарка увлечённому цветоводу. Сверьте условия ухода; кашпо входит в комплект только там, где это указано в карточке.','/assets/collections/2026-10-19/gifts.webp','center top',130,1,TRUE,'manual','[]'::jsonb),
 ('unusual','Необычные растения','Выразительная форма, фактура и необычный рост. Подборка для тех, кому интересно наблюдать за растениями.','/assets/collections/2026-10-19/unusual.webp','center top',140,1,TRUE,'manual','[]'::jsonb),
 ('variegated','Пёстрые листья','Пестролистные и узорчатые формы. Для сохранения рисунка большинству нужен хороший рассеянный свет.','/assets/collections/2026-10-19/variegated.webp','center top',150,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "patterned_foliage"}]'::jsonb),
 ('vertical','Зелёная вертикаль','Вьющиеся растения для выращивания вверх по опоре. Наличие и вид опоры проверяйте в комплектации выбранного товара.','/assets/collections/2026-10-19/vertical.webp','center top',160,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "support_climber"}]'::jsonb),
 ('home-trees','Деревце для дома','Фикусы и другие древесные формы для интерьера. Размер отправляемого экземпляра смотрите в выбранном варианте.','/assets/collections/2026-10-19/home-trees.webp','center top',170,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "tree_form"}]'::jsonb),
 ('fruiting','Цитрусы и плодовые','Виды, способные плодоносить при подходящем свете, возрасте и уходе. Наличие плодов сейчас не гарантируется; уточняйте состояние экземпляра.','/assets/collections/2026-10-19/fruiting.webp','center top',180,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "fruiting"}]'::jsonb),
 ('bonsai','Бонсай','Все бонсай, включая виды с холодным периодом покоя. Условия зимнего содержания проверяйте в паспорте растения.','/assets/collections/2026-10-19/bonsai.webp','center 30%',190,1,TRUE,'dynamic','[{"attribute": "storefront_traits", "operator": "contains", "value": "bonsai"}]'::jsonb)
ON CONFLICT(slug) DO UPDATE SET title=EXCLUDED.title,note=EXCLUDED.note,cover_url=EXCLUDED.cover_url,cover_position=EXCLUDED.cover_position,sort_order=EXCLUDED.sort_order,is_active=1,show_on_home=TRUE,mode=EXCLUDED.mode,rules=EXCLUDED.rules,updated_at=CURRENT_TIMESTAMP;

-- Historical links stay active and keep their original membership, outside the home rail.
UPDATE collections SET show_on_home=FALSE,sort_order=CASE slug WHEN 'office' THEN 200 ELSE 210 END WHERE slug IN ('office','bedroom');

UPDATE collections SET show_on_home=FALSE WHERE slug IN ('shade','pet-safe');

CREATE TEMP TABLE reviewed_editorial_members(slug TEXT,product_code BIGINT,expected_name TEXT,sort_order INTEGER) ON COMMIT DROP;

INSERT INTO reviewed_editorial_members VALUES
 ('desk',208,'Фикус микрокарпа Гинсенг',0),
 ('desk',110,'Замиокулькас Зензи',1),
 ('desk',97,'Диффенбахия Компакта',2),
 ('desk',166,'Пеперомия, микс видов и сортов',3),
 ('desk',192,'Фиалка узамбарская, микс',4),
 ('desk',228,'Фиттония, микс сортов',5),
 ('desk',220,'Филодендрон Грин Принцесс',6),
 ('desk',861,'Хлорофитум кудрявый',7),
 ('desk',233,'Хлорофитум хохлатый, полосатый',8),
 ('desk',198,'Фикус Бенджамина Грин Кинки',9),
 ('desk',210,'Фикус Бенджамина Наташа',10),
 ('desk',3,'Аглаонема Даймонд Бей',11),
 ('desk',184,'Спатифиллум Шопен',12),
 ('gifts',162,'Фаленопсис Элегант, каскад, 2 цветоноса',0),
 ('gifts',163,'Фаленопсис Элегант, каскад, микс',1),
 ('gifts',161,'Фаленопсис, 3 цветоноса',2),
 ('gifts',184,'Спатифиллум Шопен',3),
 ('gifts',183,'Спатифиллум Чико',4),
 ('gifts',3,'Аглаонема Даймонд Бей',5),
 ('gifts',5,'Аглаонема Мария',6),
 ('gifts',208,'Фикус микрокарпа Гинсенг',7),
 ('gifts',106,'Замиокулькас',8),
 ('gifts',110,'Замиокулькас Зензи',9),
 ('gifts',166,'Пеперомия, микс видов и сортов',10),
 ('gifts',233,'Хлорофитум хохлатый, полосатый',11),
 ('gifts',229,'Хамедорея изящная — Элеганс',12),
 ('gifts',119,'Калатея, микс сортов',13),
 ('gifts',90,'Декабрист (шлюмбергера)',14),
 ('gifts',610,'Шлюмбергера — декабрист',15),
 ('unusual',87,'Венерина мухоловка',0),
 ('unusual',854,'Адениум — пустынная роза',1),
 ('unusual',154,'Непентес, микс',2),
 ('unusual',167,'Платицериум',3),
 ('unusual',140,'Микросорум Крокодилус',4),
 ('unusual',27,'Алоказия Ред Секрет',5),
 ('unusual',31,'Алоказия Драгон Скейл',6),
 ('unusual',20,'Алоказия Блэк Вельвет',7),
 ('unusual',30,'Алоказия Тайни Дансерс',8),
 ('unusual',698,'Бокарнея (нолина)',9),
 ('unusual',48,'Бокарнея (нолина)',10),
 ('unusual',170,'Рипсалис',11),
 ('unusual',177,'Сансевиерия Дигиторум',12),
 ('unusual',178,'Сансевиерия Спагетти',13),
 ('unusual',188,'Тилландсия атмосферная, свисающая форма',14),
 ('unusual',189,'Тилландсия атмосферная, микс',15),
 ('unusual',125,'Крестовник Роули',16),
 ('unusual',520,'Замиокулькас Хамелеон',17),
 ('unusual',857,'Замиокулькас Раундлиф',18),
 ('unusual',22,'Алоказия Джеклин',19);

INSERT INTO collection_products(collection_id,product_id,sort_order) SELECT collection.id,product.id,review.sort_order FROM reviewed_editorial_members review JOIN collections collection ON collection.slug=review.slug JOIN products product ON product.product_code=review.product_code AND product.name=review.expected_name AND product.catalog_section='plants' AND product.status='published' ON CONFLICT(collection_id,product_id) DO NOTHING;

-- Match the exact visible sale variant, not different siblings for each condition.
CREATE OR REPLACE FUNCTION collection_rules_match_variant(target_product BIGINT, target_variant BIGINT, rule_set JSONB)
RETURNS BOOLEAN LANGUAGE plpgsql STABLE AS $$
DECLARE
  rule JSONB;
  definition_id BIGINT;
  definition_scope TEXT;
  matched BOOLEAN;
BEGIN
  IF rule_set IS NULL OR jsonb_typeof(rule_set)<>'array' OR jsonb_array_length(rule_set)=0 THEN RETURN FALSE; END IF;
  IF NOT EXISTS(SELECT 1 FROM product_variants variant WHERE variant.id=target_variant
    AND variant.product_id=target_product AND variant.is_active=1 AND variant.archived_at IS NULL
    AND NOT EXISTS(SELECT 1 FROM launch_hidden_extra_plant_variants hidden WHERE hidden.variant_id=variant.id))
  THEN RETURN FALSE; END IF;
  FOR rule IN SELECT element FROM jsonb_array_elements(rule_set) element LOOP
    definition_id := NULL;
    definition_scope := NULL;
    SELECT id,value_scope INTO definition_id,definition_scope FROM attribute_definitions
    WHERE code=BTRIM(rule->>'attribute') AND is_active LIMIT 1;
    IF definition_id IS NULL THEN RETURN FALSE; END IF;
    IF definition_scope='product' THEN
      SELECT COALESCE(BOOL_OR(collection_rule_value_matches(value,rule)),FALSE) INTO matched
      FROM product_attribute_values WHERE product_id=target_product AND attribute_id=definition_id;
    ELSE
      SELECT COALESCE(BOOL_OR(collection_rule_value_matches(value,rule)),FALSE) INTO matched
      FROM variant_attribute_values WHERE variant_id=target_variant AND attribute_id=definition_id;
    END IF;
    IF NOT matched THEN RETURN FALSE; END IF;
  END LOOP;
  RETURN TRUE;
END;
$$;

CREATE OR REPLACE FUNCTION collection_rules_match_product(target_product BIGINT,rule_set JSONB)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
  SELECT EXISTS(SELECT 1 FROM product_variants variant WHERE variant.product_id=target_product
    AND variant.is_active=1 AND variant.archived_at IS NULL
    AND NOT EXISTS(SELECT 1 FROM launch_hidden_extra_plant_variants hidden WHERE hidden.variant_id=variant.id)
    AND collection_rules_match_variant(target_product,variant.id,rule_set));
$$;

