export function OrderProductLink({
  item,
}: {
  item: {
    productId?: number;
    productCode?: string;
    sku?: string;
    productName: string;
  };
}) {
  const href = item.productCode
    ? `/product/${encodeURIComponent(item.productCode)}${item.sku ? `?sku=${encodeURIComponent(item.sku)}` : ""}`
    : item.productId
      ? `/admin?section=products&product=${item.productId}`
      : "";
  return href ? (
    <a className="order-product-link" href={href} target="_blank" rel="noopener noreferrer">
      <strong>{item.productName}</strong>
    </a>
  ) : (
    <strong>{item.productName}</strong>
  );
}
