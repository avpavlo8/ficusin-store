import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";

export type ProductMedia = { id: number; url: string; primary: boolean; sortOrder: number };

export function ProductMediaManager({ productId, onChanged, onError }: {
  productId: number; onChanged?: (primaryUrl: string) => void; onError: (message: string) => void;
}) {
  const [items, setItems] = useState<ProductMedia[]>([]);
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => {
    try {
      const data = await api<{ media?: ProductMedia[] }>(`/api/v1/admin/products/${productId}/media`);
      const next = [...(data.media || [])].sort((left, right) => Number(right.primary) - Number(left.primary) || left.sortOrder - right.sortOrder || left.id - right.id);
      setItems(next); return next;
    } catch (error) { onError((error as Error).message); }
  }, [productId, onError]);
  useEffect(() => { void load(); }, [load]);

  const upload = async (files: FileList | null) => {
    if (!files?.length || busy) return;
    setBusy(true);
    try {
      for (const file of Array.from(files)) { const form = new FormData(); form.append("file", file); await api(`/api/v1/admin/products/${productId}/media`, { method: "POST", body: form }); }
      const next=await load(); onChanged?.(next?.find((item) => item.primary)?.url || "");
    } catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };
  const remove = async (item: ProductMedia) => {
    const message = item.primary ? "Это основное фото. После удаления главным станет следующее по порядку; если других фото нет, карточка получит статус «без фото». Удалить?" : "Удалить эту фотографию?";
    if (!window.confirm(message)) return;
    setBusy(true);
    try { await api(`/api/v1/admin/products/${productId}/media/${item.id}`, { method: "DELETE" }); const next=await load();onChanged?.(next?.find((entry)=>entry.primary)?.url||""); }
    catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };
  const makePrimary = async (item: ProductMedia) => {
    if (item.primary || busy || !window.confirm("Сделать эту фотографию основной?")) return;
    setBusy(true);
    try { await api(`/api/v1/admin/products/${productId}/media/${item.id}/primary`, { method: "PATCH", body: "{}" }); const next=await load();onChanged?.(next?.find((entry)=>entry.primary)?.url||""); }
    catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };

  return <div className="pdp-admin-media">
    <label className="pdp-admin-upload"><input type="file" accept="image/jpeg,image/png,image/gif" multiple disabled={busy} onChange={(event) => { void upload(event.target.files); event.currentTarget.value = ""; }} /><span>{busy ? "Обрабатываем…" : "+ Загрузить обычные фотографии"}</span></label>
    <p className="admin-hint">Основное фото отмечено явно. Порядок стабилен; фото SKU редактируются отдельно внутри варианта.</p>
    <div className="pdp-admin-media-grid">
      {items.map((item) => <article key={item.id} className={item.primary ? "primary" : ""}><img src={item.url} alt=""/><div>{item.primary ? <strong>Главная · Основное фото</strong> : <button type="button" disabled={busy} onClick={() => void makePrimary(item)}>Сделать основным</button>}<button type="button" className="danger" disabled={busy} onClick={() => void remove(item)}>Удалить</button></div></article>)}
      {!items.length && <p className="admin-inline-error">Без фото — перед публикацией загрузите и выберите основное изображение.</p>}
    </div>
  </div>;
}
