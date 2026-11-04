import type { AdminData, Section } from "./adminTypes";
// Группы повторяют то, как сотрудник думает о работе, а не устройство
// системы: «что горит сегодня», «растения», «каналы продаж», «люди».
// Порядок внутри группы — по частоте использования.
export type SectionGroup = "today" | "plants" | "channels" | "people";
export const sectionGroups: Array<{ id: SectionGroup; label: string }> = [
  { id: "today", label: "Сегодня" },
  { id: "plants", label: "Растения" },
  { id: "channels", label: "Каналы" },
  { id: "people", label: "Люди и цифры" },
];
export const sections: Array<{
  id: Section;
  label: string;
  group: SectionGroup;
  permission?: string;
  owner?: boolean;
}> = [
  { id: "dashboard", label: "Обзор", group: "today" },
  { id: "orders", label: "Заказы", group: "today", permission: "orders.read" },
  { id: "chat", label: "Чат", group: "today", permission: "chat.manage" },
  { id: "products", label: "Каталог", group: "plants", permission: "products.read" },
  { id: "categories", label: "Категории", group: "plants", permission: "products.read" },
  { id: "collections", label: "Подборки", group: "plants", permission: "products.read" },
  { id: "procurement", label: "Закупки", group: "plants", permission: "procurement.read" },
  { id: "returns", label: "Возвраты", group: "plants", permission: "returns.read" },
  { id: "marketplaces", label: "Маркетплейсы", group: "channels", permission: "integrations.edit" },
  { id: "avito", label: "Авито", group: "channels", permission: "integrations.edit" },
  { id: "customers", label: "Клиенты", group: "people", permission: "customers.read" },
  { id: "analytics", label: "Аналитика", group: "people", permission: "analytics.read" },
  { id: "settings", label: "Настройки", group: "people", owner: true },
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
  return sections.find((item) => item.id === alias)?.id || "dashboard";
}
export function canOpenSection(data: AdminData, section: Section): boolean {
  const item = sections.find((item) => item.id === section);
  return (
    !!item &&
    (!item.permission || data.permissions.includes(item.permission)) &&
    (!item.owner || data.role === "owner")
  );
}
