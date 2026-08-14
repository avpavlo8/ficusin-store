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
        return "объект с полями: " + ", ".join(sorted(value)[:12])
    text = str(value).strip()
    if not text:
        return "строка, пуста"
    pattern = re.sub(r"\d", "9", re.sub(r"[A-Za-zА-Яа-яЁё]", "A", text))
    return "строка %d симв., шаблон %s" % (len(text), pattern[:24])


def request_json(request):
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


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
    "pageSize": 5,
    "page": 0,
}
search = os.environ.get("SABY_PROBE_SEARCH", "").strip()
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

if not items:
    raise SystemExit("позиций не пришло — уточните SABY_PROBE_SEARCH")

seen = {}
for item in items:
    for key, value in item.items():
        seen.setdefault(key, shape(value))

print("\nПоля позиции:")
for key in sorted(seen):
    print("  %-22s %s" % (key, seen[key]))

# Отдельно разворачиваем всё, что похоже на идентификатор: именно они решают,
# можно ли связать товар с карточкой маркетплейса.
print("\nПодробно по спискам идентификаторов:")
for key in ("barcodes", "attributes", "externalIds", "codes"):
    value = items[0].get(key)
    if isinstance(value, list) and value:
        print("  %s: %d элем." % (key, len(value)))
        for element in value[:6]:
            if isinstance(element, dict):
                print("    объект:", {name: shape(inner) for name, inner in element.items()})
            else:
                print("    ", shape(element))
    elif key in items[0]:
        print("  %s: %s" % (key, shape(value)))
    else:
        print("  %s: поля нет" % key)
