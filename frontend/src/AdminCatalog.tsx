import { useCallback, useEffect, useMemo, useState } from "react";
import { ImportDialog, NewProductDialog, ProductDialog, SyncDialog } from "./AdminCatalogDialogs";
import { PageHeading, api, money, sabyFieldLabels, statusLabels } from "./adminShared";
import { AttributeManager } from "./AdminPim";
import { CollectionsV2 } from "./AdminCollectionsV2";
import type { Category, Product, ReviewModerationItem } from "./adminTypes";

// Flattens the category tree into the order it reads in: each parent is
// followed by its own children, siblings sorted by sortOrder and then by
// name. Every consumer that lists categories uses this, so the admin sees
// the same order in the tree, in the parent picker and on a product card.
export function orderCategoryTree(items: Category[]): { item: Category; depth: number }[] {
  const result: { item: Category; depth: number }[] = [];
  const append = (parentId: number | null, depth: number) => {
    items
      .filter((item) => item.parentId === parentId)
      .sort((left, right) => left.sortOrder - right.sortOrder || left.name.localeCompare(right.name, "ru"))
      .forEach((item) => { result.push({ item, depth }); append(item.id, depth + 1); });
  };
  append(null, 0);
  // Categories whose parent is missing would otherwise vanish from the list.
  items
    .filter((item) => item.parentId !== null && !items.some((parent) => parent.id === item.parentId))
    .sort((left, right) => left.name.localeCompare(right.name, "ru"))
    .forEach((item) => result.push({ item, depth: 0 }));
  return result;
}

// The category list is long, so a product card shows only the current
// choice and reveals the tree on demand instead of an always-open list.
export function CategoryPicker({ categories, value, onChange }: {
  categories: Category[];
  value?: number;
  onChange: (value?: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(categories);
  const selected = categories.find((item) => item.id === value);
  // Only branches the user opened are listed, so the picker shows a handful
  // of sections instead of every leaf in the catalogue.
  const visible = ordered.filter(({ item }) => {
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = categories.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return true;
  });
  const toggle = (id: number) => setExpanded((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
  return <div className="category-picker">
    <button type="button" className="category-picker-toggle" aria-expanded={open} onClick={() => setOpen((current) => !current)}>
      <span>{selected ? selected.name : "Не указано"}</span>
      <span aria-hidden="true">{open ? "−" : "+"}</span>
    </button>
    {open && <div className="category-picker-list">
      <button type="button" className={value ? "" : "active"} onClick={() => { onChange(undefined); setOpen(false); }}>Не указано</button>
      {visible.map(({ item, depth }) => {
        const hasChildren = categories.some((candidate) => candidate.parentId === item.id);
        return <div className="category-picker-row" key={item.id} style={{ paddingLeft: depth * 18 }}>
          {hasChildren
            ? <button type="button" className="category-toggle" aria-expanded={expanded.has(item.id)} onClick={() => toggle(item.id)}>{expanded.has(item.id) ? "−" : "+"}</button>
            : <span className="category-toggle placeholder" aria-hidden="true" />}
          <button
            type="button"
            className={value === item.id ? "active" : ""}
            onClick={() => { onChange(item.id); setOpen(false); }}
          >
            {item.name}
          </button>
        </div>;
      })}
    </div>}
  </div>;
}

export function Categories({ canEdit, owner, onError }: { canEdit: boolean; owner: boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Category[]>([]);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [parentId, setParentId] = useState("");
  const load = useCallback(() => api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setItems(data.categories)).catch((error) => onError(error.message)), [onError]);
  useEffect(() => { void load(); }, [load]);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const ordered = orderCategoryTree(items);
  const orderedItems = ordered.map((entry) => entry.item);
  const depth = (item: Category) => ordered.find((entry) => entry.item.id === item.id)?.depth ?? 0;
  // Only rows whose whole chain of parents is expanded are shown; the tree
  // therefore starts collapsed to the root sections.
  const visibleItems = orderedItems.filter((item) => {
    let parent = item.parentId;
    while (parent) {
      if (!expanded.has(parent)) return false;
      parent = items.find((candidate) => candidate.id === parent)?.parentId ?? null;
    }
    return true;
  });
  const toggle = (id: number) => setExpanded((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
  const create = async () => {
    try {
      await api("/api/v1/admin/categories", { method: "POST", body: JSON.stringify({ name, slug, parentId: parentId ? Number(parentId) : null, sortOrder: items.length * 10 }) });
      setName(""); setSlug(""); setParentId(""); load();
    } catch (error) { onError((error as Error).message); }
  };
  const rename = async (item: Category) => {
    const next = window.prompt("Новое название категории", item.name);
    if (!next || next === item.name) return;
    try { await api(`/api/v1/admin/categories/${item.id}`, { method: "PATCH", body: JSON.stringify({ name: next }) }); load(); }
    catch (error) { onError((error as Error).message); }
  };
  const remove = async (item: Category) => {
    if (!window.confirm(`Удалить категорию «${item.name}»?`)) return;
    try { await api(`/api/v1/admin/categories/${item.id}`, { method: "DELETE" }); load(); }
    catch (error) { onError((error as Error).message); }
  };
  return <><PageHeading eyebrow="Структура каталога" title="Категории и атрибуты" text="Дерево любой глубины. Категории с товарами или дочерними узлами защищены от удаления." />
    {canEdit && <div className="admin-toolbar category-create"><input value={name} onChange={(event) => setName(event.target.value)} placeholder="Название" /><input value={slug} onChange={(event) => setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-"))} placeholder="slug" /><select value={parentId} onChange={(event) => setParentId(event.target.value)}><option value="">Корневая категория</option>{orderedItems.map((item) => <option value={item.id} key={item.id}>{`${"— ".repeat(depth(item))}${item.name}`}</option>)}</select><button className="admin-primary" disabled={!name.trim() || !slug.trim()} onClick={create}>Добавить</button></div>}
    <div className="admin-toolbar category-expand">
      <button className="admin-action" onClick={() => setExpanded(new Set(items.map((item) => item.id)))}>Раскрыть всё</button>
      <button className="admin-action" onClick={() => setExpanded(new Set())}>Свернуть всё</button>
    </div>
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Категория</th><th>Slug</th><th>Товары</th><th /></tr></thead><tbody>{visibleItems.map((item) => {
      const hasChildren = items.some((candidate) => candidate.parentId === item.id);
      return <tr key={item.id} className={canEdit ? "clickable" : ""} onClick={() => { if (canEdit) rename(item); }}><td><strong style={{ paddingLeft: depth(item) * 24 }}>
        {hasChildren
          ? <button className="category-toggle" aria-expanded={expanded.has(item.id)} onClick={(event) => { event.stopPropagation(); toggle(item.id); }}>{expanded.has(item.id) ? "−" : "+"}</button>
          : <span className="category-toggle placeholder" aria-hidden="true" />}
        {item.name}
      </strong></td><td><code>{item.slug}</code></td><td>{item.productsCount}</td><td>{canEdit && <button className="text-button danger" onClick={(event) => { event.stopPropagation(); remove(item); }}>Удалить</button>}</td></tr>;
    })}</tbody></table></div>
    {owner && <AttributeManager categories={items} onError={onError} />}
  </>;
}

export function Collections({ onError }: { onError: (value: string) => void }) {
  return <CollectionsV2 onError={onError} />;
}

export function Products({ can, onError }: { can: (permission: string) => boolean; onError: (value: string) => void }) {
  const [items, setItems] = useState<Product[]>([]);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<number[]>([]);
  const [editing, setEditing] = useState<Product | null>(null);
  const [syncing, setSyncing] = useState<number[] | null>(null);
  const [creating, setCreating] = useState(false);
  const [importing, setImporting] = useState(false);
  const [reviews, setReviews] = useState<ReviewModerationItem[]>([]);
  const reload = () => api<{ products: Product[] }>("/api/v1/admin/products").then((data) => setItems(data.products)).catch((error) => onError((error as Error).message));
  useEffect(() => { api<{ products: Product[] }>("/api/v1/admin/products").then((data) => setItems(data.products)).catch((error) => onError(error.message)); }, [onError]);
  useEffect(() => { if (can("products.read")) api<{ reviews?: ReviewModerationItem[] }>("/api/v1/admin/reviews").then((data) => setReviews(data.reviews || [])).catch((error) => onError(error.message)); }, [can, onError]);
  const moderate = async (id: number, status: "published" | "rejected") => { try { await api(`/api/v1/admin/reviews/${id}`, { method: "PATCH", body: JSON.stringify({ status }) }); setReviews((current) => current.filter((item) => item.id !== id)); } catch (error) { onError((error as Error).message); } };
  const filtered = useMemo(() => items.filter((item) => `${item.name} ${item.sku}`.toLowerCase().includes(query.toLowerCase())), [items, query]);
  const replace = (product: Product) => setItems((current) => current.map((item) => item.id === product.id ? product : item));
  return <><PageHeading eyebrow="Каталог" title="Товары" text="Контент сайта, цены, упаковка, публикация и выборочная синхронизация со СБИС" />
    <div className="admin-toolbar"><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название или артикул" /><span>{selected.length ? `Выбрано: ${selected.length}` : `${filtered.length} товаров`}</span>{selected.length > 0 && can("products.sync") && <button className="admin-primary" onClick={() => setSyncing(selected)}>Подтянуть из СБИС</button>}{can("products.edit") && <button onClick={() => setImporting(true)}>Импорт из СБИС</button>}{can("products.edit") && <button className="admin-primary" onClick={() => setCreating(true)}>Новый товар</button>}</div>
    <div className="admin-table-wrap"><table className="admin-table products"><thead><tr><th><input type="checkbox" checked={filtered.length > 0 && filtered.every((item) => selected.includes(item.id))} onChange={(event) => setSelected(event.target.checked ? filtered.map((item) => item.id) : [])} /></th><th>Товар</th><th>Цена / остаток</th><th>Публикация</th><th>СБИС</th><th /></tr></thead><tbody>{filtered.map((product) => <tr
      key={product.id}
      className={can("products.edit") ? "clickable" : ""}
      onClick={() => { if (can("products.edit")) setEditing(product); }}
    >
      <td onClick={(event) => event.stopPropagation()}><input type="checkbox" checked={selected.includes(product.id)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, product.id] : current.filter((id) => id !== product.id))} /></td>
      <td><div className="admin-product"><img src={product.image || "/assets/hero-monstera.png"} alt="" /><div><strong>{product.name}</strong><small>{product.sku} · {product.variantLabel}</small><a href={`/product/${product.slug}`} target="_blank" onClick={(event) => event.stopPropagation()}>Открыть карточку ↗</a></div></div></td>
      <td><strong>{money.format(product.price)}</strong><small>В наличии: {product.stock}</small><small>Опт от {product.wholesaleMinQty} шт.</small></td>
      <td><span className={`admin-pill ${product.status}`}>{statusLabels[product.status] || product.status}</span>{product.overrideFields.length > 0 && <small>Изменено вручную: {product.overrideFields.join(", ")}</small>}</td>
      <td><strong>{product.sabyId ? (product.sabyCode || "Связан") : "Наш товар"}</strong><small>{product.sabyFields.length ? "Берём: " + product.sabyFields.map((field) => sabyFieldLabels[field] || field).join(", ") : "Ничего не берём"}</small><small>{product.sabyUpdatedAt ? new Date(product.sabyUpdatedAt).toLocaleString("ru-RU") : "Не синхронизировался"}</small>{can("products.sync") && product.sabyId && <button className="text-button" onClick={(event) => { event.stopPropagation(); setSyncing([product.id]); }}>Синхронизировать</button>}</td>
      <td><span className="admin-row-arrow" aria-hidden="true">→</span></td>
    </tr>)}</tbody></table></div>
    <section className="admin-block review-moderation"><div className="admin-block-heading"><div><p className="eyebrow">Контроль качества</p><h2>Отзывы на модерации</h2></div><span className="admin-pill">{reviews.length}</span></div>{reviews.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар / покупатель</th><th>Оценка</th><th>Отзыв и медиа</th><th>Решение</th></tr></thead><tbody>{reviews.map((review) => <tr key={review.id}><td><strong>{review.product}</strong><small>{review.author} · {new Date(review.createdAt).toLocaleDateString("ru-RU")}</small></td><td><span className="review-stars">{'★'.repeat(review.rating)}{'☆'.repeat(5-review.rating)}</span></td><td><p>{review.text}</p>{review.media?.length > 0 && <div className="admin-review-media">{review.media.map((media) => media.contentType.startsWith("video/") ? <video key={media.url} src={media.url} controls preload="metadata" /> : <img key={media.url} src={media.url} alt="Вложение к отзыву" />)}</div>}</td><td><div className="admin-review-actions"><button disabled={!can("products.edit")} onClick={() => void moderate(review.id, "rejected")}>Отклонить</button><button className="admin-primary" disabled={!can("products.edit")} onClick={() => void moderate(review.id, "published")}>Опубликовать</button></div></td></tr>)}</tbody></table></div> : <p className="admin-hint">Новых отзывов нет.</p>}</section>
    {editing && <ProductDialog product={editing} onClose={() => setEditing(null)} onSaved={(product) => { replace(product); setEditing(null); }} onError={onError} />}
    {creating && <NewProductDialog onClose={() => setCreating(false)} onCreated={() => { setCreating(false); reload(); }} onError={onError} />}
    {importing && <ImportDialog onClose={() => setImporting(false)} onImported={() => { setImporting(false); reload(); }} onError={onError} />}
    {syncing && <SyncDialog count={syncing.length} onClose={() => setSyncing(null)} onSync={async (fields) => { try { await api("/api/v1/admin/products/sync", { method: "POST", body: JSON.stringify({ productIds: syncing, fields }) }); const data = await api<{ products: Product[] }>("/api/v1/admin/products"); setItems(data.products); setSelected([]); setSyncing(null); } catch (error) { onError((error as Error).message); } }} />}
  </>;
}
