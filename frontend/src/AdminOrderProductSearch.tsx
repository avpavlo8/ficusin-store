import type { Product } from "./adminTypes";
import { money } from "./adminShared";
import { AdminProductSearch } from "./AdminProductSearch";

export function AdminOrderProductSearch({
  products,
  ...props
}: {
  products: Product[];
  value: string;
  disabled: boolean;
  onChange: (sku: string) => void;
}) {
  return (
    <AdminProductSearch
      {...props}
      label="Добавить товар в заказ"
      clearOnSearch
      options={products.map((product) => ({
        value: product.sku,
        name:
          product.name +
          (product.variantLabel && !product.name.includes(product.variantLabel)
            ? ` · ${product.variantLabel}`
            : ""),
        detail: `${product.sku} · ${money.format(product.price)} · остаток ${product.stock}`,
        search: `${product.latinName ?? ""} ${product.productCode ?? ""}`,
      }))}
    />
  );
}
