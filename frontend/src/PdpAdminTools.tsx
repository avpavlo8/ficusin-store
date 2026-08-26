import { useState } from "react";
import { ProductDialog } from "./AdminCatalogDialogs";
import { Dialog, api } from "./adminShared";
import { ProductMediaManager } from "./ProductMediaManager";
import type { Product } from "./adminTypes";

function ProductMediaDialog({ product, onClose, onChanged }: {
  product: Product; onClose: () => void; onChanged: () => void;
}) {
  const [error, setError] = useState("");
  return <Dialog title={`Фотографии · ${product.name}`} onClose={onClose}>
    {error && <p className="admin-error">{error}</p>}
    <ProductMediaManager productId={product.id} onError={setError} onChanged={onChanged} />
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
