"""Правила объединения полного каталога Saby и «Общего прайс-листа».

Полный каталог нужен, чтобы менеджер мог найти любую номенклатуру по коду.
Для товара, который присутствует в выбранном прайс-листе, именно прайс-лист
является источником боевой цены и остатка витрины. Это соответствует тому,
что оператор видит в Saby в контексте этого прайс-листа.
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
    """Merge Saby snapshots with the selected price list authoritative for sale data.

    Returns ``(items, balance_conflicts)``. A conflict is diagnostic only: if
    the price-list row contains ``balance``, that value wins. The full catalog
    remains the fallback for items absent from the price list and for metadata.
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
                # Для продаваемой позиции остаток выбранного прайс-листа
                # авторитетнее общего справочника. Ноль здесь тоже валиден:
                # товар действительно может закончиться.
                if "balance" in priced and not _empty(value):
                    merged[field] = value
                continue
            if field == "cost":
                # Цена выбранного прайс-листа всегда важнее общей карточки.
                if not _empty(value):
                    merged[field] = value
                continue
            # Название, описание, фотографии и идентичность берём из полного
            # каталога. Прайс-лист лишь заполняет отсутствующие значения.
            if field not in merged or _empty(merged[field]):
                merged[field] = value

    return list(by_id.values()), balance_conflicts


def catalogue_ids_in_section(catalog_items, section_name):
    """Return canonical product IDs that belong to one Saby catalogue branch."""
    wanted = str(section_name or "").strip().casefold()
    if not wanted:
        return set()

    result = set()
    for item in catalog_items:
        # Saby may serialise isParent as the strings "true"/"false".
        # Membership is determined by ancestry; folder IDs cannot occur in
        # receipt positions, so excluding by a loosely typed flag is unsafe
        # and unnecessary.
        path = item.get("_ficusinSectionPath") or []
        if not any(str(part).strip().casefold() == wanted for part in path):
            continue
        canonical_id = str(item.get("id") or "").strip()
        if canonical_id:
            result.add(canonical_id)
    return result


def build_sales_product_ids(catalog_items):
    """Map every Saby product identifier to the numeric catalogue card ID."""
    result = {}
    for item in catalog_items:
        canonical_id = str(item.get("id") or "").strip()
        if not canonical_id:
            continue
        for field in ("id", "externalId", "hierarchicalId", "uuid", "UUID", "code", "nomNumber", "article"):
            source_id = str(item.get(field) or "").strip()
            if source_id:
                result[source_id.casefold()] = canonical_id
    return result
