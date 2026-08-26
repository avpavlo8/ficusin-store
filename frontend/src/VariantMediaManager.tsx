import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";

export type VariantMedia = { id: number; url: string; primary: boolean; sortOrder: number };

export function VariantMediaManager({ variantId, sku, onChanged, onError }: {
  variantId: number; sku: string; onChanged: () => void; onError: (message: string) => void;
}) {
  const [items, setItems] = useState<VariantMedia[]>([]);
  const [busy, setBusy] = useState(false);
  const load = useCallback(() => api<{ media?: VariantMedia[] }>(`/api/v1/admin/variants/${variantId}/media`)
    .then((data) => setItems(data.media || []))
    .catch((error) => onError((error as Error).message)), [variantId, onError]);

  useEffect(() => { void load(); }, [load]);

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    try {
      for (const file of Array.from(files)) {
        const form = new FormData();
        form.append("file", file);
        await api(`/api/v1/admin/variants/${variantId}/media`, { method: "POST", body: form });
      }
      await load();
      onChanged();
    } catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };

  const remove = async (item: VariantMedia) => {
    const message=item.primary?`Удалить основное фото SKU ${sku}? Следующее фото станет основным; если его нет, SKU останется без собственного фото.`:`Удалить фотографию SKU ${sku}?`;
    if (!window.confirm(message)) return;
    setBusy(true);
    try {
      await api(`/api/v1/admin/variants/${variantId}/media/${item.id}`, { method: "DELETE" });
      await load();
      onChanged();
    } catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };

  const makePrimary = async (item: VariantMedia) => {
    if (item.primary) return;
    setBusy(true);
    try {
      await api(`/api/v1/admin/variants/${variantId}/media/${item.id}/primary`, { method: "PATCH", body: "{}" });
      await load();
      onChanged();
    } catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };

  return <div className="wide variant-media-manager">
    <label className="pdp-admin-upload">
      <input type="file" accept="image/jpeg,image/png,image/gif" multiple disabled={busy} onChange={(event) => { void upload(event.target.files); event.currentTarget.value = ""; }} />
      <span>{busy ? "Загружаем…" : "+ Добавить фото SKU"}</span>
    </label>
    <p className="admin-hint">Фото SKU имеют приоритет на PDP. Если их нет, используется общая галерея PRODUCT.</p>
    <div className="variant-media-grid">
      {items.map((item) => <article key={item.id} className={item.primary ? "primary" : ""}>
        <img src={item.url} alt={`SKU ${sku}`} />
        <div>
          {item.primary ? <strong>Главная</strong> : <button type="button" disabled={busy} onClick={() => void makePrimary(item)}>Сделать главной</button>}
          <button type="button" className="danger" disabled={busy} onClick={() => void remove(item)}>Удалить</button>
        </div>
      </article>)}
      {!items.length && <p className="admin-inline-error">Статус SKU: без собственного фото. Будет использована общая галерея товара.</p>}
    </div>
  </div>;
}
