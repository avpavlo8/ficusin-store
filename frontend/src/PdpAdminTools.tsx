import { useState } from "react";
import { api } from "./adminShared";
import type { Product } from "./adminTypes";

export function PdpAdminTools({ slug, adminRole }: { slug: string; adminRole?: string }) {
  const allowed = adminRole === "owner" || adminRole === "administrator" || adminRole === "manager";
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
      window.location.assign(
        `/admin?section=products&product=${found.id}${next === "media" ? "&editor=media#product-media" : ""}`,
      );
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
    </>
  );
}
