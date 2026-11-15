import { useState } from "react";
import { api } from "./adminShared";
import { WorkspaceState } from "./WorkspaceUI";
export type FlowwowRemote = {
  productId: number;
  offerId: string;
  name: string;
  price: string;
  stock: number;
  type: number;
};
type Candidate = {
  variantId: number;
  productId: number;
  name: string;
  label: string;
  sku: string;
  priceMinor: number;
  stock: number;
};
type Preview = {
  site: {
    variantId: number;
    name: string;
    description: string;
    sku: string;
    priceMinor: number;
    images: string[];
  };
  remote: FlowwowRemote;
  stock: number;
  fingerprint: string;
  executionMode: string;
  shopId: string;
};
export function FlowwowBinding({
  remote,
  onSaved,
  onCancel,
}: {
  remote: FlowwowRemote;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<Candidate[] | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const search = async () => {
    setBusy(true);
    setError("");
    setPreview(null);
    try {
      const data = await api<{ items: Candidate[] }>(
        `/api/v1/admin/flowwow/candidates?q=${encodeURIComponent(query)}`,
      );
      setCandidates(data.items);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const check = async (variantId: number) => {
    setBusy(true);
    setError("");
    setPreview(null);
    try {
      setPreview(
        await api<Preview>("/api/v1/admin/flowwow/links/preview", {
          method: "POST",
          body: JSON.stringify({ variantId, remoteId: remote.productId }),
        }),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const confirm = async () => {
    if (!preview) return;
    setBusy(true);
    setError("");
    try {
      await api("/api/v1/admin/flowwow/links", {
        method: "POST",
        body: JSON.stringify({
          variantId: preview.site.variantId,
          remoteId: remote.productId,
          fingerprint: preview.fingerprint,
        }),
      });
      onSaved();
    } catch (e) {
      setError((e as Error).message);
      setPreview(null);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section aria-label="Привязка карточки Flowwow">
      <h3>
        Связать «{remote.name}» · ID {remote.productId}
      </h3>
      <p>
        Выберите конкретный размер товара сайта. После подтверждения название, описание, первые 6
        фото, цена и остаток будут обновляться с сайта автоматически.
      </p>
      <form
        className="admin-toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          void search();
        }}
      >
        <label>
          Название или артикул сайта
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            maxLength={128}
          />
        </label>
        <button type="submit" className="admin-action" disabled={busy}>
          Найти вариант
        </button>
        <button type="button" className="admin-action" onClick={onCancel} disabled={busy}>
          Отменить привязку
        </button>
      </form>
      {error && <WorkspaceState kind="error" title={error} />}
      {busy && <WorkspaceState kind="loading" title="Проверяем связь…" />}
      {!preview &&
        candidates?.map((i) => (
          <p key={i.variantId}>
            <button
              type="button"
              className="admin-action"
              disabled={busy}
              onClick={() => void check(i.variantId)}
            >
              Проверить связь: {i.name} · {i.label} · SKU {i.sku}
            </button>
          </p>
        ))}
      {!preview && candidates?.length === 0 && (
        <WorkspaceState
          kind="empty"
          title="Свободных вариантов по запросу нет"
          detail="Уточните название или артикул. Уже связанные и архивные варианты не показываются."
        />
      )}
      {preview && (
        <section aria-label="Проверка перед привязкой">
          <h3>Данные для отправки</h3>
          <p>
            {preview.executionMode === "mock"
              ? "СТГ: отправка только в тестовую карточку. Реальный магазин не меняется."
              : `Магазин Flowwow ${preview.shopId}. Данные сайта заменят управляемые поля карточки.`}
          </p>
          <p>
            <strong>{preview.site.name}</strong> · SKU {preview.site.sku} → ID{" "}
            {preview.remote.productId} · Артикул Flowwow{" "}
            {preview.remote.offerId || preview.site.sku}
          </p>
          <p>{preview.site.description || "Описание на сайте не заполнено"}</p>
          <p>
            Фото: {Math.min(6, preview.site.images.length)} · Цена:{" "}
            {(preview.site.priceMinor / 100).toLocaleString("ru-RU")} ₽ · Остаток: {preview.stock}{" "}
            шт.
          </p>
          <button
            type="button"
            className="admin-action"
            disabled={busy}
            onClick={() => void confirm()}
          >
            Подтвердить связь и включить обмен
          </button>{" "}
          <button
            type="button"
            className="admin-action"
            disabled={busy}
            onClick={() => setPreview(null)}
          >
            Выбрать другой вариант
          </button>
        </section>
      )}
    </section>
  );
}
