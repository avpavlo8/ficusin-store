#!/usr/bin/env python3
"""Generate the third exact-name, blank-only catalogue content migration."""
from __future__ import annotations
import json
from pathlib import Path
from generate_curated_plant_content import PROFILES as FIRST, q
from generate_curated_plant_content_second import PROFILES as SECOND

ROOT = Path(__file__).resolve().parents[1]
SNAPSHOT = ROOT / 'docs/catalog-curated-content-third-snapshot-2026-10-12.json'
MIGRATION = ROOT / 'timeweb/migrations/20261012_zzz_curated_plant_content_third.sql'

# Primary horticultural source, conservatively identified Latin name, short,
# description, care, light enum, watering enum. Existing manager copy wins.
PROFILES = {
    'asplenium': (
        'https://www.rhs.org.uk/plants/epiphytic-ferns', 'Asplenium',
        'Розеточный папоротник с длинными зелёными вайями.',
        'Асплениум образует плотную розетку из цельных или волнистых вай. У формы «Криспи Вейв» край листа выраженно волнистый; точный вид «Парвати» по одному торговому названию не устанавливаем.',
        'Поставьте в яркий рассеянный свет, вдали от прямого солнца. Поддерживайте субстрат умеренно влажным, поливая по краю горшка и не заливая центр розетки. Не держите у батареи.',
        'diffused', 'moderate'),
    'dracaena_sanderiana': (
        'https://www.rhs.org.uk/plants/6097/dracaena-sanderiana-%28v%29/details', 'Dracaena sanderiana',
        'Драцена Сандера с тонкими побегами, известная как «лаки бамбук».',
        'Несмотря на торговое имя, лаки бамбук относится к драценам, а не к настоящему бамбуку. Прямые и изогнутые побеги — формы выращивания одного вида Dracaena sanderiana.',
        'Держите в ярком рассеянном свете без прямого полуденного солнца. При выращивании в грунте поливайте после лёгкого подсыхания поверхности; вода должна свободно уходить из горшка. Берегите от холода.',
        'diffused', 'moderate'),
    'laurel': (
        'https://www.rhs.org.uk/plants/9876/laurus-nobilis/details', 'Laurus nobilis',
        'Лавр благородный — вечнозелёное растение с ароматными листьями.',
        'Лавр благородный выращивают в контейнере как куст или небольшое дерево. Плотные кожистые листья сохраняются круглый год, а форму кроны можно поддерживать обрезкой.',
        'Поставьте в солнечное место или лёгкую полутень. Поливайте после подсыхания верхнего слоя в хорошо дренированном грунте; не оставляйте корни в воде. Зимой защищайте контейнер от сильного промерзания.',
        'sunny', 'moderate'),
    'monstera_adansonii': (
        'https://www.rhs.org.uk/plants/11191/monstera-adansonii/details', 'Monstera adansonii',
        'Монстера Monkey Leaf с ажурными отверстиями в листьях.',
        'Monstera adansonii — тропическая лиана с сердцевидными листьями и овальными отверстиями между жилками. Побеги можно направить по опоре или оставить свисать.',
        'Выберите яркий непрямой свет и тепло. В период роста поливайте после лёгкого подсыхания верхнего слоя, не оставляя воду в поддоне; зимой полив сокращайте.',
        'diffused', 'moderate'),
    'monstera_thai': (
        'https://www.rhs.org.uk/plants/520520/monstera-deliciosa-thai-constellation-v/details', "Monstera deliciosa 'Thai Constellation'",
        'Пестролистная монстера Thai Constellation с кремовыми пятнами на листьях.',
        'Monstera deliciosa Thai Constellation — вьющаяся пестролистная форма. По мере взросления листья становятся крупнее и образуют характерные разрезы и отверстия; для роста побегам нужна опора.',
        'Держите в ярком непрямом свете: белые участки листа легче обгорают под прямым солнцем. Поливайте после подсыхания верхнего слоя грунта и сливайте воду из поддона; обеспечьте рыхлый субстрат.',
        'diffused', 'moderate'),
    'pachira': (
        'https://www.rhs.org.uk/plants/12078/pachira-aquatica/details', 'Pachira aquatica',
        'Пахира акватика с пальчатыми листьями и плетёным стволом.',
        'Пахира акватика — тропическое дерево, которое в комнатной культуре часто выращивают из нескольких переплетённых стволиков. Её крупные листья состоят из нескольких листочков.',
        'Поставьте в яркий рассеянный свет. Поливайте после подсыхания верхнего слоя грунта и сливайте воду из поддона; зимой уменьшайте полив. Не допускайте холодных сквозняков.',
        'diffused', 'moderate'),
    'syngonium': (
        'https://plants.ces.ncsu.edu/plants/syngonium-podophyllum/common-name/arrow-head-vine/', 'Syngonium',
        'Сингониум с листьями стреловидной формы и гибкими побегами.',
        'Сингониум — тропическое растение семейства ароидных. Молодые листья обычно стреловидные; с возрастом растение может образовывать более длинные побеги, которым дают опору или позволяют свисать.',
        'Держите в ярком рассеянном свете без жёсткого солнца. Поливайте после подсыхания поверхности рыхлого субстрата, затем сливайте лишнюю воду; берегите от холода.',
        'diffused', 'moderate'),
    'strelitzia': (
        'https://www.rhs.org.uk/plants/strelitzia/how-to-grow-strelitzia', 'Strelitzia nicolai',
        'Стрелиция Николая с крупными листьями для просторного светлого места.',
        'Strelitzia nicolai образует высокие черешки и крупные похожие на банановые листья. В зрелом возрасте становится значительно больше компактных комнатных растений, поэтому ей нужно место для роста.',
        'Поставьте в очень светлое место и постепенно приучайте к прямому солнцу. В период роста поливайте регулярно после подсыхания верхнего слоя, зимой реже; не допускайте застоя воды.',
        'sunny', 'moderate'),
    'ficus_benjamina': FIRST['ficus_benjamina'],
    'ficus_elastica': FIRST['ficus_elastica'],
    'ficus_schrijveriana': (
        'https://www.rhs.org.uk/plants/77243/ficus-elastica-schrijveriana-v/details', "Ficus elastica 'Schrijveriana'",
        'Фикус Шривериана с крупными мраморными листьями.',
        'Ficus elastica Schrijveriana — пестролистная форма каучуконосного фикуса. На плотных листьях сочетаются зелёные, серые и кремовые участки; выраженность рисунка зависит от освещения.',
        'Поставьте в яркий рассеянный свет, защищая листья от резкого полуденного солнца. Поливайте после подсыхания верхнего слоя грунта, сливайте воду из поддона; берегите от холодных сквозняков.',
        'diffused', 'moderate'),
    'ficus_lyrata': SECOND['ficus_lyrata'],
    'fittonia': (
        'https://plants.ces.ncsu.edu/plants/fittonia-albivenis/common-name/nerve-plant/', 'Fittonia',
        'Невысокая фиттония с выразительной сеткой жилок на листьях.',
        'Фиттония — компактное тропическое растение, которое выращивают ради контрастных жилок на небольших листьях. Ей подходят тёплые места с влажным воздухом без палящего солнца.',
        'Дайте рассеянный свет или лёгкую полутень. Сохраняйте грунт слегка влажным, но не мокрым: при пересушке листья быстро теряют упругость. Не ставьте у отопления.',
        'diffused', 'frequent'),
    'chamaedorea': (
        'https://www.rhs.org.uk/plants/chamaedorea/growing-guide', 'Chamaedorea elegans',
        'Хамедорея элеганс — компактная пальма с тонкими перистыми листьями.',
        'Хамедорея элеганс медленно растёт и образует изящные дугообразные вайи. Эта пальма подходит для выращивания в комнате без прямого обжигающего солнца.',
        'Поставьте в рассеянный свет или лёгкую полутень. Поливайте после подсыхания поверхности грунта и не оставляйте воду в поддоне. Берегите от горячего сухого воздуха.',
        'diffused', 'moderate'),
    'hedera': (
        'https://www.rhs.org.uk/plants/ivy/ivy-as-a-houseplant', 'Hedera',
        'Комнатный плющ с длинными побегами для подвесного кашпо или опоры.',
        'Хедера — вечнозелёная лиана с побегами, которые могут свисать или оплетать опору. Форма и рисунок листьев зависят от сорта; точный вид карточки не установлен.',
        'Выберите непрямой свет; зелёные формы переносят и более тенистое место. Поливайте после подсыхания поверхности грунта, не допуская застоя воды. Держите подальше от горячей батареи.',
        'diffused', 'moderate'),
    'hoya': (
        'https://www.rhs.org.uk/plants/hoya/how-to-grow', 'Hoya',
        'Хойя — вьющийся комнатный цветок с плотными листьями.',
        'Хойи образуют длинные побеги и со временем могут цвести гроздьями восковых цветков. Вид и рисунок листьев в этой карточке не подтверждены, поэтому особенности сорта не обещаем.',
        'Дайте яркий непрямой свет и тепло. Поливайте умеренно после подсыхания части субстрата; избыток воды сливайте. Для длинных побегов предусмотрите опору или подвесное размещение.',
        'diffused', 'moderate'),
    'schefflera': (
        'https://www.rhs.org.uk/plants/70437/schefflera-arboricola/details', 'Schefflera',
        'Шеффлера с лучистыми пальчатыми листьями и стройной кроной.',
        'Шеффлера — вечнозелёное декоративнолистное растение: несколько листочков расходятся от общего черешка как спицы зонтика. По торговым названиям «Герда» и «Луизиана» ботанический вид не уточняем.',
        'Поставьте в яркий рассеянный свет, защищая от палящего солнца. Поливайте после подсыхания верхнего слоя и сливайте лишнюю воду; зимой поливайте осторожнее.',
        'diffused', 'moderate'),
    'epipremnum': (
        'https://www.rhs.org.uk/plants/epipremnum/growing-guide', 'Epipremnum',
        'Эпипремнум — гибкая лиана для опоры или подвесного кашпо.',
        'Эпипремнум образует длинные побеги с глянцевыми сердцевидными листьями. Его можно выращивать свисающим с полки или направлять вверх по опоре; точный сорт этой карточки не установлен.',
        'Дайте яркий рассеянный свет, избегая сильного прямого солнца. Поливайте, когда верхние сантиметры субстрата подсохнут, и не держите корни в воде.',
        'diffused', 'moderate'),
    'coffee': (
        'https://www.rhs.org.uk/plants/4159/coffea-arabica/details', 'Coffea arabica',
        'Кофе арабика с глянцевыми тёмно-зелёными листьями.',
        'Coffea arabica — вечнозелёный кустарник или небольшое дерево. При хорошем уходе взрослое растение может цвести белыми ароматными цветками, но цветение молодого экземпляра не гарантировано.',
        'Поставьте в светлое место без палящего солнца. Держите почву умеренно влажной, поливая после начала подсыхания поверхности; сливайте лишнюю воду и избегайте холодных сквозняков.',
        'diffused', 'moderate'),
    'ananas': (
        'https://www.rhs.org.uk/plants/1106/ananas-comosus-f/details', 'Ananas comosus',
        'Ананас — розеточное растение с жёсткими листьями и декоративным плодом.',
        'Ананас относится к наземным бромелиевым. Плотная розетка длинных листьев окружает цветонос; при подходящих условиях после цветения формируется плод.',
        'Держите в очень светлом тёплом месте. Во время активного роста поливайте после подсыхания верхнего слоя хорошо дренированного субстрата; в прохладный сезон сократите полив.',
        'sunny', 'moderate'),
    'cycas': (
        'https://www.rhs.org.uk/plants/5127/cycas-revoluta/details', 'Cycas',
        'Цикас с жёсткими перистыми листьями, похожий на небольшую пальму.',
        'Цикас — медленно растущее саговниковое растение, не настоящая пальма. Листья располагаются розеткой вокруг короткого ствола; растение ядовито при поедании. Вид по названию не уточняем.',
        'Поставьте в яркий свет с защитой от самого жаркого полуденного солнца. Поливайте умеренно во время роста, после подсыхания верхнего слоя; зимой значительно реже.',
        'sunny', 'moderate'),
    'portulacaria': (
        'https://www.rhs.org.uk/plants/13534/portulacaria-afra/details', 'Portulacaria afra',
        'Бонсай из портулакарии с мелкими мясистыми листьями.',
        'Portulacaria afra — суккулентный кустарник с красноватыми молодыми побегами и округлыми листьями. В форме бонсай крону поддерживают аккуратной обрезкой.',
        'Поставьте в максимально светлое место. Поливайте только после просыхания субстрата, обязательно используйте горшок с дренажным отверстием. Зимой при замедлении роста поливайте редко.',
        'sunny', 'rare'),
    'musa': (
        'https://www.rhs.org.uk/plants/banana/growing-guide', 'Musa',
        'Банановое растение с крупными тропическими листьями.',
        'Муса — травянистое растение: видимый «ствол» образован основаниями листьев. Плодоношение комнатного экземпляра не обещаем; его ценят прежде всего за крупную листву.',
        'Поставьте в тёплое очень светлое место. В период активного роста поливайте регулярно, поддерживая грунт влажным без застоя воды; зимой сократите полив и берегите от холода.',
        'sunny', 'frequent'),
    'calla': (
        'https://www.rhs.org.uk/plants/zantedeschia/growing-guide', 'Zantedeschia',
        'Калла с крупными листьями и выразительным покрывалом соцветия.',
        'Каллы относятся к роду Zantedeschia. Настоящие мелкие цветки собраны в початок внутри окрашенного покрывала; точный вид и окраска этого экземпляра в названии не указаны.',
        'Во время роста поставьте в светлое место без палящего солнца. Держите грунт умеренно влажным. После завершения сезона роста полив уменьшайте; период покоя для комнатной каллы важен.',
        'diffused', 'frequent'),
    'crassula': (
        'https://www.rhs.org.uk/plants/4739/crassula-ovata/details', 'Crassula',
        'Крассула — суккулент с мясистыми листьями и ветвящимися побегами.',
        'Крассулы запасают влагу в листьях и лучше переносят краткую сухость, чем постоянно мокрый грунт. Точный вид в названии этой карточки не указан.',
        'Поставьте в очень светлое место. Посадите в хорошо дренированный субстрат и поливайте после полного просыхания значительной части грунта; зимой ещё реже.',
        'sunny', 'rare'),
    'curio': (
        'https://www.rhs.org.uk/plants/353584/curio-rowleyanus/details/', 'Curio rowleyanus',
        'Крестовник Роули с тонкими свисающими побегами и листьями-шариками.',
        'Растение, известное как Senecio rowleyanus, сейчас относят к роду Curio. Шаровидные мясистые листья нанизаны на длинные побеги, поэтому ему подходит подвесное размещение.',
        'Дайте яркое место с несколькими часами мягкого солнца. Поливайте только после просыхания рыхлого субстрата; зимой поливайте очень редко и не держите корни в воде.',
        'sunny', 'rare'),
    'mimosa': (
        'https://www.rhs.org.uk/plants/21000/mimosa-pudica/details', 'Mimosa pudica',
        'Мимоза стыдливая складывает перистые листья при прикосновении.',
        'Mimosa pudica — недолговечное тропическое растение с тонко рассечёнными листьями. Листочки реагируют на прикосновение, а летом могут появляться небольшие розовые соцветия.',
        'Поставьте в очень светлое тёплое место. Поливайте после начала подсыхания поверхности субстрата, не допуская застоя воды; защищайте от холода.',
        'sunny', 'moderate'),
    'myrtle': (
        'https://www.rhs.org.uk/plants/11311/myrtus-communis/details', 'Myrtus communis',
        'Мирт обыкновенный с мелкими ароматными вечнозелёными листьями.',
        'Myrtus communis формирует плотный вечнозелёный кустарник. Штамбовая и спиральная формы получены формировкой кроны, а не другим видом растения.',
        'Выберите солнечное место с хорошим проветриванием. Поливайте после лёгкого подсыхания верхнего слоя, не оставляя воду в поддоне; зимой держите светло и прохладнее, поливая реже.',
        'sunny', 'moderate'),
    'metasequoia': (
        'https://www.rhs.org.uk/plants/11093/metasequoia-glyptostroboides/details', 'Metasequoia glyptostroboides',
        'Бонсай из метасеквойи — листопадного хвойного дерева.',
        'Метасеквойя глиптостробовидная отличается мягкой зелёной хвоей, которая осенью буреет и опадает. Форма «лес» объединяет несколько растений того же вида; зимний листопад естественен.',
        'Поставьте на открытом воздухе в светлое место, защищая маленький горшок от сильного промерзания. В сезон роста поддерживайте субстрат равномерно влажным; зимой поливайте по состоянию, не оставляя корни сухими надолго.',
        'sunny', 'frequent'),
    'hibiscus': (
        'https://www.rhs.org.uk/plants/hibiscus/growing-guide', 'Hibiscus',
        'Гибискус с крупными цветками и глянцевой листвой.',
        'Комнатные гибискусы выращивают ради ярких крупных цветков. Вид и окраска конкретного экземпляра в названии не указаны, поэтому сортовые признаки не заявляем.',
        'Поставьте в очень светлое тёплое место. Во время активного роста поливайте после лёгкого подсыхания поверхности грунта и не допускайте длительной засухи; зимой полив сократите.',
        'sunny', 'moderate'),
    'davallia': (
        'https://www.rhs.org.uk/plants/epiphytic-ferns', 'Davallia',
        'Даваллия — папоротник с тонкими вайями и ползучими пушистыми корневищами.',
        'У даваллии корневища могут выходить на поверхность и свисать через край горшка. Это естественная особенность эпифитного папоротника; точный вид по карточке не устанавливаем.',
        'Поставьте в яркий рассеянный свет. Поддерживайте влажность воздуха. Поливайте так, чтобы субстрат оставался умеренно влажным, но вода не стояла у корней; не засыпайте выступающие корневища.',
        'diffused', 'frequent'),
    'platycerium': (
        'https://www.rhs.org.uk/plants/13234/platycerium-bifurcatum/details', 'Platycerium',
        'Платицериум — эпифитный папоротник с вайями, похожими на оленьи рога.',
        'Платицериум образует разные типы вай: округлые прикрывают основание, а более длинные ветвятся. Вид данного экземпляра в карточке не уточнён.',
        'Выберите яркий непрямой свет. Поддерживайте влажность воздуха. Поливайте умеренно, давая рыхлому субстрату слегка подсохнуть между поливами; не держите растение в стоячей воде.',
        'diffused', 'moderate'),
    'rhipsalis': (
        'https://www.rhs.org.uk/plants/types/cacti-succulents/houseplants/growing-guide', 'Rhipsalis',
        'Рипсалис — свисающий лесной кактус с тонкими ветвящимися побегами.',
        'Рипсалисы в природе растут как эпифиты. В отличие от пустынных кактусов им подходит рассеянный свет и несколько более регулярный полив; точный вид этой карточки не установлен.',
        'Поставьте в яркий рассеянный свет без жёсткого полуденного солнца. Поливайте после лёгкого подсыхания субстрата, в зимний период реже; обеспечьте свободный сток воды.',
        'diffused', 'moderate'),
    'maranta': (
        'https://www.rhs.org.uk/plants/119598/maranta-leuconeura/details', 'Maranta',
        'Маранта с узорчатыми листьями, которые меняют положение к вечеру.',
        'Маранта — невысокое тропическое растение с выразительным рисунком листьев. Листовые пластинки могут приподниматься к вечеру; вид конкретного экземпляра не указан.',
        'Поставьте в яркий рассеянный свет или лёгкую полутень. Держите субстрат слегка влажным, поливая после начала подсыхания поверхности; берегите от сухого горячего воздуха.',
        'diffused', 'frequent'),
    'sinningia': (
        'https://www.rhs.org.uk/plants/156475/sinningia-speciosa/details', 'Sinningia',
        'Глоксиния Соната — компактная синнингия с крупными цветками.',
        'Под торговым названием «глоксиния» обычно выращивают синнингию. Это клубневое растение с бархатистыми листьями и крупными колокольчатыми цветками; окраску серии Sonata без этикетки не уточняем.',
        'Во время роста поставьте в яркий рассеянный свет. Поливайте по краю горшка после лёгкого подсыхания поверхности, не заливая листья и клубень; после отмирания надземной части полив сократите.',
        'diffused', 'moderate'),
    'caladium': (
        'https://www.rhs.org.uk/plants/2601/caladium-bicolor-v/details', 'Caladium',
        'Каладиум с тонкими листьями стреловидной формы и ярким рисунком.',
        'Каладиумы растут из клубней и образуют декоративные листья. Точный сорт этой карточки не установлен; в конце сезона листья могут отмирать перед периодом покоя.',
        'Поставьте в яркий рассеянный свет и тепло. Во время роста поддерживайте грунт умеренно влажным без застоя воды; при переходе к покою постепенно сократите полив.',
        'diffused', 'moderate'),
    'clusia': (
        'https://www.rhs.org.uk/plants/29557/clusia-rosea/details', 'Clusia',
        'Клузия с плотными глянцевыми листьями и прочными побегами.',
        'Клузии — тропические вечнозелёные кустарники с кожистой листвой. У этой карточки не указан точный вид; комнатное растение ценят за компактную крону, а цветение не гарантируем.',
        'Поставьте в яркий фильтрованный свет без жаркого полуденного солнца. Летом поливайте после лёгкого подсыхания поверхности, зимой осторожнее; не оставляйте воду в поддоне.',
        'diffused', 'moderate'),
    'cordyline': (
        'https://www.rhs.org.uk/plants/cordyline/growing-guide', 'Cordyline',
        'Кордилина с длинными ремневидными листьями и стройным силуэтом.',
        'Кордилины выращивают ради пучков узких листьев: окраска и будущая высота зависят от вида и сорта. У данной карточки вид не указан, поэтому размер взрослого растения не обещаем.',
        'Выберите яркий рассеянный свет; пёстрые листья защищайте от жёсткого полуденного солнца. Поливайте после подсыхания верхнего слоя хорошо дренированного грунта и берегите от мороза.',
        'diffused', 'moderate'),
    'cyrtomium': (
        'https://www.rhs.org.uk/plants/5235/cyrtomium-falcatum/details', 'Cyrtomium',
        'Циртомиум — папоротник с плотными блестящими листочками на вайях.',
        'Циртомиумы образуют изящные перистые вайи с кожистыми сегментами. Вид этой карточки не уточнён; в комнате растение держат вдали от сухого горячего воздуха.',
        'Поставьте в рассеянный свет или лёгкую полутень. Поливайте после начала подсыхания поверхности, сохраняя субстрат умеренно влажным и не оставляя воду у корней.',
        'diffused', 'moderate'),
    'roses': (
        'https://www.rhs.org.uk/plants/roses/growing-in-containers', 'Rosa',
        'Компактная горшечная роза для светлого балкона или патио.',
        'Миниатюрные и патио-розы подходят для контейнеров, но нуждаются в солнечном месте и сезонном уходе. Цвет и точный сорт конкретной карточки не установлены.',
        'Поставьте на открытом воздухе в место с несколькими часами прямого солнца. Поливайте регулярно, когда верхний слой грунта подсыхает, не пересушивая весь ком; зимой защищайте контейнер от сильного промерзания.',
        'sunny', 'frequent'),
    'tradescantia': (
        'https://www.rhs.org.uk/plants/18299/tradescantia-spathacea/details', 'Tradescantia',
        'Традесканция с гибкими побегами и декоративной листвой.',
        'Традесканции — травянистые растения, которые выращивают ради формы и окраски листьев. Вид этой карточки не указан; побеги можно направить вдоль полки или выращивать в подвесном кашпо.',
        'Дайте яркий рассеянный свет для сохранения окраски. Поливайте после подсыхания верхнего слоя грунта и сливайте избыток воды; зимой частоту полива уменьшайте.',
        'diffused', 'moderate'),
    'zelkova_bonsai': (
        'https://www.rhs.org.uk/plants/types/trees/bonsai', 'Zelkova',
        'Листопадный бонсай из зельквы с мелкой зубчатой листвой.',
        'Зельквы — деревья с зубчатыми листьями и выразительной осенней окраской. Вид этого бонсай не указан; сезонный листопад для зельквы естественен.',
        'Держите на открытом воздухе в солнечном месте или лёгкой полутени. В сезон роста проверяйте маленький горшок ежедневно и поливайте после подсыхания поверхности; зимой защищайте корневой ком от сильного промерзания.',
        'sunny', 'frequent'),
    'carmona_bonsai': (
        'https://www.rhs.org.uk/plants/207663/carmona/details', 'Carmona',
        'Кармона — комнатный бонсай с мелкими глянцевыми листьями.',
        'Кармону выращивают как субтропический бонсай благодаря компактной листве и плотной кроне. При благоприятных условиях могут появляться мелкие белые цветки; цветение не гарантируем.',
        'Поставьте в очень светлое тёплое место без холодных сквозняков. Поливайте после лёгкого подсыхания поверхности дренированного грунта, не позволяя маленькому кому полностью пересохнуть.',
        'sunny', 'frequent'),
    'ligustrum_bonsai': (
        'https://www.rhs.org.uk/plants/81603/ligustrum-sinense/details', 'Ligustrum sinense',
        'Бонсай из китайской бирючины с мелкой зелёной листвой.',
        'Ligustrum sinense — китайская бирючина, которую можно формировать обрезкой. В зависимости от условий она бывает полувечнозелёной и способна частично сбрасывать листья.',
        'Поставьте на открытом воздухе на солнце или в лёгкую полутень. В сезон роста поливайте после подсыхания поверхности грунта, не пересушивая маленький горшок; зимой защищайте корни от сильного промерзания.',
        'sunny', 'frequent'),
    'radermachera_bonsai': (
        'https://www.rhs.org.uk/plants/106221/radermachera-sinica/details', 'Radermachera',
        'Комнатный бонсай из радермахеры с мелкими блестящими листочками.',
        'Радермахера — вечнозелёное дерево, которое в помещении ценят за густую перистую листву. Точный вид бонсай в названии не указан, поэтому оставляем определение на уровне рода.',
        'Поставьте в яркий непрямой свет и тепло. Поливайте после подсыхания верхнего слоя грунта, не допуская длительной сырости; избегайте резкой перестановки и холодных сквозняков.',
        'diffused', 'moderate'),
    'pomegranate_bonsai': (
        'https://www.rhs.org.uk/plants/14189/punica-granatum/details', 'Punica granatum',
        'Бонсай из граната с мелкой листвой и сезонным листопадом.',
        'Гранат — листопадный кустарник, который можно формировать как бонсай. При хорошем освещении взрослое растение способно цвести; наличие плодов у этого экземпляра не обещаем.',
        'Поставьте в максимально светлое место с доступом к солнцу. В сезон роста поливайте после подсыхания верхнего слоя, не пересушивая маленький горшок; зимой в прохладе поливайте реже.',
        'sunny', 'moderate'),
    'ilex_bonsai': (
        'https://www.rhs.org.uk/plants/9122/ilex-crenata/details', 'Ilex crenata',
        'Бонсай из падуба городчатого с мелкими блестящими листьями.',
        'Ilex crenata — вечнозелёный японский падуб, который хорошо поддаётся формировке. Ягоды могут появиться только у женского растения после опыления; плодоношение не гарантируем.',
        'Держите на открытом воздухе на солнце или в лёгкой полутени. Поливайте после лёгкого подсыхания поверхности и не допускайте ни пересушки маленького кома, ни застоя воды; зимой защищайте горшок от сильного промерзания.',
        'sunny', 'moderate'),
    'olive_bonsai': (
        'https://www.rhs.org.uk/plants/11735/olea-europaea-f/details', 'Olea europaea',
        'Бонсай из оливы с узкими серебристо-зелёными листьями.',
        'Олива — медленно растущее вечнозелёное дерево с плотными кожистыми листьями. В названии «Sylvestris» указана торговая форма; точный ботанический статус по этикетке не подтверждён.',
        'Поставьте на самое светлое место с прямым солнцем. Поливайте после заметного подсыхания хорошо дренированного грунта; зимой держите светло и прохладнее, защищая небольшой горшок от мороза.',
        'sunny', 'moderate'),
    'apple_bonsai': (
        'https://www.rhs.org.uk/plants/60558/malus-domestica-f/details', 'Malus domestica',
        'Бонсай из домашней яблони с сезонной листвой.',
        'Домашняя яблоня — листопадное плодовое дерево. В форме бонсай её крону и корни поддерживают обрезкой; цветение и завязь плодов зависят от возраста, условий и опыления.',
        'Держите на открытом воздухе в солнечном месте. В сезон роста поливайте регулярно после подсыхания поверхности грунта, не пересушивая маленький горшок; на зиму защищайте корни от сильного промерзания.',
        'sunny', 'frequent'),
    'myrsine_africana': (
        'https://www.rhs.org.uk/plants/11304/myrsine-africana/details', 'Myrsine africana',
        'Мирсина африканская — вечнозелёный куст с мелкими блестящими листьями.',
        'Myrsine africana медленно растёт и хорошо переносит формирующую обрезку. Плоды появляются только при наличии мужского и женского растений, поэтому плодоношение одного экземпляра не обещаем.',
        'Поставьте в светлое место с мягким солнцем или лёгкой полутенью. Поливайте после подсыхания поверхности грунта, не пересушивая весь ком; в холодный сезон защищайте контейнер от мороза.',
        'sunny', 'moderate'),
    'myrsine_standard': (
        'https://www.rhs.org.uk/plants/11304/myrsine-africana/details', 'Myrsine',
        'Штамбовая мирсина с мелкой плотной вечнозелёной листвой.',
        'Штамбовая форма получается обрезкой и не обозначает отдельный вид. Точный вид этого экземпляра не подтверждён, поэтому оставляем определение на уровне рода Myrsine.',
        'Поставьте в светлое место с мягким солнцем или лёгкой полутенью. Поливайте после подсыхания поверхности грунта, не пересушивая весь ком; зимой берегите небольшой горшок от мороза.',
        'sunny', 'moderate'),
    'thuja_brabant': (
        'https://www.rhs.org.uk/plants/163488/thuja-occidentalis-brabant/details', "Thuja occidentalis 'Brabant'",
        'Туя Брабант — уличное хвойное растение с плотной зелёной кроной.',
        'Thuja occidentalis Brabant — быстрорастущий вечнозелёный сорт с чешуйчатой хвоей. В грунте его часто используют для живых изгородей; в контейнере корни требуют большей защиты зимой.',
        'Высаживайте на открытом воздухе на солнце или в полутени. Поливайте после подсыхания поверхности грунта, не допуская длительной пересушки контейнера; обеспечьте дренаж и защитите горшок от сильного промерзания.',
        'sunny', 'moderate'),
    'ficus_altissima': (
        'https://www.rhs.org.uk/plants/158823/ficus-altissima/details', 'Ficus altissima',
        'Фикус альтиссима с крупными плотными глянцевыми листьями.',
        'Ficus altissima — тропическое вечнозелёное дерево, которое в комнате выращивают как крупное декоративнолистное растение. Окраска листьев зависит от формы и освещения.',
        'Поставьте в яркий рассеянный свет и стабильное тепло. Поливайте после подсыхания верхнего слоя грунта, сливая воду из поддона; берегите от холодного сквозняка.',
        'diffused', 'moderate'),
}

ASSIGNMENTS = {
    'asplenium': [45,43], 'dracaena_sanderiana': [46,47,104],
    'laurel': [130,131], 'monstera_adansonii': [148], 'monstera_thai': [152],
    'pachira': [165], 'syngonium': [179], 'strelitzia': [186],
    'ficus_benjamina': [204,210,213], 'ficus_elastica': [212],
    'ficus_schrijveriana': [214],
    'fittonia': [228], 'chamaedorea': [229], 'hedera': [230], 'hoya': [234],
    'schefflera': [241,242,243], 'epipremnum': [244], 'coffee': [123],
    'ananas': [32], 'cycas': [238], 'portulacaria': [77], 'musa': [153],
    'calla': [120], 'crassula': [124], 'curio': [125], 'mimosa': [142],
    'myrtle': [145,146,147], 'metasequoia': [67,68], 'hibiscus': [88],
    'davallia': [89], 'platycerium': [167], 'rhipsalis': [170],
    'maranta': [139], 'sinningia': [180], 'caladium': [116],
    'clusia': [121], 'cordyline': [122], 'cyrtomium': [240],
    'roses': [172,173], 'tradescantia': [190],
    'zelkova_bonsai': [58], 'carmona_bonsai': [60],
    'ligustrum_bonsai': [65], 'radermachera_bonsai': [78],
    'pomegranate_bonsai': [56], 'ilex_bonsai': [74],
    'olive_bonsai': [72], 'apple_bonsai': [86],
    'myrsine_africana': [143], 'myrsine_standard': [144],
    'thuja_brabant': [191], 'ficus_altissima': [194],
}

# Exact old value and product name are both required. These corrections are
# deliberately separate from the blank-only content batch.
BOTANICAL_CORRECTIONS = [
    (27, 'Алоказия Ред Секрет', 'Alocasia', "Alocasia cuprea 'Red Secret'", 'https://www.rhs.org.uk/plants/504110/alocasia-cuprea-red-secret/details'),
    (110, 'Замиокулькас «Зензи»', 'Zamioculcas', 'Zamioculcas zamiifolia', 'https://www.rhs.org.uk/plants/365733/zamioculcas-zamiifolia-zenzi-hansoti-13/details'),
    (112, 'Замиокулькас Равен (Raven)', 'Zamioculcas', "Zamioculcas zamiifolia 'Dowon'", 'https://www.rhs.org.uk/plants/364189/zamioculcas-zamiifolia-dowon/details'),
    (113, 'Замиокулькас Равен (Raven)', 'Zamioculcas', "Zamioculcas zamiifolia 'Dowon'", 'https://www.rhs.org.uk/plants/364189/zamioculcas-zamiifolia-dowon/details'),
    (198, 'Фикус Бенджамина Грин Кинки', 'Ficus', "Ficus benjamina 'Green Kinky'", 'https://www.rhs.org.uk/plants/317750/ficus-benjamina-green-kinky/details'),
    (205, 'Фикус Лирата Бамбино', 'Ficus', "Ficus lyrata 'Bambino'", 'https://www.rhs.org.uk/plants/147427/ficus-lyrata-bambino/details'),
    (91, 'Дипсис Арека', 'Dypsis', 'Chrysalidocarpus lutescens', 'https://powo.science.kew.org/taxon/urn:lsid:ipni.org:names:665962-1'),
    (92, 'Дипсис Арека', 'Dypsis', 'Chrysalidocarpus lutescens', 'https://powo.science.kew.org/taxon/urn:lsid:ipni.org:names:665962-1'),
    (156, 'Нефролепис Грин Леди', 'Nephrolepis', "Nephrolepis exaltata 'Green Lady'", 'https://www.rhs.org.uk/plants/304353/nephrolepis-exaltata-green-lady/details'),
]

def passport_advice(profile):
    sentences = [sentence.strip()+'.' for sentence in profile[4].split('.') if sentence.strip()]
    light = next((sentence for sentence in sentences if any(word in sentence.lower() for word in ('свет','солнц','солнеч','полутен'))), None)
    water = next((sentence for sentence in sentences
                  if any(word in sentence.lower() for word in ('полив','полива'))
                  or ('влажн' in sentence.lower() and any(word in sentence.lower() for word in ('грунт','субстрат','почв')))), None)
    if not light or not water:
        raise ValueError('Missing thematic lighting or watering advice: '+profile[1])
    return light,water

def main() -> None:
    names = {int(k): v for k,v in json.loads(SNAPSHOT.read_text()).items()}
    ids = [code for codes in ASSIGNMENTS.values() for code in codes]
    if len(ids) != len(set(ids)) or set(ids) != set(names):
        raise SystemExit('Third batch assignment/snapshot mismatch')
    rows = []
    for key,codes in ASSIGNMENTS.items():
        source,latin,short,description,care,light,watering = PROFILES[key]
        guide_light,guide_water = passport_advice(PROFILES[key])
        for code in codes:
            rows.append((code,names[code],latin,short,description,care,light,watering,guide_light,guide_water))
    lines = [
        '-- Third exact-name plant content batch. Only blank fields and verified source-backed cards.',
        'CREATE TEMP TABLE curated_plant_content_third (product_code bigint PRIMARY KEY, expected_name text NOT NULL, latin text NOT NULL, short_text text NOT NULL, full_text text NOT NULL, care_text text NOT NULL, light text NOT NULL, watering text NOT NULL, guide_light text NOT NULL, guide_water text NOT NULL) ON COMMIT DROP;',
        'INSERT INTO curated_plant_content_third(product_code,expected_name,latin,short_text,full_text,care_text,light,watering,guide_light,guide_water) VALUES',
        ',\n'.join('  ('+','.join([str(code)]+[q(v) for v in (name,latin,short,description,care,light,watering,guide_light,guide_water)])+')' for code,name,latin,short,description,care,light,watering,guide_light,guide_water in rows)+';',
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
        'FROM curated_plant_content_third AS c',
        "WHERE p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",
        " AND (BTRIM(COALESCE(p.latin_name,''))='' OR BTRIM(COALESCE(p.short_description,''))='' OR BTRIM(COALESCE(p.description,''))='' OR BTRIM(COALESCE(p.care_instructions,''))=''",
        "  OR BTRIM(COALESCE(p.light_level,''))='' OR BTRIM(COALESCE(p.watering,''))='' OR BTRIM(COALESCE(p.plant_passport->>'lighting',''))='' OR BTRIM(COALESCE(p.plant_passport->>'watering',''))='');",
        "-- Exact correction for our own genus-only value, supported by RHS and NC Extension.",
    ]
    for code, name, old, new, source in BOTANICAL_CORRECTIONS:
        lines.append(f'-- Botanical correction: {source}')
        lines.append(f'UPDATE products SET latin_name={q(new)},updated_at=CURRENT_TIMESTAMP WHERE product_code={code} AND catalog_section=\'plants\' AND name={q(name)} AND latin_name={q(old)};')
    MIGRATION.write_text('\n'.join(lines)+'\n')
    print(f'Generated {len(rows)} guarded cards')

if __name__ == '__main__':
    main()
