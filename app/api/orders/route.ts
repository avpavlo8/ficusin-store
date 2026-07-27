import {
  calculatePvzDelivery,
  getOffices,
} from "../../../lib/integrations/cdek";
import { sendTelegramOrder } from "../../../lib/integrations/telegram";
import { normalizeRussianPhone } from "../../../lib/phone";

const catalog = new Map([
  ["strelitzia-nicolai", { name: "Стрелиция Николая", price: 6490 }],
  ["ficus-burgundy", { name: "Фикус Бургунди", price: 3290 }],
  ["epipremnum-aureum", { name: "Эпипремнум золотистый", price: 1890 }],
  ["anthurium-terracotta", { name: "Антуриум Терракота", price: 2790 }],
  ["monstera-deliciosa", { name: "Монстера Делициоза", price: 4590 }],
  ["ficus-compacta", { name: "Фикус Компакта", price: 2390 }],
  ["pothos-neon", { name: "Эпипремнум Неон", price: 2190 }],
  ["anthurium-mini", { name: "Антуриум Мини", price: 1990 }],
]);

const deliveryFees: Record<string, number> = { pickup: 0, courier: 490, post: 590 };

type OrderPayload = {
  customer?: { name?: string; phone?: string; email?: string; address?: string; comment?: string };
  delivery?: string;
  items?: Array<{ id?: string; quantity?: number }>;
  cdek?: {
    cityCode?: number;
    cityName?: string;
    officeCode?: string;
  };
};

export async function POST(request: Request) {
  try {
    const { env } = await import("cloudflare:workers");
    const payload = (await request.json()) as OrderPayload;
    const customer = payload.customer;
    const delivery = payload.delivery ?? "";
    const normalizedPhone = normalizeRussianPhone(customer?.phone ?? "");
    if (!customer?.name?.trim() || !customer.email?.trim()) {
      return Response.json({ error: "Заполните имя, телефон и email" }, { status: 400 });
    }
    if (!normalizedPhone) {
      return Response.json(
        { error: "Введите корректный российский номер телефона" },
        { status: 400 },
      );
    }
    if (!(delivery in deliveryFees) && delivery !== "cdek") {
      return Response.json({ error: "Выберите способ получения" }, { status: 400 });
    }
    if (delivery !== "pickup" && delivery !== "cdek" && !customer.address?.trim()) {
      return Response.json({ error: "Укажите адрес или пункт выдачи" }, { status: 400 });
    }

    const items = await Promise.all((payload.items ?? []).map(async (item) => {
      const quantity = Math.max(1, Math.min(20, Math.floor(item.quantity ?? 0)));
      if (!item.id) throw new Error("В корзине найден неизвестный товар");

      const liveProduct = await env.DB.prepare(`
        SELECT
          p.name,
          pv.base_price_minor,
          COALESCE(SUM(MAX(i.available_qty - i.reserved_qty, 0)), 0) AS stock
        FROM products p
        JOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1
        LEFT JOIN inventory i ON i.variant_id = pv.id
        WHERE p.slug = ? AND p.status = 'published'
        GROUP BY p.id, pv.id
        ORDER BY pv.id
        LIMIT 1
      `)
        .bind(item.id)
        .first<{ name: string; base_price_minor: number; stock: number }>();

      if (liveProduct) {
        if (Number(liveProduct.stock) < quantity) {
          throw new Error(
            `${liveProduct.name}: доступно только ${liveProduct.stock} шт.`,
          );
        }
        return {
          id: item.id,
          name: liveProduct.name,
          price: liveProduct.base_price_minor / 100,
          quantity,
        };
      }

      const demoProduct = catalog.get(item.id);
      if (!demoProduct) throw new Error("В корзине найден неизвестный товар");
      return { id: item.id, ...demoProduct, quantity };
    }));
    if (!items.length) return Response.json({ error: "Корзина пуста" }, { status: 400 });

    const subtotal = items.reduce((sum, item) => sum + item.price * item.quantity, 0);
    let deliveryFee = deliveryFees[delivery] ?? 0;
    let deliveryAddress = customer.address?.trim() ?? "";
    let cdekCityCode: number | null = null;
    let cdekCityName: string | null = null;
    let cdekOfficeCode: string | null = null;
    let cdekTariffCode: number | null = null;
    if (delivery === "cdek") {
      cdekCityCode = Number(payload.cdek?.cityCode);
      cdekOfficeCode = payload.cdek?.officeCode?.trim() ?? "";
      if (!Number.isInteger(cdekCityCode) || cdekCityCode <= 0 || !cdekOfficeCode) {
        return Response.json(
          { error: "Выберите город и пункт выдачи СДЭК" },
          { status: 400 },
        );
      }
      const [quote, offices] = await Promise.all([
        calculatePvzDelivery(
          cdekCityCode,
          items.reduce((sum, item) => sum + item.quantity, 0),
        ),
        getOffices(cdekCityCode),
      ]);
      const office = offices.find((item) => item.code === cdekOfficeCode);
      if (!office) {
        return Response.json(
          { error: "Выбранный пункт СДЭК больше недоступен" },
          { status: 400 },
        );
      }
      deliveryFee = quote.price;
      cdekTariffCode = quote.tariffCode;
      cdekCityName = office.location.city || payload.cdek?.cityName?.trim() || "";
      deliveryAddress =
        office.location.address_full || office.location.address || office.name;
    }
    const total = subtotal + deliveryFee;
    const orderNumber = `ZR-${new Date().toISOString().slice(2, 10).replaceAll("-", "")}-${crypto.randomUUID().slice(0, 5).toUpperCase()}`;

    const insertOrder = await env.DB.prepare(`
      INSERT INTO orders (
        order_number, customer_name, phone, email, address, comment,
        delivery_method, delivery_fee, cdek_city_code, cdek_city_name,
        cdek_office_code, cdek_tariff_code, subtotal, total, payment_status, status
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).bind(
      orderNumber, customer.name.trim(), normalizedPhone, customer.email.trim(),
      deliveryAddress, customer.comment?.trim() ?? "", delivery,
      deliveryFee, cdekCityCode, cdekCityName, cdekOfficeCode, cdekTariffCode,
      subtotal, total, "payment_provider_pending", "new"
    ).run();

    const orderId = Number(insertOrder.meta.last_row_id);
    await env.DB.batch(
      items.map((item) =>
        env.DB.prepare(
          "INSERT INTO order_items (order_id, product_id, product_name, unit_price, quantity) VALUES (?, ?, ?, ?, ?)"
        ).bind(orderId, item.id, item.name, item.price, item.quantity)
      )
    );

    try {
      await sendTelegramOrder({
        orderNumber,
        customerName: customer.name.trim(),
        phone: normalizedPhone,
        email: customer.email.trim(),
        address: deliveryAddress,
        comment: customer.comment?.trim() ?? "",
        deliveryMethod: delivery,
        deliveryFee,
        subtotal,
        total,
        items: items.map((item) => ({
          name: item.name,
          price: item.price,
          quantity: item.quantity,
        })),
      });
      await env.DB.prepare(
        "UPDATE orders SET telegram_notified_at = CURRENT_TIMESTAMP WHERE id = ?",
      )
        .bind(orderId)
        .run();
    } catch (error) {
      console.error("Не удалось отправить заказ в Telegram", error);
    }

    return Response.json({ orderNumber, paymentStatus: "payment_provider_pending" }, { status: 201 });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Не удалось сохранить заказ" },
      { status: 500 },
    );
  }
}
