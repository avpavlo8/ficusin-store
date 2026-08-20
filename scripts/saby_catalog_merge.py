"""Правила объединения полного каталога Saby и прайс-листа.

Полный каталог точки — источник складского остатка. Прайс-лист нужен только
для цены: он не должен обнулять или менять balance, который уже пришёл из
полного каталога той же точки.
"""


def _key(item):
    value = item.get("id")
    if value is None:
        return ""
    return str(value).strip()


def _empty(value):
    return value is None or value == "" or value == [] or value == {}


def _number(value):
    if value is None:
        return None
    try:
        return float(str(value).strip().replace(",", "."))
    except (TypeError, ValueError):
        return None


def merge_catalog_items(catalog_items, price_items):
    """Merge Saby snapshots without letting a price-list row replace stock.

    Returns ``(items, balance_conflicts)``. A conflict is only diagnostic: the
    full catalogue balance remains authoritative.
    """
    by_id = {}
    for item in catalog_items:
        key = _key(item)
        if key:
            by_id[key] = dict(item)

    balance_conflicts = 0
    for priced in price_items:
        key = _key(priced)
        if not key:
            continue
        if key not in by_id:
            by_id[key] = dict(priced)
            continue

        merged = by_id[key]
        base_balance = _number(merged.get("balance"))
        priced_balance = _number(priced.get("balance"))
        if (
            base_balance is not None
            and priced_balance is not None
            and base_balance != priced_balance
        ):
            balance_conflicts += 1

        for field, value in priced.items():
            if field == "balance":
                # Если полный каталог вообще не прислал balance, используем
                # запасное значение из прайс-листа. Ноль в полном каталоге —
                # полноценное значение и не заменяется.
                if _empty(merged.get(field)) and not _empty(value):
                    merged[field] = value
                continue
            if field == "cost":
                # Цена — как раз то, ради чего второй запрос выполняется.
                if not _empty(value):
                    merged[field] = value
                continue
            # Прайс-лист может содержать поле, которого нет в общем каталоге.
            # Заполняем только пробелы и не превращаем второй ответ в источник
            # названия, описания, фотографий или идентичности товара.
            if field not in merged or _empty(merged[field]):
                merged[field] = value

    return list(by_id.values()), balance_conflicts
