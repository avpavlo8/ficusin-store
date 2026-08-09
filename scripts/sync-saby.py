import base64
import json
import os
import urllib.error
import urllib.parse
import urllib.request

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
    # целиком, а внутри раздела листаем курсором: постраничная навигация
    # упирается в потолок каталога, курсорная — нет.
    def request_page(query):
        request = urllib.request.Request(
            "https://api.sbis.ru/retail/v2/nomenclature/list?"
            + urllib.parse.urlencode(query),
            headers={"X-SBISAccessToken": saby_token},
        )
        result = request_json(request)
        return (
            result.get("nomenclatures")
            or result.get("items")
            or result.get("result")
            or []
        )

    def load_section(base_query, folder, seen_folders, depth):
        collected = []
        known = set()
        position = None
        for _ in range(200):
            query = dict(base_query)
            if folder is not None:
                query["folder"] = folder
            if position is not None:
                query["position"] = position
                query["order"] = "after"
            fresh = [
                item
                for item in request_page(query)
                if item.get("hierarchicalId") not in known
            ]
            if not fresh:
                break
            for item in fresh:
                known.add(item.get("hierarchicalId"))
            collected.extend(fresh)
            position = fresh[-1].get("hierarchicalId")
            if position is None:
                break
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
            collected.extend(
                load_section(base_query, section, seen_folders, depth + 1)
            )
        return collected

    # Прайс-лист сужает выдачу до продаваемых позиций с ценой. Он нужен
    # для витрины, но не годится для справочника: товар, которого в этом
    # прайс-листе нет, менеджер всё равно должен уметь завести по коду.
    # Поэтому ходим дважды и склеиваем, отдавая предпочтение позиции с
    # ценой.
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

    by_id = {}
    for item in load(False):
        key = str(item.get("id"))
        if key:
            by_id[key] = item
    for item in load(True):
        key = str(item.get("id"))
        if key:
            by_id[key] = item
    catalog_items = list(by_id.values())
    if not catalog_items:
        raise RuntimeError("empty catalog")

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
        "barcode",
        "name",
        "description",
        "cost",
        "balance",
        "images",
        "published",
        "isParent",
    )
    public_catalog = [
        {key: item.get(key) for key in allowed_fields if key in item}
        for item in catalog_items
    ]

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

    report(f"saby/store-sync/success-r{len(public_catalog)}", "success")
    print("Saby catalog sync completed")
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
