import { useState } from "react";
import { Dialog } from "./adminShared";
import { ProductMediaManager } from "./ProductMediaManager";
import type { Product } from "./adminTypes";

export default function PdpProductMediaDialog({
  canDelete,
  product,
  onClose,
  onChanged,
}: {
  canDelete: boolean;
  product: Product;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [error, setError] = useState("");
  return (
    <Dialog title={`Фотографии · ${product.name}`} onClose={onClose}>
      {error && <p className="admin-error">{error}</p>}
      <ProductMediaManager
        canDelete={canDelete}
        productId={product.id}
        onError={setError}
        onChanged={onChanged}
      />
    </Dialog>
  );
}
