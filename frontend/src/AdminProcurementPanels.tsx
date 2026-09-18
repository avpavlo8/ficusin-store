import { useState } from "react";
import { api } from "./adminShared";
import type { ProcurementRequest, ProcurementSettings } from "./adminTypes";

export const availabilityLabel = (value: string) => ({ available: "Есть", check: "Проверить", temporarily_unavailable: "Временно нет", discontinued: "Снят с продажи", unknown: "Неизвестно" }[value] || value);

export const salesChannelLabel = (value: string) => ({ site: "Сайт", saby: "СБИС / магазин", wb: "Wildberries", ozon: "Ozon" }[value] || value);

export const salesSyncLabel = (value: string) => ({ pending: "Ожидает первой загрузки", running: "Обновляется", ok: "Актуально", error: "Ошибка", disabled: "Не подключено" }[value] || value);

export const recommendationStatusLabel = (value: string) => ({ recommended: "К заказу", already_ordered: "Уже заказано", check_availability: "Проверить наличие", supplier_unavailable: "Нет у поставщика", excluded: "Не закупаем" }[value] || value);

export const recommendationEmptyTitle = (value: string) => ({ recommended: "Дефицита нет", already_ordered: "Товаров в пути нет", check_availability: "Проверять нечего", supplier_unavailable: "У поставщиков всё есть", excluded: "Ничего не снято с закупки" }[value] || "Список пуст");

export const recommendationEmptyText = (value: string) => value === "recommended" ? "Текущий остаток и товары в пути покрывают рассчитанный спрос." : value === "already_ordered" ? "Здесь появятся позиции из действующих закупок, чтобы не заказать их повторно." : value === "check_availability" ? "Отметьте кнопкой «Проверить наличие» то, чего у поставщика может не оказаться." : value === "supplier_unavailable" ? "Растения, которых у поставщика нет, уходят сюда и не мешают собирать заказ." : "Снятое с закупки решением магазина видно здесь и в заказ не попадает.";

export const integrationChannelLabel = (value: string) => ({ saby: "СБИС / Saby", wb: "Wildberries", ozon: "Ozon" }[value] || value);

export async function updateAvailability(supplierId: number, sabyId: string, status: string, reload: () => Promise<unknown>, onError: (message: string) => void, details: {checkAfter?: string;reason?: string;comment?: string} = {}) {
  const date = status === "check" ? new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10) : "";
  try { await api("/api/v1/admin/procurement/availability", { method: "PATCH", body: JSON.stringify({ supplierId, sabyId, status, checkAfter: details.checkAfter ?? date, reason: details.reason || "", comment: details.comment || "" }) }); await reload(); }
  catch (error) { onError((error as Error).message); }
}

export async function setExclusion(sabyId: string, excluded: boolean, reason: string, reload: () => Promise<unknown>, onError: (message: string) => void) {
  const note = excluded ? reason.trim() : "";
  try { await api("/api/v1/admin/procurement/exclusions", { method: "PUT", body: JSON.stringify({ sabyId, excluded, reason: note }) }); await reload(); }
  catch (error) { onError((error as Error).message); }
}

export async function updateRequestStatus(item: ProcurementRequest, status: string, reload: () => Promise<unknown>, onError: (value: string) => void) {
  try {
    await api(`/api/v1/admin/procurement/requests/${item.id}`, { method: "PATCH", body: JSON.stringify({ sabyId: item.sabyId, requestedName: item.requestedName, quantity: item.quantity, status, notes: item.notes }) });
    await reload();
  } catch (error) { onError((error as Error).message); }
}

export function ProcurementSettingsPanel({ settings, onSaved, onError }: { settings: ProcurementSettings; onSaved: () => void; onError: (value: string) => void }) {
  const [draft, setDraft] = useState(settings); const [saving, setSaving] = useState(false);
  const number = (key: keyof ProcurementSettings, value: string) => setDraft((current) => ({ ...current, [key]: Number(value) }));
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/settings", { method: "PUT", body: JSON.stringify(draft) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <section className="admin-block procurement-block"><div className="admin-block-heading"><div><p className="eyebrow">Версия {settings.version}</p><h2>Формула цены</h2></div><button className="admin-primary" disabled={saving} onClick={save}>{saving ? "Сохраняем…" : "Сохранить новую версию"}</button></div>
    <p className="admin-hint procurement-note">Проценты вводятся как 5%, 46%, 8%. Уже рассчитанные поставки сохраняют снимок прежней версии.</p>
    <div className="procurement-settings-grid">
      <PercentField label="Возвраты" value={draft.returnLossRate} onChange={(value) => setDraft({ ...draft, returnLossRate: value })} />
      <PercentField label="Расходы маркетплейсов" value={draft.marketplaceCostRate} onChange={(value) => setDraft({ ...draft, marketplaceCostRate: value })} />
      <PercentField label="Налог" value={draft.taxRate} onChange={(value) => setDraft({ ...draft, taxRate: value })} />
      <PercentField label="Резерв" value={draft.reserveRate} onChange={(value) => setDraft({ ...draft, reserveRate: value })} />
      <label>Упаковка, ₽<input type="number" value={draft.packageRub} onChange={(event) => number("packageRub", event.target.value)} /></label>
      <label>Логистика маркетплейса, ₽ за см высоты<input type="number" step="0.5" min="0" value={draft.marketplaceLogisticsPerCm} onChange={(event) => number("marketplaceLogisticsPerCm", event.target.value)} /></label>
      <PercentField label="Менять цену при отклонении более" value={draft.priceChangeThreshold} onChange={(value) => setDraft({ ...draft, priceChangeThreshold: value })} />
      <PercentField label="Наценка на закупочную стоимость" value={draft.retailMarkupMultiplier - 1} onChange={(value) => setDraft({ ...draft, retailMarkupMultiplier: 1 + value })} />
      <PercentField label="Цена МП без скидки" value={draft.marketplaceStrikeMarkup} onChange={(value) => setDraft({ ...draft, marketplaceStrikeMarkup: value })} />
      <label>Период анализа продаж, дней<input type="number" value={draft.recommendationDays} onChange={(event) => number("recommendationDays", event.target.value)} /></label>
      <label>Закупаем запас на, дней<input type="number" value={draft.targetCoverDays} onChange={(event) => number("targetCoverDays", event.target.value)} /></label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.roundPrices} onChange={(event) => setDraft({ ...draft, roundPrices: event.target.checked })} />Округлять цены до ближайших 50 или 90</label>
    </div>
  </section>;
}

export function PercentField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label>{label}, %<input type="number" step="0.1" value={Math.round(value * 10000) / 100} onChange={(event) => onChange(Number(event.target.value) / 100)} /></label>;
}
