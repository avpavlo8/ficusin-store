import {
  fetchSabyCatalog,
  isSabyConfigured,
} from "../../../../../lib/integrations/saby";
import type { IntegrationEnv } from "../../../../../lib/integrations/types";

export async function GET(request: Request) {
  try {
    const { env } = await import("cloudflare:workers");
    const integrationEnv = env as unknown as IntegrationEnv;
    if (!isSabyConfigured(integrationEnv)) {
      return Response.json({ error: "Saby ещё не настроен" }, { status: 503 });
    }
    const expectedSecret = integrationEnv.SABY_SYNC_SECRET?.trim();
    const suppliedSecret = request.headers
      .get("Authorization")
      ?.replace(/^Bearer\s+/i, "")
      .trim();
    if (!expectedSecret || suppliedSecret !== expectedSecret) {
      return Response.json({ error: "Доступ запрещён" }, { status: 403 });
    }

    const items = (await fetchSabyCatalog(integrationEnv)).filter(
      (item) => !item.isParent,
    );
    return Response.json({
      items,
      count: items.length,
      fetchedAt: new Date().toISOString(),
    });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Ошибка синхронизации Saby" },
      { status: 502 },
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

export async function POST(request: Request) {
  try {
    const { env } = await import("cloudflare:workers");
    const integrationEnv = env as unknown as IntegrationEnv;
    if (!isSabyConfigured(integrationEnv)) {
      return Response.json({ error: "Saby ещё не настроен" }, { status: 503 });
    }
    const expectedSecret = integrationEnv.SABY_SYNC_SECRET?.trim();
    const suppliedSecret = request.headers
      .get("Authorization")
      ?.replace(/^Bearer\s+/i, "")
      .trim();
    if (!expectedSecret || suppliedSecret !== expectedSecret) {
      return Response.json({ error: "Доступ запрещён" }, { status: 403 });
    }

    const sourceItems = (await fetchSabyCatalog(integrationEnv)).filter(
      (item) =>
        !item.isParent &&
        item.published !== false &&
        Boolean(item.name?.trim()) &&
        Number.isFinite(Number(item.cost)),
    );
    const warehouseExternalId =
      integrationEnv.SABY_WAREHOUSE_ID?.trim() || "saby-ryazan-main";

    await env.DB.prepare(`
      INSERT INTO warehouses (saby_id, name, city, address, is_active)
      VALUES (?, 'Основной склад', 'Рязань', 'Новосёлов, 40А', 1)
      ON CONFLICT(saby_id) DO UPDATE SET is_active = 1
    `)
      .bind(warehouseExternalId)
      .run();

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
            item.name.trim(),
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

    for (const group of chunks(sourceItems, 25)) {
      await env.DB.batch(
        group.flatMap((item) => {
          const productId = productIds.get(String(item.id));
          const image = item.images?.find((value) => value?.trim());
          if (!productId || !image) return [];
          return [
            env.DB.prepare("DELETE FROM product_media WHERE product_id = ?").bind(
              productId,
            ),
            env.DB.prepare(`
              INSERT INTO product_media (
                product_id, object_key, alt_text, sort_order, is_primary
              ) VALUES (?, ?, ?, 0, 1)
            `).bind(productId, image, item.name),
          ];
        }),
      );
    }

    for (const group of chunks(sourceItems, 50)) {
      await env.DB.batch(
        group.flatMap((item) => {
          const productId = productIds.get(String(item.id));
          if (!productId) return [];
          return [
            env.DB.prepare(`
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
              item.article?.trim() || `SABY-${item.id}`,
              Math.max(0, Math.round(Number(item.cost) * 100)),
            ),
          ];
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
      "SELECT id FROM warehouses WHERE saby_id = ?",
    )
      .bind(warehouseExternalId)
      .first<{ id: number }>();
    if (!warehouse) throw new Error("Не удалось создать склад");

    for (const group of chunks(sourceItems, 50)) {
      await env.DB.batch(
        group.flatMap((item) => {
          const variantId = variantIds.get(String(item.id));
          if (!variantId) return [];
          return [
            env.DB.prepare(`
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
            ),
          ];
        }),
      );
    }

    return Response.json({
      ok: true,
      itemsRead: sourceItems.length,
      syncedAt: new Date().toISOString(),
    });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Ошибка синхронизации Saby" },
      { status: 502 },
    );
  }
}
