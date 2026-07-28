import { getPayment } from "../../../../lib/integrations/yookassa";
import type { IntegrationEnv } from "../../../../lib/integrations/types";
import { getRuntimeEnv } from "../../../../lib/server/runtime-db";

type YooKassaNotification = {
  type?: string;
  event?: string;
  object?: { id?: string };
};

function getYooKassaEnv(): IntegrationEnv {
  return {
    YOOKASSA_SHOP_ID: process.env.YOOKASSA_SHOP_ID,
    YOOKASSA_SECRET_KEY: process.env.YOOKASSA_SECRET_KEY,
    YOOKASSA_VAT_CODE: process.env.YOOKASSA_VAT_CODE,
  };
}

export async function POST(request: Request) {
  try {
    const notification = (await request.json()) as YooKassaNotification;
    const paymentId = notification.object?.id;
    if (!paymentId || notification.type !== "notification") {
      return Response.json({ error: "Некорректное уведомление" }, { status: 400 });
    }

    const payment = await getPayment(getYooKassaEnv(), paymentId);
    const orderNumber = payment.metadata?.order_number;
    if (!orderNumber) {
      return Response.json({ error: "В платеже нет номера заказа" }, { status: 400 });
    }

    const paymentStatus =
      payment.status === "succeeded"
        ? "paid"
        : payment.status === "canceled"
          ? "cancelled"
          : payment.status;
    const orderStatus = payment.status === "succeeded" ? "confirmed" : "new";
    const env = getRuntimeEnv();

    await env.DB.prepare(
      `UPDATE orders
       SET payment_status = ?, status = ?
       WHERE order_number = ?`,
    )
      .bind(paymentStatus, orderStatus, orderNumber)
      .run();

    return Response.json({ ok: true });
  } catch (error) {
    console.error("Ошибка webhook ЮKassa", error);
    return Response.json({ error: "Не удалось обработать уведомление" }, { status: 500 });
  }
}
