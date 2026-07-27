import {
  createPayment,
  isYooKassaConfigured,
} from "../../../lib/integrations/yookassa";
import { notifyNewOrder } from "../../../lib/integrations/telegram";
import type { IntegrationEnv } from "../../../lib/integrations/types";
import {
  calculatePvzDelivery,
  isCdekConfigured,
} from "../../../lib/integrations/cdek";

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

const deliveryFees: Record<string, number> = { pickup: 0, courier: 490, cdek: 690, post: 590 };
const deliveryLabels: Record<string, string> = {
  pickup: "Самовывоз в Рязани",
  courier: "Курьер по Рязани",
  cdek: "СДЭК по России",
  post: "Почта России",
};

type OrderPayload = {
  customer?: { name?: string; phone?: string; email?: string; address?: string; comment?: string };
  delivery?: string;
  items?: Array<{ id?: string; quantity?: number }>;
  cdek?: { cityCode?: number; officeCode?: string; officeAddress?: string };
};

export async function POST(request: Request) {
  try {
    const { env } = await import("cloudflare:workers");
    const integrationEnv = env as unknown as IntegrationEnv;
    const payload = (await request.json()) as OrderPayload;
    const customer = payload.customer;
    const delivery = payload.delivery ?? "";
    if (!customer?.name?.trim() || !customer.phone?.trim() || !customer.email?.trim()) {
      return Response.json({ error: "Заполните имя, телефон и email" }, { status: 400 });
    }
    if (!(delivery in deliveryFees)) {
      return Response.json({ error: "Выберите способ получения" }, { status: 400 });
    }
    if (delivery !== "pickup" && !customer.address?.trim()) {
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
    let deliveryFee = deliveryFees[delivery];
    if (delivery === "cdek") {
      if (!isCdekConfigured(integrationEnv)) {
        return Response.json({ error: "Расчёт СДЭК временно недоступен" }, { status: 503 });
      }
      if (
        !Number.isInteger(payload.cdek?.cityCode) ||
        !payload.cdek?.officeCode?.trim()
      ) {
        return Response.json({ error: "Выберите пункт выдачи СДЭК" }, { status: 400 });
      }
      const quote = await calculatePvzDelivery(integrationEnv, {
        cityCode: Number(payload.cdek.cityCode),
        weightGrams: items.reduce(
          (sum, item) => sum + item.quantity * 2000,
          0,
        ),
        lengthCm: 30,
        widthCm: 30,
        heightCm: 60,
      });
      deliveryFee = quote.price;
    }
    const total = subtotal + deliveryFee;
    const orderNumber = `ZR-${new Date().toISOString().slice(2, 10).replaceAll("-", "")}-${crypto.randomUUID().slice(0, 5).toUpperCase()}`;

    const insertOrder = await env.DB.prepare(`
      INSERT INTO orders (
        order_number, customer_name, phone, email, address, comment,
        delivery_method, delivery_fee, subtotal, total, payment_status, status
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).bind(
      orderNumber, customer.name.trim(), customer.phone.trim(), customer.email.trim(),
      customer.address?.trim() ?? "", customer.comment?.trim() ?? "", delivery,
      deliveryFee, subtotal, total, "payment_provider_pending", "new"
    ).run();

    const orderId = Number(insertOrder.meta.last_row_id);
    await env.DB.batch(
      items.map((item) =>
        env.DB.prepare(
          "INSERT INTO order_items (order_id, product_id, product_name, unit_price, quantity) VALUES (?, ?, ?, ?, ?)"
        ).bind(orderId, item.id, item.name, item.price, item.quantity)
      )
    );

    let paymentUrl: string | undefined;
    let paymentStatus = "payment_provider_pending";
    let paymentError: string | undefined;

    if (isYooKassaConfigured(integrationEnv)) {
      try {
        const payment = await createPayment(integrationEnv, {
          orderNumber,
          amount: total,
          description: `Заказ ${orderNumber} в магазине Фикусин`,
          returnUrl: `${new URL(request.url).origin}/?payment=return&order=${encodeURIComponent(orderNumber)}`,
          email: customer.email.trim(),
          items: items.map((item) => ({
            description: item.name,
            quantity: item.quantity,
            unitPrice: item.price,
          })),
          delivery: {
            description: deliveryLabels[delivery],
            price: deliveryFee,
          },
        });
        paymentUrl = payment.confirmation?.confirmation_url;
        paymentStatus = payment.status;
      } catch (error) {
        paymentError =
          error instanceof Error ? error.message : "Не удалось создать платёж";
      }
    }

    try {
      await notifyNewOrder(integrationEnv, {
        orderNumber,
        customerName: customer.name.trim(),
        phone: customer.phone.trim(),
        email: customer.email.trim(),
        address: customer.address?.trim() ?? "",
        deliveryLabel: deliveryLabels[delivery],
        total,
        items,
      });
    } catch (error) {
      console.error("Не удалось отправить заказ в Telegram", error);
    }

    return Response.json(
      { orderNumber, paymentStatus, paymentUrl, paymentError },
      { status: 201 },
    );
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Не удалось сохранить заказ" },
      { status: 500 },
    );
  }
}
