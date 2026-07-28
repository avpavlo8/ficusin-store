import { getRuntimeEnv } from "../../../../../lib/server/runtime-db";

type SabyCatalogItem = {
  id?: number | string;
  article?: string;
  name?: string;
  description?: string;
  cost?: number | string;
  balance?: number | string;
  images?: string[];
  published?: boolean;
  isParent?: boolean;
};

type GithubClaims = {
  iss?: string;
  aud?: string | string[];
  exp?: number;
  nbf?: number;
  repository?: string;
  ref?: string;
  workflow_ref?: string;
};

type Jwks = {
  keys: Array<JsonWebKey & { kid?: string; alg?: string }>;
};

const EXPECTED_AUDIENCE = "ficusin-store-saby-sync";
const EXPECTED_REPOSITORY = "avpavlo8/ficusin-store";
const EXPECTED_REF = "refs/heads/main";
const EXPECTED_WORKFLOW =
  "avpavlo8/ficusin-store/.github/workflows/saby-catalog-sync.yml@refs/heads/main";

let cachedJwks: { value: Jwks; expiresAt: number } | undefined;

class SyncAuthError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

function decodeBase64Url(value: string) {
  const base64 = value.replaceAll("-", "+").replaceAll("_", "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  const decoded = atob(padded);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

function decodeJson<T>(value: string): T {
  return JSON.parse(new TextDecoder().decode(decodeBase64Url(value))) as T;
}

async function getGithubJwks(): Promise<Jwks> {
  if (cachedJwks && cachedJwks.expiresAt > Date.now()) {
    return cachedJwks.value;
  }
  const response = await fetch(
    "https://token.actions.githubusercontent.com/.well-known/jwks",
  );
  if (!response.ok) {
    throw new SyncAuthError("jwks-fetch", "Не удалось проверить подпись GitHub");
  }
  const value = (await response.json()) as Jwks;
  cachedJwks = { value, expiresAt: Date.now() + 60 * 60 * 1000 };
  return value;
}

async function verifyGithubToken(token: string) {
  const parts = token.split(".");
  if (parts.length !== 3) {
    throw new SyncAuthError("jwt-format", "Некорректный токен GitHub");
  }
  const [encodedHeader, encodedPayload, encodedSignature] = parts;
  const header = decodeJson<{ alg?: string; kid?: string }>(encodedHeader);
  if (header.alg !== "RS256" || !header.kid) {
    throw new SyncAuthError("jwt-header", "Неподдерживаемая подпись GitHub");
  }

  const jwks = await getGithubJwks();
  const jwk = jwks.keys.find((key) => key.kid === header.kid);
  if (!jwk) {
    throw new SyncAuthError("jwks-kid", "Ключ подписи GitHub не найден");
  }
  let key: CryptoKey;
  try {
    key = await crypto.subtle.importKey(
      "jwk",
      jwk,
      { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
      false,
      ["verify"],
    );
  } catch {
    throw new SyncAuthError("jwk-import", "Ключ GitHub имеет неверный формат");
  }
  const valid = await crypto.subtle.verify(
    "RSASSA-PKCS1-v1_5",
    key,
    decodeBase64Url(encodedSignature),
    new TextEncoder().encode(`${encodedHeader}.${encodedPayload}`),
  );
  if (!valid) {
    throw new SyncAuthError("jwt-signature", "Подпись GitHub не прошла проверку");
  }

  const claims = decodeJson<GithubClaims>(encodedPayload);
  const now = Math.floor(Date.now() / 1000);
  const audiences = Array.isArray(claims.aud) ? claims.aud : [claims.aud];
  if (
    claims.iss !== "https://token.actions.githubusercontent.com" ||
    !audiences.includes(EXPECTED_AUDIENCE) ||
    !claims.exp ||
    claims.exp <= now ||
    (claims.nbf ?? 0) > now ||
    claims.repository !== EXPECTED_REPOSITORY ||
    claims.ref !== EXPECTED_REF ||
    claims.workflow_ref !== EXPECTED_WORKFLOW
  ) {
    throw new SyncAuthError(
      "jwt-claims",
      "GitHub Actions не имеет доступа к синхронизации",
    );
  }
}

const chunks = <T,>(items: T[], size: number) => {
  const result: T[][] = [];
  for (let index = 0; index < items.length; index += size) {
    result.push(items.slice(index, index + size));
  }
  return result;
};

function resolveSabyImage(value: string) {
  try {
    let url = new URL(value.trim(), "https://online.sbis.ru");
    const encodedParams = url.searchParams.get("params");
    if (url.pathname === "/img" && encodedParams) {
      const photoUrl = decodeJson<{ PhotoURL?: string }>(
        encodedParams,
      ).PhotoURL?.trim();
      if (photoUrl) url = new URL(photoUrl);
    }
    if (url.protocol === "http:") url.protocol = "https:";
    return url.protocol === "https:" ? url.toString() : "";
  } catch {
    return "";
  }
}

export async function POST(request: Request) {
  const suppliedToken = request.headers
    .get("X-Ficusin-GitHub-OIDC")
    ?.trim();
  if (!suppliedToken) {
    return Response.json({ error: "Доступ запрещён" }, { status: 401 });
  }

  try {
    await verifyGithubToken(suppliedToken);
  } catch (error) {
    console.error("Отклонена синхронизация Saby", error);
    const code =
      error instanceof SyncAuthError ? error.code : "auth-error";
    return Response.json(
      { error: "Доступ запрещён", code },
      {
        status: 403,
        headers: {
          "X-Saby-Sync-Error": code,
        },
      },
    );
  }

  const env = getRuntimeEnv();
  let syncRunId: number | undefined;

  try {
    const body = (await request.json()) as { items?: SabyCatalogItem[] };
    if (!Array.isArray(body.items) || body.items.length > 2000) {
      return Response.json({ error: "Некорректный каталог" }, { status: 400 });
    }

    const sourceItems = body.items.filter(
      (item) =>
        !item.isParent &&
        item.published !== false &&
        item.id !== undefined &&
        Boolean(item.name?.trim()) &&
        Number.isFinite(Number(item.cost)),
    );
    if (!sourceItems.length) {
      return Response.json({ error: "Каталог Saby пуст" }, { status: 400 });
    }

    const started = await env.DB.prepare(`
      INSERT INTO sync_runs (source, direction, status, items_read)
      VALUES ('saby', 'import', 'running', ?)
    `)
      .bind(sourceItems.length)
      .run();
    syncRunId = Number(started.meta.last_row_id);

    await env.DB.prepare(`
      INSERT INTO warehouses (saby_id, name, city, address, is_active)
      VALUES ('saby-ryazan-main', 'Основной склад', 'Рязань', 'Новосёлов, 40А', 1)
      ON CONFLICT(saby_id) DO UPDATE SET
        name = excluded.name,
        city = excluded.city,
        address = excluded.address,
        is_active = 1
    `).run();

    for (const group of chunks(sourceItems, 50)) {
      await env.DB.batch(
        group.map((item) =>
          env.DB.prepare(`
            INSERT INTO products (
              saby_id, name, slug, description, search_text, status,
              saby_updated_at, updated_at
            )
            VALUES (?, ?, ?, ?, ?, 'published', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
            ON CONFLICT(saby_id) DO UPDATE SET
              name = excluded.name,
              description = excluded.description,
              search_text = excluded.search_text,
              status = 'published',
              saby_updated_at = CURRENT_TIMESTAMP,
              updated_at = CURRENT_TIMESTAMP
          `).bind(
            String(item.id),
            item.name!.trim(),
            `saby-${item.id}`,
            item.description?.trim() ?? "",
            `${item.name} ${item.article ?? ""}`.trim(),
          ),
        ),
      );
    }

    const productRows = await env.DB.prepare(
      "SELECT id, saby_id FROM products WHERE saby_id IS NOT NULL",
    ).all<{ id: number; saby_id: string }>();
    const productIds = new Map(
      productRows.results.map((row) => [row.saby_id, row.id]),
    );

    const articleCounts = new Map<string, number>();
    for (const item of sourceItems) {
      const article = item.article?.trim();
      if (article) articleCounts.set(article, (articleCounts.get(article) ?? 0) + 1);
    }

    for (const group of chunks(sourceItems, 40)) {
      await env.DB.batch(
        group.map((item) => {
          const productId = productIds.get(String(item.id));
          if (!productId) throw new Error("Не найден созданный товар");
          const article = item.article?.trim();
          const sku =
            article && articleCounts.get(article) === 1
              ? article
              : `${article || "SABY"}-${item.id}`;
          return env.DB.prepare(`
            INSERT INTO product_variants (
              product_id, saby_id, sku, label, base_price_minor,
              is_active, saby_updated_at, updated_at
            )
            VALUES (?, ?, ?, 'Основной размер', ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
            ON CONFLICT(saby_id) DO UPDATE SET
              product_id = excluded.product_id,
              sku = excluded.sku,
              base_price_minor = excluded.base_price_minor,
              is_active = 1,
              saby_updated_at = CURRENT_TIMESTAMP,
              updated_at = CURRENT_TIMESTAMP
          `).bind(
            productId,
            String(item.id),
            sku,
            Math.max(0, Math.round(Number(item.cost) * 100)),
          );
        }),
      );
    }

    const variantRows = await env.DB.prepare(
      "SELECT id, saby_id FROM product_variants WHERE saby_id IS NOT NULL",
    ).all<{ id: number; saby_id: string }>();
    const variantIds = new Map(
      variantRows.results.map((row) => [row.saby_id, row.id]),
    );
    const warehouse = await env.DB.prepare(
      "SELECT id FROM warehouses WHERE saby_id = 'saby-ryazan-main'",
    ).first<{ id: number }>();
    if (!warehouse) throw new Error("Не удалось создать склад");

    for (const group of chunks(sourceItems, 40)) {
      await env.DB.batch(
        group.map((item) => {
          const variantId = variantIds.get(String(item.id));
          if (!variantId) throw new Error("Не найден созданный вариант товара");
          return env.DB.prepare(`
            INSERT INTO inventory (
              warehouse_id, variant_id, available_qty, reserved_qty, synced_at
            )
            VALUES (?, ?, ?, 0, CURRENT_TIMESTAMP)
            ON CONFLICT(warehouse_id, variant_id) DO UPDATE SET
              available_qty = excluded.available_qty,
              synced_at = CURRENT_TIMESTAMP
          `).bind(
            warehouse.id,
            variantId,
            Math.max(0, Math.floor(Number(item.balance ?? 0))),
          );
        }),
      );
    }

    for (const group of chunks(sourceItems, 20)) {
      const statements = group.flatMap((item) => {
        const productId = productIds.get(String(item.id));
        if (!productId) return [];
        const images = (item.images ?? [])
          .map(resolveSabyImage)
          .filter(Boolean)
          .slice(0, 8);
        return [
          env.DB.prepare("DELETE FROM product_media WHERE product_id = ?").bind(
            productId,
          ),
          ...images.map((image, index) =>
            env.DB.prepare(`
              INSERT INTO product_media (
                product_id, object_key, alt_text, sort_order, is_primary
              ) VALUES (?, ?, ?, ?, ?)
            `).bind(productId, image, item.name, index, index === 0 ? 1 : 0),
          ),
        ];
      });
      if (statements.length) await env.DB.batch(statements);
    }

    const receivedIds = new Set(sourceItems.map((item) => String(item.id)));
    const missingProductIds = productRows.results
      .filter((row) => !receivedIds.has(row.saby_id))
      .map((row) => row.id);
    for (const group of chunks(missingProductIds, 50)) {
      await env.DB.batch(
        group.map((productId) =>
          env.DB.prepare(
            "UPDATE products SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
          ).bind(productId),
        ),
      );
    }

    await env.DB.prepare(`
      UPDATE sync_runs SET
        status = 'success',
        items_created = ?,
        items_updated = ?,
        finished_at = CURRENT_TIMESTAMP
      WHERE id = ?
    `)
      .bind(sourceItems.length, sourceItems.length, syncRunId)
      .run();

    return Response.json({
      ok: true,
      itemsRead: sourceItems.length,
      syncedAt: new Date().toISOString(),
    });
  } catch (error) {
    console.error("Ошибка сохранения каталога Saby", error);
    if (syncRunId) {
      await env.DB.prepare(`
        UPDATE sync_runs SET
          status = 'failed',
          errors_count = 1,
          error_summary = ?,
          finished_at = CURRENT_TIMESTAMP
        WHERE id = ?
      `)
        .bind(
          error instanceof Error ? error.message.slice(0, 500) : "Неизвестная ошибка",
          syncRunId,
        )
        .run();
    }
    return Response.json({ error: "Не удалось обновить каталог" }, { status: 500 });
  }
}
