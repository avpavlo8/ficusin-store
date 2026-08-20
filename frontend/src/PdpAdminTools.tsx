import { useCallback, useEffect, useState } from "react";
import { ProductDialog } from "./AdminCatalogDialogs";
import { Dialog, api } from "./adminShared";
import type { Product } from "./adminTypes";

export type ProductMedia = { id: number; url: string; primary: boolean; sortOrder: number };

function ProductMediaDialog({ product, onClose, onChanged }: {
  product: Product; onClose: () => void; onChanged: () => void;
}) {
  const [items, setItems] = useState<ProductMedia[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(() => api<{ media: ProductMedia[] }>(`/api/v1/admin/products/${product.id}/media`)
    .then((data) => setItems(data.media || []))
    .catch((reason) => setError((reason as Error).message)), [product.id]);
  useEffect(() => { void load(); }, [load]);

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true); setError("");
    try {
      for (const file of Array.from(files)) {
        const form = new FormData();
        form.append("file", file);
        await api(`/api/v1/admin/products/${product.id}/media`, { method: "POST", body: form });
      }
      await load(); onChanged();
    } catch (reason) { setError((reason as Error).message); }
    finally { setBusy(false); }
  };

  const remove = async (item: ProductMedia) => {
    if (!window.confirm("Удалить эту фотографию из карточки товара?")) return;
    setBusy(true); setError("");
    try {
      await api(`/api/v1/admin/products/${product.id}/media/${item.id}`, { method: "DELETE" });
      await load(); onChanged();
    } catch (reason) { setError((reason as Error).message); }
    finally { setBusy(false); }
  };

  const makePrimary = async (item: ProductMedia) => {
    if (item.primary) return;
    setBusy(true); setError("");
    try {
      await api(`/api/v1/admin/products/${product.id}/media/${item.id}/primary`, { method: "PATCH", body: "{}" });
      await load(); onChanged();
    } catch (reason) { setError((reason as Error).message); }
    finally { setBusy(false); }
  };

  return <Dialog title={`Фотографии · ${product.name}`} onClose={onClose}>
    <div className="pdp-admin-media">
      <label className="pdp-admin-upload">
        <input type="file" accept="image/jpeg,image/png,image/gif" multiple disabled={busy} onChange={(event) => { void upload(event.target.files); event.currentTarget.value = ""; }} />
        <span>{busy ? "Загружаем…" : "+ Добавить фотографии"}</span>
      </label>
      <p className="admin-hint">JPEG, PNG или GIF до 12 МБ. Файл сохраняется в нашем S3, а не в СБИС.</p>
      {error && <p className="admin-error">{error}</p>}
      <div className="pdp-admin-media-grid">
        {items.map((item) => <article key={item.id} className={item.primary ? "primary" : ""}>
          <img src={item.url} alt="" />
          <div>
            {item.primary ? <strong>Главная</strong> : <button type="button" disabled={busy} onClick={() => void makePrimary(item)}>Сделать главной</button>}
            <button type="button" className="danger" disabled={busy} onClick={() => void remove(item)}>Удалить</button>
          </div>
        </article>)}
        {!items.length && <p className="admin-hint">У товара пока нет собственных фотографий.</p>}
      </div>
    </div>
  </Dialog>;
}

export function PdpAdminTools({ slug, adminRole, onChanged }: {
  slug: string; adminRole?: string; onChanged: () => void;
}) {
  const allowed = adminRole === "owner" || adminRole === "manager";
  const [product, setProduct] = useState<Product | null>(null);
  const [mode, setMode] = useState<"edit" | "media" | "">("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const open = async (next: "edit" | "media") => {
    if (!allowed) return;
    setLoading(true); setError("");
    try {
      const data = await api<{ products: Product[] }>("/api/v1/admin/products");
      const found = data.products.find((item) => item.slug === slug);
      if (!found) throw new Error("Карточка не найдена в редакторе");
      setProduct(found); setMode(next);
    } catch (reason) { setError((reason as Error).message); }
    finally { setLoading(false); }
  };

  if (!allowed) return null;
  return <>
    <div className="pdp-admin-toolbar" aria-label="Управление товаром">
      <span>Режим администратора</span>
      <button type="button" disabled={loading} onClick={() => void open("edit")}>Редактировать карточку</button>
      <button type="button" disabled={loading} onClick={() => void open("media")}>Фотографии</button>
      {error && <small>{error}</small>}
    </div>
    {product && mode === "edit" && <ProductDialog
      product={product}
      onClose={() => setMode("")}
      onError={setError}
      onSaved={(saved) => { setProduct(saved); setMode(""); onChanged(); }}
    />}
    {product && mode === "media" && <ProductMediaDialog
      product={product}
      onClose={() => setMode("")}
      onChanged={onChanged}
    />}
  </>;
}
