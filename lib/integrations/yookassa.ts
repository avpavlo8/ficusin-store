import type { IntegrationEnv } from "./types";
import { requireSetting } from "./types";

const API_URL = "https://api.yookassa.ru/v3";

export type YooKassaPayment = {
  id: string;
  status: "pending" | "waiting_for_capture" | "succeeded" | "canceled";
  paid: boolean;
  confirmation?: { type: string; confirmation_url?: string };
  metadata?: Record<string, string>;
};

function authHeader(env: IntegrationEnv) {
  const shopId = requireSetting(env, "YOOKASSA_SHOP_ID");
  const secretKey = requireSetting(env, "YOOKASSA_SECRET_KEY");
  return `Basic ${btoa(`${shopId}:${secretKey}`)}`;
}

export function isYooKassaConfigured(env: IntegrationEnv) {
  return Boolean(env.YOOKASSA_SHOP_ID && env.YOOKASSA_SECRET_KEY);
}

export async function createPayment(
  env: IntegrationEnv,
  input: {
    orderNumber: string;
    amount: number;
    description: string;
    returnUrl: string;
    email: string;
    items: Array<{ description: string; quantity: number; unitPrice: number }>;
    delivery?: { description: string; price: number };
  },
): Promise<YooKassaPayment> {
  const vatCode = Math.max(1, Math.min(6, Number(env.YOOKASSA_VAT_CODE ?? 1)));
  const receiptItems = input.items.map((item) => ({
    description: item.description.slice(0, 128),
    quantity: item.quantity.toFixed(2),
    amount: {
      value: item.unitPrice.toFixed(2),
      currency: "RUB",
    },
    vat_code: vatCode,
    payment_mode: "full_payment",
    payment_subject: "commodity",
  }));
  if (input.delivery && input.delivery.price > 0) {
    receiptItems.push({
      description: input.delivery.description.slice(0, 128),
      quantity: "1.00",
      amount: {
        value: input.delivery.price.toFixed(2),
        currency: "RUB",
      },
      vat_code: vatCode,
      payment_mode: "full_payment",
      payment_subject: "service",
    });
  }

  const response = await fetch(`${API_URL}/payments`, {
    method: "POST",
    headers: {
      Authorization: authHeader(env),
      "Content-Type": "application/json",
      "Idempotence-Key": input.orderNumber,
    },
    body: JSON.stringify({
      amount: {
        value: input.amount.toFixed(2),
        currency: "RUB",
      },
      capture: true,
      confirmation: {
        type: "redirect",
        return_url: input.returnUrl,
      },
      description: input.description.slice(0, 128),
      metadata: {
        order_number: input.orderNumber,
      },
      receipt: {
        customer: { email: input.email },
        items: receiptItems,
      },
    }),
  });

  if (!response.ok) {
    const body = await response.text();
    throw new Error(`ЮKassa не создала платёж: ${response.status} ${body}`);
  }

  return (await response.json()) as YooKassaPayment;
}

export async function getPayment(
  env: IntegrationEnv,
  paymentId: string,
): Promise<YooKassaPayment> {
  const response = await fetch(`${API_URL}/payments/${paymentId}`, {
    headers: { Authorization: authHeader(env) },
  });
  if (!response.ok) {
    throw new Error(`Не удалось проверить платёж ЮKassa: ${response.status}`);
  }
  return (await response.json()) as YooKassaPayment;
}
