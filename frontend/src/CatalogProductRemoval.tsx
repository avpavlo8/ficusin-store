import { useEffect, useState } from "react";
import { ConfirmDialog, api } from "./adminShared";
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
  useEffect(
    () => () => {
      // Safari does not focus the trigger on tap. Restore it after modal cleanup.
      queueMicrotask(() => {
        document
          .querySelector<HTMLButtonElement>(`button[data-catalog-remove="${product.id}"]`)
          ?.focus();
      });
    },
    [product.id],
  );
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
    <ConfirmDialog
      title={`Удалить «${product.name}» из каталога?`}
      text={
        <>
          <p>
            Свободный черновик будет удалён. Опубликованные карточки и черновики с остатками,
            заказами или связями попадут в архив. Карточки на площадках останутся.
          </p>
          {error && <p role="alert">{error}</p>}
        </>
      }
      confirmLabel={busy ? "Удаляем…" : "Удалить карточку"}
      busy={busy}
      onCancel={onClose}
      onConfirm={() => void remove()}
    />
  );
}
