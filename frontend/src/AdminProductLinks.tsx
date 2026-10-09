import { useEffect, useMemo, useState } from "react";
import { api, statusLabels } from "./adminShared";

export type LinkChannel = "saby" | "wb" | "ozon" | "avito";
type ChannelLink = { linked: boolean; externalIds: string[]; listingNames?: string[] };
type VariantLink = { id: number; sku: string; label: string; channels: Record<"saby" | "wb" | "ozon", { externalIds: string[] }> };
export type ProductLink = {
  id: number;
  name: string;
  slug: string;
  status: string;
  variants: VariantLink[];
  channels: Record<LinkChannel, ChannelLink>;
};

const channelNames: Record<LinkChannel, string> = { saby: "СБИС", wb: "Wildberries", ozon: "Ozon", avito: "Авито" };
const fullyLinked = (product: ProductLink, channel: LinkChannel) => channel === "avito"
  ? Boolean(product.channels.avito?.linked)
  : product.variants.length > 0 && product.variants.every((variant) => variant.channels[channel]?.externalIds.length > 0);

export function AdminProductLinks({ channel, canOpen, canManageAvito, onOpen, onError }: {
  channel: LinkChannel;
  canOpen: boolean;
  canManageAvito: boolean;
  onOpen: (productId: number) => void;
  onError: (message: string) => void;
}) {
  const [products, setProducts] = useState<ProductLink[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<"all" | "linked" | "unlinked">("all");
  useEffect(() => {
    let active = true;
    api<{ products: ProductLink[] }>("/api/v1/admin/products/links")
      .then((data) => { if (active) { setProducts(data.products || []); setError(""); } })
      .catch((caught) => { if (active) { const message = (caught as Error).message; setError(message); onError(message); } })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [onError]);
  const counts = useMemo(() => ({
    linked: products.filter((product) => fullyLinked(product, channel)).length,
    unlinked: products.filter((product) => !fullyLinked(product, channel)).length,
  }), [products, channel]);
  const visible = useMemo(() => products.filter((product) => {
    const linked = fullyLinked(product, channel);
    if (filter !== "all" && linked !== (filter === "linked")) return false;
    const channelIds = product.channels[channel]?.externalIds || [];
    const names = product.channels[channel]?.listingNames || [];
    const searchable = [product.name, product.id, product.slug, ...channelIds, ...names, ...product.variants.flatMap((variant) => [variant.sku, variant.label])].join(" ").toLowerCase();
    return searchable.includes(query.trim().toLowerCase());
  }), [products, channel, filter, query]);

  return <section className="admin-product-links" aria-label={`Связи ${channelNames[channel]}`}>
    <header className="admin-product-links-heading"><div><h2>Связи · {channelNames[channel]}</h2><p>{channel === "avito" ? "Одно объявление может предлагать несколько размеров растения. Связь Авито относится ко всей карточке; сопоставление выполняется в разделе Авито." : "Связи показаны по SKU каждого размера. В «Не связанные» входят карточки, у которых не связан хотя бы один размер."}</p></div></header>
    <div className="admin-product-links-filters" role="group" aria-label="Состояние связи">
      <button type="button" className={filter === "all" ? "active" : ""} aria-pressed={filter === "all"} onClick={() => setFilter("all")}>Все <b>{products.length}</b></button>
      <button type="button" className={filter === "linked" ? "active" : ""} aria-pressed={filter === "linked"} onClick={() => setFilter("linked")}>Связанные <b>{counts.linked}</b></button>
      <button type="button" className={filter === "unlinked" ? "active" : ""} aria-pressed={filter === "unlinked"} onClick={() => setFilter("unlinked")}>Не связанные <b>{counts.unlinked}</b></button>
      <input aria-label="Поиск по товару или внешнему коду" placeholder="Название, SKU или внешний код" value={query} onChange={(event) => setQuery(event.target.value)} />
    </div>
    {loading ? <p className="admin-hint">Загружаем связи…</p> : error ? <p className="admin-inline-error" role="alert">Не удалось загрузить связи: {error}</p> : visible.length === 0 ? <p className="admin-hint">По выбранному фильтру товаров нет.</p> : <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар сайта</th><th>Состояние</th><th>Связь с {channelNames[channel]}</th><th /></tr></thead><tbody>{visible.map((product) => {
      const link = product.channels[channel];
      const partiallyLinked = channel !== "avito" && link?.linked && product.variants.some((variant) => !variant.channels[channel]?.externalIds.length);
      return <tr key={product.id} className={channel === "avito" || !canOpen ? "" : "clickable"} onClick={channel === "avito" || !canOpen ? undefined : () => onOpen(product.id)}>
        <td><strong>{product.name}</strong><small>Карточка #{product.id} · {statusLabels[product.status as keyof typeof statusLabels] || product.status}</small><small>{product.variants.length} {product.variants.length === 1 ? "размер" : "размеров"}: {product.variants.map((variant) => `${variant.sku}${variant.label ? ` (${variant.label})` : ""}`).join(", ")}</small></td>
        <td><span className={`admin-pill ${link?.linked ? "published" : "draft"}`}>{partiallyLinked ? "Часть размеров связана" : link?.linked ? "Связан" : "Не связан"}</span></td>
        <td>{link?.linked ? <>
          {channel === "avito" && (link.listingNames || []).length > 0 && <strong>{link.listingNames?.join(", ")}</strong>}
          <small>Коды: {link.externalIds.join(", ")}</small>
          {channel !== "avito" && product.variants.map((variant) => <small key={variant.id}>SKU {variant.sku}: {variant.channels[channel]?.externalIds.length ? variant.channels[channel].externalIds.join(", ") : "нет связи"}</small>)}
        </> : <small>Внешняя карточка не привязана</small>}</td>
        <td>{channel === "avito" ? canManageAvito ? <a className="text-button" href="/admin?section=avito">Сопоставить в Авито ↗</a> : null : canOpen ? <button type="button" className="text-button" onClick={(event) => { event.stopPropagation(); onOpen(product.id); }}>Открыть связи</button> : null}</td>
      </tr>;
    })}</tbody></table></div>}
  </section>;
}
