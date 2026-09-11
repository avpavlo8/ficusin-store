import type { AdminData, Section } from "./adminTypes";
export const sections: Array<{ id: Section; label: string; permission?: string; owner?: boolean }> = [
  { id: "dashboard", label: "Обзор" },
  { id: "products", label: "Каталог", permission: "products.read" },
  { id: "categories", label: "Категории", permission: "products.read" },
  { id: "collections", label: "Подборки", permission: "products.read" },
  { id: "orders", label: "Заказы", permission: "orders.read" },
  { id: "procurement", label: "Закупки", permission: "procurement.read" },
  { id: "returns", label: "Возвраты", permission: "returns.read" },
  { id: "marketplaces", label: "Маркетплейсы", permission: "procurement.read" },
  { id: "finance", label: "Финансы", owner: true },
  { id: "analytics", label: "Аналитика", permission: "analytics.read" },
  { id: "customers", label: "Клиенты", permission: "customers.read" },
  { id: "settings", label: "Настройки", owner: true },
];
export function currentAdminSection(): Section {
  const url = new URL(window.location.href);
  const value = url.searchParams.get("section") || url.searchParams.get("tab") || url.pathname.split("/")[2] || url.hash.replace(/^#/, "");
  const alias = value === "catalog" ? "products" : value;
  return (sections.find(item => item.id === alias)?.id || "dashboard");
}
export function canOpenSection(data: AdminData, section: Section): boolean {
  const item = sections.find(item => item.id === section);
  return !!item && (!item.permission || data.permissions.includes(item.permission)) && (!item.owner || data.role === "owner");
}
