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

type OrderPayload = {
  customer?: { name?: string; phone?: string; email?: string; address?: string; comment?: string };
  delivery?: string;
  items?: Array<{ id?: string; quantity?: number }>;
};

export async function POST(request: Request) {
  try {
    const { env } = await import("cloudflare:workers");
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

    const items = (payload.items ?? []).map((item) => {
      const product = item.id ? catalog.get(item.id) : undefined;
      const quantity = Math.max(1, Math.min(20, Math.floor(item.quantity ?? 0)));
      if (!product || !item.id) throw new Error("В корзине найден неизвестный товар");
      return { id: item.id, ...product, quantity };
    });
    if (!items.length) return Response.json({ error: "Корзина пуста" }, { status: 400 });

    const subtotal = items.reduce((sum, item) => sum + item.price * item.quantity, 0);
    const deliveryFee = deliveryFees[delivery];
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

    return Response.json({ orderNumber, paymentStatus: "payment_provider_pending" }, { status: 201 });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Не удалось сохранить заказ" },
      { status: 500 },
    );
  }
}
