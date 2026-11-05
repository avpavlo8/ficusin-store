import type { AdminData, Section } from "./adminTypes";
import { isStandalone } from "./lib/pwa";
export const sections: Array<{
  id: Section;
  label: string;
  permission?: string;
  owner?: boolean;
}> = [
  { id: "dashboard", label: "Обзор" },
  { id: "orders", label: "Заказы", permission: "orders.read" },
  { id: "chat", label: "Чат", permission: "chat.manage" },
  { id: "products", label: "Каталог", permission: "products.read" },
  { id: "categories", label: "Категории", permission: "products.read" },
  { id: "collections", label: "Подборки", permission: "products.read" },
  { id: "procurement", label: "Закупки", permission: "procurement.read" },
  { id: "returns", label: "Возвраты", permission: "returns.read" },
  { id: "marketplaces", label: "Маркетплейсы", permission: "integrations.edit" },
  { id: "avito", label: "Авито", permission: "integrations.edit" },
  { id: "customers", label: "Клиенты", permission: "customers.read" },
  { id: "analytics", label: "Аналитика", permission: "analytics.read" },
  { id: "settings", label: "Настройки", owner: true },
];
// На телефоне внизу помещается четыре кнопки и «Ещё». Сюда попадают
// разделы, с которыми сотрудник работает руками каждый день.
export const mobilePrimarySections: Section[] = ["orders", "chat", "products", "returns"];
export function currentAdminSection(): Section {
  const url = new URL(window.location.href);
  const value =
    url.searchParams.get("section") ||
    url.searchParams.get("tab") ||
    url.pathname.split("/")[2] ||
    url.hash.replace(/^#/, "");
  const alias = value === "catalog" ? "products" : value;
  return (
    sections.find((item) => item.id === alias)?.id || (isStandalone() ? "orders" : "dashboard")
  );
}
export function canOpenSection(data: AdminData, section: Section): boolean {
  const item = sections.find((item) => item.id === section);
  return (
    !!item &&
    (!item.permission || data.permissions.includes(item.permission)) &&
    (!item.owner || data.role === "owner")
  );
}
