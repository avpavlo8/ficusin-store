import type { IntegrationEnv } from "./types";

type TelegramOrder = {
  orderNumber: string;
  customerName: string;
  phone: string;
  email: string;
  address: string;
  deliveryLabel: string;
  total: number;
  items: Array<{ name: string; quantity: number; price: number }>;
};

const escapeHtml = (value: string) =>
  value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
  }).format(value);

export async function notifyNewOrder(
  env: IntegrationEnv,
  order: TelegramOrder,
): Promise<"sent" | "not_configured"> {
  const token = env.TELEGRAM_BOT_TOKEN?.trim();
  const chatId = env.TELEGRAM_ORDER_CHAT_ID?.trim();
  if (!token || !chatId) return "not_configured";

  const itemLines = order.items
    .map(
      (item) =>
        `• ${escapeHtml(item.name)} × ${item.quantity} — ${escapeHtml(
          money(item.price * item.quantity),
        )}`,
    )
    .join("\n");

  const text = [
    `🌿 <b>Новый заказ ${escapeHtml(order.orderNumber)}</b>`,
    "",
    itemLines,
    "",
    `<b>Итого:</b> ${escapeHtml(money(order.total))}`,
    `<b>Получение:</b> ${escapeHtml(order.deliveryLabel)}`,
    order.address ? `<b>Адрес:</b> ${escapeHtml(order.address)}` : "",
    "",
    `<b>Покупатель:</b> ${escapeHtml(order.customerName)}`,
    `<b>Телефон:</b> ${escapeHtml(order.phone)}`,
    `<b>Email:</b> ${escapeHtml(order.email)}`,
  ]
    .filter(Boolean)
    .join("\n");

  const response = await fetch(
    `https://api.telegram.org/bot${token}/sendMessage`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        chat_id: chatId,
        text,
        parse_mode: "HTML",
        disable_web_page_preview: true,
      }),
    },
  );

  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Telegram не принял уведомление: ${response.status} ${body}`);
  }

  return "sent";
}
