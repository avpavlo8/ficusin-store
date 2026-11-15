import { useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import type { Product } from "./adminTypes";

export type ProductRemovalResult = { productId: number; action: "deleted" | "archived" };

export function CatalogProductRemoval({
  product,
  onClose,
  onRemoved,
  onBusyChange,
}: {
  product: Product;
  onClose: () => void;
  onRemoved: (result: ProductRemovalResult, message: string) => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const confirmation = useRef<HTMLElement | null>(null);
  useEffect(() => {
    confirmation.current?.scrollIntoView({ block: "nearest" });
    confirmation.current?.focus();
    return () => {
      document
        .querySelector<HTMLButtonElement>(`button[data-catalog-remove="${product.id}"]`)
        ?.focus();
    };
  }, [product.id]);
  const remove = async () => {
    if (busy) return;
    setBusy(true);
    onBusyChange(true);
    setError("");
    try {
      const result = await api<ProductRemovalResult>(`/api/v1/admin/products/${product.id}`, {
        method: "DELETE",
      });
      if (result.productId !== product.id || !["deleted", "archived"].includes(result.action))
        throw new Error("Сервер не подтвердил удаление карточки. Обновите каталог.");
      onRemoved(
        result,
        result.action === "deleted"
          ? `Карточка «${product.name}» удалена.`
          : `Карточка «${product.name}» перенесена в архив. Фотографии, связи и история заказов сохранены.`,
      );
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
      onBusyChange(false);
    }
  };
  return (
    <section
      className="catalog-bulk-confirm"
      aria-label="Удаление карточки"
      ref={confirmation}
      tabIndex={-1}
    >
      <strong>Удалить «{product.name}» из каталога?</strong>
      <p>
        Свободный черновик будет удалён. Карточка с остатками, заказами или связями попадёт в архив.
        Карточки на площадках останутся.
      </p>
      {error && <p role="alert">{error}</p>}
      <button type="button" disabled={busy} onClick={onClose}>
        Отмена
      </button>
      <button type="button" className="admin-danger" disabled={busy} onClick={() => void remove()}>
        {busy ? "Удаляем…" : "Удалить карточку"}
      </button>
    </section>
  );
}
