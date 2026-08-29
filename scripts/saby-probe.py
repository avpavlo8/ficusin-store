"""Разведка: какие идентификаторы номенклатуры отдаёт API СБИС.

Связка продаж маркетплейсов с товаром упирается в один вопрос — есть ли в
ответе `nomenclature/list` то, чем товар опознаётся на площадке. В карточке
СБИС видно, что у растения несколько кодов сразу: код товара `X941092466`,
два штрихкода EAN13 и штрихкод Ozon вида `OZN1851256804`, а ниже — «внешний
код» с артикулом площадки («араукария 40» для OZON и для Wildberries).
Документация метода перечисляет не все из них, поэтому спрашиваем саму
площадку.

Скрипт ничего не создаёт и не меняет. В журнал печатается форма ответа:
какие поля пришли, какого они типа, сколько в списках элементов и на что
похожи значения. Сами значения не печатаются — журнал публичного
репозитория читает кто угодно.
"""

import json
import os
import re
import urllib.parse
import urllib.request


def shape(value):
    """Описание значения без самого значения."""
    if value is None:
        return "пусто"
    if isinstance(value, bool):
        return "да/нет"
    if isinstance(value, (int, float)):
        return "число"
    if isinstance(value, list):
        if not value:
            return "список, пуст"
        return "список из %d, внутри %s" % (len(value), shape(value[0]))
    if isinstance(value, dict):
        if not value:
            return "объект, пуст"
        return "объект с полями: " + ", ".join(sorted(value)[:12])
    text = str(value).strip()
    if not text:
        return "строка, пуста"
    pattern = re.sub(r"\d", "9", re.sub(r"[A-Za-zА-Яа-яЁё]", "A", text))
    return "строка %d симв., шаблон %s" % (len(text), pattern[:24])


def request_json(request):
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def rpc(token, method, params):
    """Read-only JSON-RPC call used by the manual schema probe."""
    response = request_json(
        urllib.request.Request(
            "https://online.sbis.ru/service/?srv=1",
            data=json.dumps(
                {"jsonrpc": "2.0", "method": method, "params": params, "id": 1},
                ensure_ascii=False,
            ).encode("utf-8"),
            headers={
                "Content-Type": "application/json; charset=utf-8",
                "X-SBISAccessToken": token,
            },
            method="POST",
        )
    )
    if response.get("error"):
        error = response["error"]
        raise SystemExit("%s: %s" % (method, error.get("message") or "ошибка Saby"))
    return response.get("result")


def print_record_schema(title, value):
    """Print field names and shapes, never values from the customer account."""
    print("\n" + title)
    if isinstance(value, dict) and value.get("_type") == "record":
        fields = value.get("s") or []
        data = value.get("d") or []
        for index, field in enumerate(fields):
            name = field.get("n") if isinstance(field, dict) else str(field)
            print("  %-36s %s" % (name, shape(data[index] if index < len(data) else None)))
        return
    if isinstance(value, dict) and value.get("_type") == "recordset":
        print("  recordset, rows:", len(value.get("d") or []))
        fields = value.get("s") or []
        for field in fields:
            print("  ", field.get("n") if isinstance(field, dict) else str(field))
        return
    print("  ", shape(value))


required = ("SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY", "SABY_POINT_ID")
missing = [name for name in required if not os.environ.get(name)]
if missing:
    raise SystemExit("не заданы: " + ", ".join(missing))

token = request_json(
    urllib.request.Request(
        "https://online.sbis.ru/oauth/service/",
        data=json.dumps(
            {
                "app_client_id": os.environ["SABY_APP_CLIENT_ID"],
                "app_secret": os.environ["SABY_APP_SECRET"],
                "secret_key": os.environ["SABY_SECRET_KEY"],
            }
        ).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
).get("token")
if not token:
    raise SystemExit("СБИС не вернул токен")

query = {
    "pointId": os.environ["SABY_POINT_ID"],
    "withBalance": "true",
    "withBarcode": "true",
    "pageSize": 50,
    "page": 0,
}
search = os.environ.get("SABY_PROBE_SEARCH", "").strip()
if search.startswith("__pricechange_copy__:"):
    document_id = int(search.rsplit(":", 1)[1])
    copied = rpc(
        token,
        "PriceChange.Копировать",
        {"ИдО": str(document_id), "ИмяМетода": "PriceChange.Список"},
    )
    print("Черновик изменения цен скопирован через API.")
    print_record_schema("Ответ PriceChange.Копировать", copied)
    raise SystemExit(0)

if search.startswith("__pricechange_schema__:"):
    document_id = int(search.rsplit(":", 1)[1])
    document = rpc(
        token,
        "PriceChange.ПрочитатьДляУчастника",
        {"ИдО": str(document_id), "ИмяМетода": "PriceChange.Список"},
    )
    print_record_schema("PriceChange.ПрочитатьДляУчастника", document)
    positions = rpc(
        token,
        "PriceChangePosition.GetList",
        {
            "Фильтр": {"PriceChange": document_id, "HowPriceOrMarkupChanged": 0},
            "Сортировка": None,
            "Навигация": {"Страница": 0, "РазмерСтраницы": 50, "ЕстьЕще": True},
            "ДопПоля": [],
        },
    )
    print_record_schema("PriceChangePosition.GetList", positions)
    raise SystemExit(0)

if search:
    query["searchString"] = search

result = request_json(
    urllib.request.Request(
        "https://api.sbis.ru/retail/v2/nomenclature/list?" + urllib.parse.urlencode(query),
        headers={"X-SBISAccessToken": token},
    )
)

items = result.get("nomenclatures") or result.get("items") or result.get("result") or []
print("Ответ верхнего уровня, поля:", ", ".join(sorted(result)))
print("Позиций в ответе:", len(items))

# Поиск возвращает и разделы каталога, причём первым. У раздела нет ни
# артикула, ни штрихкодов, и если судить по нему — выйдет, что их нет ни у
# кого. Смотрим только на товары.
products = [item for item in items if not item.get("isParent")]
print("Из них товаров (не разделов):", len(products))
if not products:
    raise SystemExit("товаров не нашлось — уточните SABY_PROBE_SEARCH")

seen = {}
for item in products:
    for key, value in item.items():
        current = shape(value)
        if key not in seen or seen[key] in ("пусто", "строка, пуста", "список, пуст"):
            seen[key] = current

print("\nПоля товара (по всем %d найденным):" % len(products))
for key in sorted(seen):
    print("  %-22s %s" % (key, seen[key]))

# Отдельно разворачиваем всё, что похоже на идентификатор: именно они решают,
# можно ли связать товар с карточкой маркетплейса. Берём тот товар, у
# которого этих сведений больше всего.
def richness(item):
    return sum(1 for key in ("barcodes", "attributes", "article", "code") if item.get(key))


sample = max(products, key=richness)
print("\nПодробно по спискам идентификаторов (у самого заполненного товара):")
for key in ("barcodes", "attributes", "externalIds", "codes", "code", "codeType", "article"):
    if key not in sample:
        print("  %s: поля нет" % key)
        continue
    value = sample[key]
    if isinstance(value, list) and value:
        print("  %s: %d элем." % (key, len(value)))
        for element in value[:8]:
            if isinstance(element, dict):
                print("    объект:", {name: shape(inner) for name, inner in element.items()})
            else:
                print("    ", shape(element))
    elif isinstance(value, dict) and value:
        print("  %s: объект" % key)
        for name, inner in list(value.items())[:8]:
            print("    %-18s %s" % (name, shape(inner)))
    else:
        print("  %s: %s" % (key, shape(value)))
