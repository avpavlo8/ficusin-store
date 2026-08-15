import { type ReactNode } from "react";
import type { Role } from "./adminTypes";

// Что товару разрешено брать из СБИС. Пусто значит «ничего»: карточка целиком наша.
export const sabyFieldLabels: Record<string, string> = { stock: "остаток", price: "цена", name: "название", description: "описание", photo: "фото" };

export const money = new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 });

export const roles: Array<{ value: Role; label: string }> = [
  { value: "", label: "Без доступа" }, { value: "manager", label: "Менеджер" },
];

export const roleLabel = (role: Role) => role === "owner" ? "Владелец" : roles.find((item) => item.value === role)?.label || "Клиент";

export const paymentLabels: Record<string, string> = {
  pending: "Ожидает оплаты", paid: "Оплачен", on_delivery: "При получении",
  invoice: "По счёту", cancelled: "Оплата отменена",
  payment_provider_pending: "Без онлайн-оплаты", refunded: "Возвращён",
};

export const paymentMethodLabels: Record<string, string> = {
  on_delivery: "Оплатит при получении", invoice: "Нужен счёт на организацию",
  manager_confirmation: "Оплата после подтверждения менеджером",
};

export const orderStatuses = ["new", "confirmed", "assembling", "ready", "shipped", "completed", "cancelled"];

export const statusLabels: Record<string, string> = {
  new: "Новый", confirmed: "Подтверждён", assembling: "Собирается", ready: "Готов",
  shipped: "Отправлен", completed: "Завершён", cancelled: "Отменён",
  draft: "Черновик", published: "Опубликован", archived: "В архиве",
};

export const catalogOptions = {
  catalogSection: [["plants", "Растения"], ["soil", "Грунт"], ["fertilizer", "Удобрения"], ["pots", "Кашпо и горшки"], ["accessories", "Аксессуары"]],
  plantKind: [["aglaonema", "Аглаонема"], ["alocasia", "Алоказия"], ["pineapple", "Ананас"], ["bonsai", "Бонсай"]],
  lightLevel: [["sunny", "Солнечная сторона"], ["diffused", "Яркий рассеянный свет"], ["low_light", "Затемнённое место"]],
  watering: [["frequent", "Частый"], ["moderate", "Умеренный"], ["rare", "Редкий"]],
  heightClass: [["low", "Низкий"], ["medium", "Средний"], ["high", "Высокий"]],
  careLevel: [["easy", "Почти не требует ухода"], ["medium", "Обычный уход"], ["demanding", "Капризный"]],
  placement: [["bathroom", "Ванная"], ["bedroom", "Спальня"], ["office", "Офис"], ["nursery", "Детская"]],
  petSafety: [["safe", "Безопасно для питомцев"], ["caution", "Требует осторожности"]],
  growthHabit: [["compact", "Компактный"], ["upright", "Прямостоячий"], ["trailing", "Ампельный"], ["climbing", "Вьющийся"]],
} satisfies Record<string, string[][]>;

export async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const headers = new Headers(options?.headers);
  if (!(options?.body instanceof FormData) && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const response = await fetch(path, {
    credentials: "same-origin", cache: "no-store", ...options,
    headers,
  });
  if (response.status === 401) { window.location.assign("/login?returnTo=/admin"); throw new Error("Требуется вход"); }
  if (response.status === 403) throw new Error("Недостаточно прав для этого действия");
  const text = response.status === 204 ? "" : await response.text();
  let result = {} as T & { error?: string };
  if (text) {
    try { result = JSON.parse(text) as T & { error?: string }; }
    catch { throw new Error(response.ok ? "Сервер вернул некорректный ответ" : "Не удалось выполнить операцию"); }
  }
  if (!response.ok) throw new Error(result.error || "Не удалось выполнить операцию");
  return result;
}

// Number("") is zero, so a controlled number field used to restore the zero
// immediately while the operator was trying to replace it. Selecting a zero
// on focus/click makes the next digit replace it in every admin form.
export function selectZeroNumberInput(event: React.SyntheticEvent<HTMLElement>) {
  const input = event.target;
  if (input instanceof HTMLInputElement && input.type === "number" && Number(input.value) === 0) input.select();
}

// Shares the account page's heading block so both areas read identically.
export function PageHeading({ eyebrow, title, text }: { eyebrow: string; title: string; text: string }) {
  return <div className="account-title"><div><p className="eyebrow">{eyebrow}</p><h2>{title}</h2><p className="account-title-note">{text}</p></div></div>;
}

export function ConfirmDialog({ title, text, confirmLabel, busy, danger, onCancel, onConfirm }: {
  title: string; text: ReactNode; confirmLabel: string; busy?: boolean; danger?: boolean;
  onCancel: () => void; onConfirm: () => void;
}) {
  return <><button className="admin-dialog-backdrop confirm-backdrop" aria-label="Закрыть подтверждение" onClick={onCancel} disabled={busy} />
    <div className="admin-dialog confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-text">
      <header><h2 id="confirm-dialog-title">{title}</h2><button onClick={onCancel} aria-label="Закрыть" disabled={busy}>×</button></header>
      <p id="confirm-dialog-text">{text}</p>
      <div className="dialog-actions"><button onClick={onCancel} disabled={busy}>Отмена</button><button className={danger ? "danger-primary" : "primary"} onClick={onConfirm} disabled={busy}>{confirmLabel}</button></div>
    </div></>;
}

export function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><section className="admin-dialog" role="dialog" aria-modal="true"><header><h2>{title}</h2><button onClick={onClose}>×</button></header>{children}</section></>;
}
