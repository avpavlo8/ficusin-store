import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

export function useProcurementError(report: (message: string) => void) {
  const [error, setError] = useState("");
  const errorRef = useRef<HTMLDivElement>(null);
  const notify = useCallback(
    (message: string) => {
      setError(message);
      report(message);
      window.requestAnimationFrame(() => {
        errorRef.current?.focus();
        errorRef.current?.scrollIntoView({ block: "nearest" });
      });
    },
    [report],
  );
  return { error, errorRef, notify, clear: () => setError("") };
}

// A modal owns keyboard focus until it closes. Keep the latest callback without
// refocusing the form on each keystroke.
export function useProcurementDialog(onClose: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  }, [onClose]);
  useEffect(() => {
    const panel = ref.current;
    const previous = document.activeElement as HTMLElement | null;
    if (!panel) return;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    panel.tabIndex = -1;
    panel.focus();
    const key = (event: KeyboardEvent) => {
      if (
        document
          .querySelectorAll('[aria-modal="true"]')
          .item(document.querySelectorAll('[aria-modal="true"]').length - 1) !== panel
      )
        return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        close.current();
      }
      if (event.key !== "Tab") return;
      // Closed details can report geometry for controls that cannot receive focus.
      const elements = [
        ...panel.querySelectorAll<HTMLElement>(
          'summary,a[href],button:not(:disabled):not([tabindex="-1"]),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]',
        ),
      ].filter(
        (item) =>
          item.getClientRects().length &&
          !item.matches(
            "details:not([open]) > :not(summary), details:not([open]) > :not(summary) *",
          ),
      );
      const first = elements[0];
      const last = elements.at(-1);
      if (!first) {
        event.preventDefault();
        return;
      }
      if (
        event.shiftKey &&
        (document.activeElement === first || document.activeElement === panel)
      ) {
        event.preventDefault();
        last?.focus();
      } else if (
        !event.shiftKey &&
        (document.activeElement === last || !panel.contains(document.activeElement))
      ) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("keydown", key, true);
      document.body.style.overflow = overflow;
      previous?.focus();
    };
  }, []);
  return ref;
}

export async function validateProcurementPDF(file: File) {
  if (
    !/\.pdf$/i.test(file.name) ||
    (file.type && !["application/pdf", "application/octet-stream"].includes(file.type))
  )
    throw new Error("Выберите PDF-файл поставщика.");
  if (file.size > 20 * 1024 * 1024) throw new Error("PDF должен быть не больше 20 МБ.");
  if (new TextDecoder().decode(await file.slice(0, 5).arrayBuffer()) !== "%PDF-")
    throw new Error("Файл не является PDF или повреждён.");
}

export async function uploadProcurementPDF<T>(body: FormData): Promise<T> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 60000);
  try {
    const response = await fetch("/api/v1/admin/procurement/documents", {
      method: "POST",
      credentials: "same-origin",
      body,
      signal: controller.signal,
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Не удалось разобрать PDF.");
    return result as T;
  } catch (error) {
    if ((error as Error).name === "AbortError")
      throw new Error(
        "Ответ не получен за минуту. Документ мог сохраниться: обновите список документов перед повторной загрузкой.",
      );
    throw error;
  } finally {
    window.clearTimeout(timer);
  }
}

export const procurementOrderHref = (id: number) =>
  `/admin?section=procurement&view=orders&order=${id}`;
export const quantityLabel = (value: number) =>
  `${value} ${value % 10 === 1 && value % 100 !== 11 ? "позиция" : value % 10 >= 2 && value % 10 <= 4 && (value % 100 < 12 || value % 100 > 14) ? "позиции" : "позиций"}`;

export const receivingModeLabel = (mode?: string) =>
  mode === "awaiting_catalog"
    ? "Ожидает карточку сайта"
    : mode === "assortment"
      ? "Разобрать после приезда"
      : mode === "customer_handoff"
        ? "Под заказ · передать клиенту"
        : "Обычная приёмка";

// Keep editing and scrolling together when the plan inserts or reorders rows.
export function useProcurementLineNavigation(
  items: readonly unknown[],
  sortOrder: readonly string[],
) {
  const lineFirstFields = useRef(new Map<string, HTMLInputElement>());
  const pendingNewLine = useRef<string | null>(null);
  useLayoutEffect(() => {
    const id = pendingNewLine.current;
    if (!id) return;
    const field = lineFirstFields.current.get(id);
    if (!field) return;
    pendingNewLine.current = null;
    field.focus({ preventScroll: true });
    field.scrollIntoView({ block: "center", inline: "nearest" });
  }, [items]);
  const rowScrollAnchor = useRef<{ row: HTMLElement; top: number } | null>(null);
  useLayoutEffect(() => {
    const anchor = rowScrollAnchor.current;
    rowScrollAnchor.current = null;
    if (!anchor?.row.isConnected) return;
    const offset = anchor.row.getBoundingClientRect().top - anchor.top;
    if (Math.abs(offset) < 1) return;
    // Keep the row at its previous screen position, including in the mobile
    // dialog where the dialog itself scrolls instead of the table wrapper.
    for (let parent = anchor.row.parentElement; parent; parent = parent.parentElement) {
      if (
        parent.scrollHeight > parent.clientHeight &&
        /auto|scroll|overlay/.test(window.getComputedStyle(parent).overflowY)
      ) {
        parent.scrollTop += offset;
        break;
      }
    }
  }, [sortOrder]);
  const setFirstField = (id: string, field: HTMLInputElement | null) => {
    if (field) lineFirstFields.current.set(id, field);
    else lineFirstFields.current.delete(id);
  };
  const rememberRow = useCallback((editedRow?: HTMLElement) => {
    // Focus has moved by now. Follow the newly focused row when editing
    // continues in the table; otherwise follow the row that was edited.
    const activeRow = document.activeElement?.closest<HTMLElement>(
      ".procurement-plan-table tbody tr",
    );
    const row = activeRow || editedRow;
    if (row?.isConnected) rowScrollAnchor.current = { row, top: row.getBoundingClientRect().top };
  }, []);
  return { setFirstField, pendingNewLine, rememberRow };
}
