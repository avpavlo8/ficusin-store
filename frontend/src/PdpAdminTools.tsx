import { lazy, Suspense, useState } from "react";
import { Dialog, api } from "./adminShared";
import type { Product } from "./adminTypes";

const ProductMediaDialog = lazy(() => import("./PdpProductMediaDialog"));

export function PdpAdminTools({
  slug,
  adminRole,
  onChanged,
}: {
  slug: string;
  adminRole?: string;
  onChanged: () => void;
}) {
  const allowed = adminRole === "owner" || adminRole === "administrator" || adminRole === "manager";
  const [product, setProduct] = useState<Product | null>(null);
  const [mode, setMode] = useState<"media" | "">("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const open = async (next: "edit" | "media") => {
    if (!allowed) return;
    setLoading(true);
    setError("");
    try {
      const data = await api<{ products: Product[] }>("/api/v1/admin/products");
      const found = data.products.find((item) => (item.productCode || item.slug) === slug);
      if (!found) throw new Error("Карточка не найдена в редакторе");
      if (next === "edit") {
        window.location.assign(`/admin?section=products&product=${found.id}`);
        return;
      }
      setProduct(found);
      setMode("media");
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setLoading(false);
    }
  };

  if (!allowed) return null;
  return (
    <>
      <div className="pdp-admin-toolbar" aria-label="Управление товаром">
        <span>Режим администратора</span>
        <button type="button" disabled={loading} onClick={() => void open("edit")}>
          Редактировать карточку
        </button>
        <button type="button" disabled={loading} onClick={() => void open("media")}>
          Фотографии
        </button>
        {error && <small>{error}</small>}
      </div>
      {product && mode === "media" && (
        <Suspense
          fallback={
            <Dialog title="Фотографии" onClose={() => setMode("")}>
              <p role="status">Загружаем фотографии…</p>
            </Dialog>
          }
        >
          <ProductMediaDialog
            owner={adminRole === "owner"}
            product={product}
            onClose={() => setMode("")}
            onChanged={onChanged}
          />
        </Suspense>
      )}
    </>
  );
}
