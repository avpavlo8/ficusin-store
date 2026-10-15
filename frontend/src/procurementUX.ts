import { useCallback, useEffect, useRef, useState } from "react";

export function useProcurementError(report: (message: string) => void) {
  const [error, setError] = useState("");
  const errorRef = useRef<HTMLDivElement>(null);
  const notify = useCallback((message: string) => {
    setError(message);
    report(message);
    window.requestAnimationFrame(() => {
      errorRef.current?.focus();
      errorRef.current?.scrollIntoView({ block: "nearest" });
    });
  }, [report]);
  return { error, errorRef, notify, clear: () => setError("") };
}

// A modal owns keyboard focus until it closes. Keep the latest callback without
// refocusing the form on each keystroke.
export function useProcurementDialog(onClose: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => { close.current = onClose; }, [onClose]);
  useEffect(() => {
    const panel = ref.current;
    const previous = document.activeElement as HTMLElement | null;
    if (!panel) return;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    panel.tabIndex = -1;
    panel.focus();
    const key = (event: KeyboardEvent) => {
      if (document.querySelectorAll('[aria-modal="true"]').item(document.querySelectorAll('[aria-modal="true"]').length - 1) !== panel) return;
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close.current(); }
      if (event.key !== "Tab") return;
      const elements = [...panel.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]')].filter((item) => item.getClientRects().length);
      const first = elements[0]; const last = elements.at(-1);
      if (!first) { event.preventDefault(); return; }
      if (event.shiftKey && (document.activeElement === first || document.activeElement === panel)) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || !panel.contains(document.activeElement))) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", key, true);
    return () => { document.removeEventListener("keydown", key, true); document.body.style.overflow = overflow; previous?.focus(); };
  }, []);
  return ref;
}

export async function validateProcurementPDF(file: File) {
  if (!/\.pdf$/i.test(file.name) || (file.type && !["application/pdf", "application/octet-stream"].includes(file.type))) throw new Error("Выберите PDF-файл поставщика.");
  if (file.size > 20 * 1024 * 1024) throw new Error("PDF должен быть не больше 20 МБ.");
  if (new TextDecoder().decode(await file.slice(0, 5).arrayBuffer()) !== "%PDF-") throw new Error("Файл не является PDF или повреждён.");
}

export async function uploadProcurementPDF<T>(body: FormData): Promise<T> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 60000);
  try {
    const response = await fetch("/api/v1/admin/procurement/documents", { method: "POST", credentials: "same-origin", body, signal: controller.signal });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Не удалось разобрать PDF.");
    return result as T;
  } catch (error) {
    if ((error as Error).name === "AbortError") throw new Error("Ответ не получен за минуту. Документ мог сохраниться: обновите список документов перед повторной загрузкой.");
    throw error;
  } finally { window.clearTimeout(timer); }
}

export const procurementOrderHref = (id: number) => `/admin?section=procurement&view=orders&order=${id}`;
export const quantityLabel = (value: number) => `${value} ${value % 10 === 1 && value % 100 !== 11 ? "позиция" : value % 10 >= 2 && value % 10 <= 4 && (value % 100 < 12 || value % 100 > 14) ? "позиции" : "позиций"}`;

export const receivingModeLabel = (mode?: string) => mode === "awaiting_catalog" ? "Ожидает карточку СБИС" : mode === "assortment" ? "Разобрать после приезда" : mode === "customer_handoff" ? "Под заказ · передать клиенту" : "Обычная приёмка";
