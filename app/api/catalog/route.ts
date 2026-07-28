import { getRuntimeEnv } from "../../../lib/server/runtime-db";

export async function GET() {
  try {
    const env = getRuntimeEnv();
    const result = await env.DB.prepare(`
      SELECT
        p.slug AS id,
        p.name,
        p.latin_name AS latin,
        'Растения' AS category,
        pv.base_price_minor AS price_minor,
        COALESCE(
          (
            SELECT pm.object_key
            FROM product_media pm
            WHERE pm.product_id = p.id
            ORDER BY pm.is_primary DESC, pm.sort_order ASC
            LIMIT 1
          ),
          '/assets/hero-monstera.png'
        ) AS image,
        pv.label AS size,
        COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0) AS stock
      FROM products p
      JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
      LEFT JOIN inventory i ON i.variant_id = pv.id
      WHERE p.status = 'published'
      GROUP BY p.id, pv.id
      HAVING COALESCE(SUM(GREATEST(i.available_qty - i.reserved_qty, 0)), 0) > 0
      ORDER BY p.is_featured DESC, p.name ASC
      LIMIT 1000
    `).all<{
      id: string;
      name: string;
      latin: string;
      category: string;
      price_minor: number;
      image: string;
      size: string;
      stock: number;
    }>();

    return Response.json(
      {
        products: result.results.map((product) => ({
          id: product.id,
          name: product.name,
          latin: product.latin,
          category: product.category,
          price: product.price_minor / 100,
          image: product.image,
          light: "Уточните у консультанта",
          size: product.size,
          stock: Number(product.stock),
        })),
      },
      {
        headers: {
          "Cache-Control": "public, max-age=60, stale-while-revalidate=300",
        },
      },
    );
  } catch (error) {
    console.error("Не удалось прочитать каталог", error);
    return Response.json({ products: [] }, { status: 503 });
  }
}
