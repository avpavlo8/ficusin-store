import base64
import datetime
import json
import os
import urllib.error
import urllib.parse
import urllib.request

from saby_catalog_merge import build_sales_product_ids, merge_catalog_items

stage = "settings"


def request_json(request):
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def report(context, state):
    request = urllib.request.Request(
        "https://api.github.com/repos/"
        + os.environ["GITHUB_REPOSITORY"]
        + "/statuses/"
        + os.environ["GITHUB_SHA"],
        data=json.dumps(
            {
                "state": state,
                "description": (
                    "Saby catalog delivered to the store"
                    if state == "success"
                    else "Saby catalog sync failed"
                ),
                "context": context[:100],
            }
        ).encode("utf-8"),
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + os.environ["GH_TOKEN"],
            "X-GitHub-Api-Version": "2022-11-28",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=30):
        pass


try:
    required = (
        "SABY_APP_CLIENT_ID",
        "SABY_APP_SECRET",
        "SABY_SECRET_KEY",
        "ACTIONS_ID_TOKEN_REQUEST_URL",
        "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
    )
    if any(not os.environ.get(name) for name in required):
        raise RuntimeError("missing settings")

    stage = "saby-auth"
    auth_request = urllib.request.Request(
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
    saby_token = request_json(auth_request).get("token")
    if not saby_token:
        raise RuntimeError("no Saby token")

    stage = "saby-catalog"

    # Каталог отдаётся по одному уровню за раз: без folder приходят только
    # записи корня — папки и то, что лежит рядом с ними. Позиция внутри
    # «Грунта» или «Горшков» так не появится никогда, за ней нужно
    # спуститься в раздел отдельным запросом. Поэтому обходим дерево
    # целиком и листаем каждый раздел отдельно: потолок в тысячу позиций
    # считается на каталог, а не на раздел, и по разделам мы в него не
    # упираемся. Курсорную навигацию СБИС на этом методе не принимает —
    # position с order отвечают пятисотой.
    def request_page(query):
        request = urllib.request.Request(
            "https://api.sbis.ru/retail/v2/nomenclature/list?"
            + urllib.parse.urlencode(query),
            headers={"X-SBISAccessToken": saby_token},
        )
        try:
            result = request_json(request)
        except urllib.error.HTTPError as error:
            print(
                "saby request failed: %s %s"
                % (error.code, urllib.parse.urlencode(query))
            )
            raise
        return (
            result.get("nomenclatures")
            or result.get("items")
            or result.get("result")
            or []
        )

    def load_section(base_query, folder, seen_folders, depth, section_path=()):
        collected = []
        known = set()
        for page in range(200):
            query = dict(base_query)
            if folder is not None:
                query["folder"] = folder
            query["page"] = page
            # Повтор вместо новой страницы означает, что листать больше
            # нечего: так обмен не зависнет, если page вдруг перестанет
            # учитываться.
            fresh = []
            for source_item in request_page(query):
                if source_item.get("hierarchicalId") in known:
                    continue
                item = dict(source_item)
                # The retail API returns one catalogue level at a time and does
                # not repeat parent names on child products. Keep the ancestry
                # while walking the tree so sales can be limited to the exact
                # Saby section selected by the store owner.
                item["_ficusinSectionPath"] = list(section_path)
                fresh.append(item)
            if not fresh:
                break
            for item in fresh:
                known.add(item.get("hierarchicalId"))
            collected.extend(fresh)
        else:
            raise RuntimeError("pagination limit")

        if depth >= 8:
            return collected
        for item in list(collected):
            section = item.get("hierarchicalId")
            if not item.get("isParent") or section is None:
                continue
            if section in seen_folders:
                continue
            seen_folders.add(section)
            child_path = section_path + (str(item.get("name") or "").strip(),)
            collected.extend(
                load_section(
                    base_query, section, seen_folders, depth + 1, child_path
                )
            )
        return collected

    # Полный каталог точки является источником остатков. Прайс-лист нужен
    # только для цены. Раньше второй ответ целиком заменял первый по id, и
    # нулевой/устаревший balance одной позиции из прайса мог обнулить товар
    # на сайте, хотя в общем каталоге точки остаток был положительным.
    def load(with_price_list):
        query = {
            "pointId": os.environ["SABY_POINT_ID"],
            "withBalance": "true",
            "withBarcode": "true",
            "pageSize": 1000,
        }
        if with_price_list:
            query["priceListId"] = os.environ["SABY_PRICE_LIST_ID"]
            query["noStopList"] = "true"
        return load_section(query, None, set(), 0)

    full_catalog = load(False)
    price_catalog = load(True)
    catalog_items, balance_conflicts = merge_catalog_items(
        full_catalog, price_catalog
    )
    if not catalog_items:
        raise RuntimeError("empty catalog")

    # Retail receipts identify nomenclature by NomenclatureUUID, while the
    # catalogue endpoint uses its own `id` as the canonical key. Build the
    # translation from every stable identifier returned in the same catalogue
    # snapshot so sales and balances reach one product in the store database.
    # nomNumber/code (X...) is searchable and human-readable, but it is not
    # the catalogue identity. Always translate sales to catalogue.id so stock,
    # sales and procurement all refer to one card.
    sales_product_ids = build_sales_product_ids(catalog_items)
    stage = "saby-sales"
    sales_days = max(7, min(365, int(os.environ.get("SABY_SALES_SYNC_DAYS", "365"))))
    moscow = datetime.timezone(datetime.timedelta(hours=3))
    sales_to = datetime.datetime.now(moscow).date()
    sales_from = sales_to - datetime.timedelta(days=sales_days - 1)
    sales_by_day = {}
    for page in range(1000):
        query = {
            "pointId": os.environ["SABY_POINT_ID"],
            "fromDateTime": sales_from.strftime("%Y-%m-%d 00:00:00"),
            "toDateTime": sales_to.strftime("%Y-%m-%d 23:59:59"),
            "page": page,
            "pageSize": 100,
        }
        request = urllib.request.Request(
            "https://api.sbis.ru/retail/order/list?" + urllib.parse.urlencode(query),
            headers={"X-SBISAccessToken": saby_token},
        )
        response = request_json(request)
        orders = response.get("orders") or response.get("sales") or []
        for order in orders:
            if order.get("Deleted"):
                continue
            raw_date = order.get("ClosedWTZ") or order.get("DateWTZ") or order.get("OpenedWTZ") or ""
            sale_date = str(raw_date)[:10]
            if len(sale_date) != 10:
                continue
            is_return = bool(order.get("Return"))
            positions = order.get("SaleNomenclatures") or order.get("Positions") or []
            for position in positions:
                if position.get("Refused") or position.get("IsModifier"):
                    continue
                source_id = str(
                    position.get("NomenclatureUUID")
                    or position.get("NomenclatureID")
                    or position.get("Nomenclature")
                    or position.get("NomNumber")
                    or position.get("Article")
                    or ""
                ).strip()
                if not source_id:
                    continue
                saby_id = sales_product_ids.get(source_id.casefold(), source_id)
                try:
                    quantity = float(position.get("Quantity") or 0)
                    total = float(position.get("TotalPrice") or 0)
                except (TypeError, ValueError):
                    continue
                sign = -1 if is_return or position.get("IsReturn") else 1
                key = (sale_date, saby_id)
                current = sales_by_day.setdefault(key, {"units": 0, "grossRub": 0.0})
                current["units"] += sign * int(round(abs(quantity)))
                current["grossRub"] += sign * abs(total)
        if len(orders) < 100:
            break
    else:
        raise RuntimeError("sales pagination limit")

    public_sales = [
        {
            "date": sale_date,
            "sabyId": saby_id,
            "units": values["units"],
            "grossRub": round(values["grossRub"], 2),
        }
        for (sale_date, saby_id), values in sorted(sales_by_day.items())
        if values["units"] != 0 or values["grossRub"] != 0
    ]

    stage = "github-oidc"
    oidc_url = os.environ["ACTIONS_ID_TOKEN_REQUEST_URL"]
    separator = "&" if "?" in oidc_url else "?"
    oidc_request = urllib.request.Request(
        oidc_url
        + separator
        + urllib.parse.urlencode({"audience": "ficusin-store-saby-sync"}),
        headers={
            "Authorization": "Bearer "
            + os.environ["ACTIONS_ID_TOKEN_REQUEST_TOKEN"],
        },
    )
    oidc_token = request_json(oidc_request).get("value")
    if not oidc_token:
        raise RuntimeError("no OIDC token")

    payload_part = oidc_token.split(".")[1]
    payload_part += "=" * (-len(payload_part) % 4)
    claims = json.loads(base64.urlsafe_b64decode(payload_part.encode("ascii")))
    audience = claims.get("aud")
    audiences = audience if isinstance(audience, list) else [audience]
    flags = "".join(
        [
            (
                "i1"
                if claims.get("iss")
                == "https://token.actions.githubusercontent.com"
                else "i0"
            ),
            "a1" if "ficusin-store-saby-sync" in audiences else "a0",
            (
                "r1"
                if claims.get("repository") == "avpavlo8/ficusin-store"
                else "r0"
            ),
            "f1" if claims.get("ref") == "refs/heads/main" else "f0",
            (
                "w1"
                if claims.get("workflow_ref")
                == "avpavlo8/ficusin-store/.github/workflows/"
                "saby-catalog-sync.yml@refs/heads/main"
                else "w0"
            ),
        ]
    )
    report("saby/oidc-" + flags, "success")

    # Коды нужны, чтобы менеджер мог завести товар в магазин по тому же
    # номеру, который видит в СБИС. Зовётся он в выгрузке по-разному, поэтому
    # пропускаем все три варианта и штрихкод заодно.
    allowed_fields = (
        "id",
        "article",
        "code",
        "externalId",
        "nomNumber",
        # Штрихкодов у растения несколько: два EAN13 с этикетки и код,
        # который сгенерировал маркетплейс (вида OZN…). Раньше просили
        # «barcode» в единственном числе — такого поля в ответе нет вовсе,
        # и до магазина не доезжал ни один код. Именно по ним товар
        # опознаётся на площадке.
        "barcodes",
        "name",
        "description",
        "cost",
        "balance",
        "images",
        # Raw supplier characteristics are persisted for diagnostics and the
        # backend maps only a conservative allowlist into category attributes.
        "attributes",
        "published",
        "isParent",
    )
    public_catalog = [
        {key: item.get(key) for key in allowed_fields if key in item}
        for item in catalog_items
    ]

    # Keep enough aggregate evidence in Actions to distinguish an invalid
    # warehouse/point response from a mapping or database failure. Product
    # names and identifiers are deliberately omitted from the public log.
    balances_present = sum("balance" in item for item in public_catalog)
    def is_positive_balance(value):
        try:
            return float(str(value).strip().replace(",", ".")) > 0
        except (TypeError, ValueError):
            return False

    positive_balances = sum(
        is_positive_balance(item.get("balance")) for item in public_catalog
    )
    print(
        "Saby catalog health: "
        f"items={len(public_catalog)} balanceFields={balances_present} "
        f"positiveBalances={positive_balances} "
        f"balanceConflicts={balance_conflicts} point={os.environ['SABY_POINT_ID']}"
    )

    stage = "store"
    sync_request = urllib.request.Request(
        os.environ["FICUSIN_SYNC_URL"],
        data=json.dumps(
            {"items": public_catalog}, ensure_ascii=False
        ).encode("utf-8"),
        headers={
            "X-Ficusin-GitHub-OIDC": oidc_token,
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "Ficusin-Saby-Sync/2.0",
        },
        method="POST",
    )
    response = request_json(sync_request)
    if not response.get("ok"):
        raise RuntimeError("store rejected catalog")

    stage = "store-sales"
    sales_sync_url = os.environ.get("FICUSIN_SALES_SYNC_URL") or os.environ["FICUSIN_SYNC_URL"].replace(
        "/catalog", "/sales"
    )
    sales_request = urllib.request.Request(
        sales_sync_url,
        data=json.dumps(
            {
                "from": sales_from.isoformat(),
                "to": sales_to.isoformat(),
                "items": public_sales,
            },
            ensure_ascii=False,
        ).encode("utf-8"),
        headers={
            "X-Ficusin-GitHub-OIDC": oidc_token,
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "Ficusin-Saby-Sync/3.0",
        },
        method="POST",
    )
    sales_response = request_json(sales_request)
    if not sales_response.get("ok"):
        raise RuntimeError("store rejected sales")
    linked_sales = int(sales_response.get("linkedRows") or 0)
    recommendation_rows = int(sales_response.get("recommendationRows") or 0)
    if public_sales and linked_sales == 0:
        raise RuntimeError("store linked zero Saby sales")

    report(
        f"saby/store-sync/success-c{len(public_catalog)}-s{len(public_sales)}-l{linked_sales}-r{recommendation_rows}",
        "success",
    )
    print(
        "Saby catalog sync completed: "
        f"catalog={len(public_catalog)} sales={len(public_sales)} "
        f"linked={linked_sales} recommendations={recommendation_rows}"
    )
except urllib.error.HTTPError as error:
    safe_code = error.headers.get("X-Saby-Sync-Error")
    if not safe_code:
        try:
            safe_code = json.loads(
                error.read().decode("utf-8", errors="replace")
            ).get("code")
        except Exception:
            safe_code = None
    suffix = safe_code or f"http{error.code}"
    report(f"saby/store-sync/{stage}-{suffix}", "failure")
    raise SystemExit(f"Sync failed at {stage}: {suffix}") from None
except Exception:
    report(f"saby/store-sync/{stage}-error", "failure")
    raise SystemExit(f"Sync failed at {stage}") from None
