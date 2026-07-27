import { getTelegramCredentials } from "./credentials";

type TelegramOrder = {
  orderNumber: string;
  customerName: string;
  phone: string;
  email: string;
  address: string;
  comment: string;
  deliveryMethod: string;
  deliveryFee: number;
  subtotal: number;
  total: number;
  items: Array<{ name: string; price: number; quantity: number }>;
};

const deliveryLabels: Record<string, string> = {
  pickup: "Самовывоз в Рязани",
  courier: "Курьер по Рязани",
  cdek: "СДЭК по России",
  post: "Почта России",
};

function escapeHtml(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function money(value: number) {
  return `${new Intl.NumberFormat("ru-RU", {
    maximumFractionDigits: 0,
  }).format(value)} ₽`;
}

export async function sendTelegramOrder(order: TelegramOrder) {
  const { botToken } = await getTelegramCredentials();
  const itemLines = order.items.map(
    (item) =>
      `• ${escapeHtml(item.name)} × ${item.quantity} — ${escapeHtml(
        money(item.price * item.quantity),
      )}`,
  );
  const lines = [
    `🌿 <b>Новый заказ ${escapeHtml(order.orderNumber)}</b>`,
    "",
    ...itemLines,
    "",
    `<b>Товары:</b> ${escapeHtml(money(order.subtotal))}`,
    `<b>Доставка:</b> ${escapeHtml(money(order.deliveryFee))}`,
    `<b>Итого:</b> ${escapeHtml(money(order.total))}`,
    `<b>Получение:</b> ${escapeHtml(
      deliveryLabels[order.deliveryMethod] || order.deliveryMethod,
    )}`,
  ];
  if (order.address) lines.push(`<b>Адрес:</b> ${escapeHtml(order.address)}`);
  lines.push(
    "",
    `<b>Покупатель:</b> ${escapeHtml(order.customerName)}`,
    `<b>Телефон:</b> ${escapeHtml(order.phone)}`,
    `<b>Email:</b> ${escapeHtml(order.email)}`,
  );
  if (order.comment) {
    lines.push(`<b>Комментарий:</b> ${escapeHtml(order.comment).slice(0, 500)}`);
  }

  const response = await fetch(
    `https://api.telegram.org/bot${botToken}/sendMessage`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        chat_id: "-5430918511",
        text: lines.join("\n").slice(0, 4000),
        parse_mode: "HTML",
        disable_web_page_preview: true,
      }),
    },
  );
  const result = (await response.json()) as { ok?: boolean; description?: string };
  if (!response.ok || !result.ok) {
    throw new Error(result.description || `Telegram вернул ${response.status}`);
  }
}
