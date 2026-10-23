import { useEffect, useMemo, useState } from "react";
import { api, statusLabels } from "./adminShared";

export type LinkChannel = "saby" | "wb" | "ozon" | "avito";
type ChannelLink = { linked: boolean; externalIds: string[]; listingNames?: string[] };
type MarketplaceScope = "unknown" | "site_only" | "marketplaces";
type VariantLink = {
  id: number;
  sku: string;
  label: string;
  sabyId: string;
  marketplaceScope: MarketplaceScope;
  sitePriceRub: number;
  channelPricesRub: Partial<Record<"saby" | "wb" | "ozon", number | null>>;
  channels: Record<"saby" | "wb" | "ozon", { externalIds: string[] }>;
};
export type ProductLink = {
  id: number;
  name: string;
  slug: string;
  status: string;
  variants: VariantLink[];
  channels: Record<LinkChannel, ChannelLink>;
};

const channelNames: Record<LinkChannel, string> = {
  saby: "СБИС",
  wb: "Wildberries",
  ozon: "Ozon",
  avito: "Авито",
};
const rubles = (value: number) =>
  `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })} ₽`;
const fullyLinked = (product: ProductLink, channel: LinkChannel) =>
  channel === "avito"
    ? Boolean(product.channels.avito?.linked)
    : product.variants.length > 0 &&
      product.variants.every((variant) => variant.channels[channel]?.externalIds.length > 0);
type LinkState = "linked" | "unlinked" | "site_only" | "unknown" | "conflict";
const marketplaceState = (product: ProductLink, channel: "wb" | "ozon"): LinkState => {
  const variants = product.variants;
  if (
    variants.some(
      (variant) =>
        variant.marketplaceScope === "site_only" && variant.channels[channel]?.externalIds.length,
    )
  )
    return "conflict";
  const expected = variants.filter((variant) => variant.marketplaceScope === "marketplaces");
  if (expected.some((variant) => !variant.channels[channel]?.externalIds.length)) return "unlinked";
  if (variants.some((variant) => variant.marketplaceScope === "unknown")) return "unknown";
  return expected.length ? "linked" : "site_only";
};
const linkState = (product: ProductLink, channel: LinkChannel): LinkState =>
  channel === "wb" || channel === "ozon"
    ? marketplaceState(product, channel)
    : fullyLinked(product, channel)
      ? "linked"
      : "unlinked";
const stateLabels: Record<LinkState, string> = {
  linked: "Связан",
  unlinked: "Нужна связь",
  site_only: "Только сайт и Авито",
  unknown: "Каналы не определены",
  conflict: "Лишняя связь",
};
const potSize = (value: string) => value.match(/(?:^|\s|[,(])(?:d|д)\s*(\d{1,2})(?=$|\D)/i)?.[1];
const sizeMismatch = (product: ProductLink, channel: "wb" | "ozon") =>
  product.variants.some((variant) => {
    const siteSize = potSize(variant.label);
    return (
      siteSize &&
      variant.channels[channel]?.externalIds.some((externalId) => {
        const externalSize = potSize(externalId);
        return externalSize && Number(externalSize) !== Number(siteSize);
      })
    );
  });

type SabyCandidate = {
  id: string;
  code: string;
  article: string;
  name: string;
  missing: boolean;
  linkedVariantId?: number;
  linkedProductId?: number;
  mappedProductId?: number;
};

function SabyLinkPicker({
  product,
  variant,
  onLinked,
  onError,
}: {
  product: ProductLink;
  variant: VariantLink;
  onLinked: () => void;
  onError: (message: string) => void;
}) {
  const replacing = Boolean(variant.sabyId);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<SabyCandidate[]>([]);
  const [selected, setSelected] = useState<SabyCandidate | null>(null);
  const [busy, setBusy] = useState(false);
  const search = async () => {
    if (query.trim().length < 2) return;
    setBusy(true);
    setSelected(null);
    try {
      const data = await api<{ candidates: SabyCandidate[] }>(
        `/api/v1/admin/saby/link-candidates?q=${encodeURIComponent(query.trim())}`,
      );
      setCandidates(data.candidates || []);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const link = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      await api(`/api/v1/admin/variants/${variant.id}/${replacing ? "saby-relink" : "saby-link"}`, {
        method: "POST",
        body: JSON.stringify(
          replacing
            ? { oldSabyId: variant.sabyId, newSabyId: selected.id }
            : { sabyId: selected.id },
        ),
      });
      setOpen(false);
      onLinked();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div onClick={(event) => event.stopPropagation()}>
      <button type="button" className="text-button" onClick={() => setOpen(!open)}>
        {open ? "Закрыть поиск" : `${replacing ? "Перепривязать" : "Связать"} SKU ${variant.sku}`}
      </button>
      {open && (
        <div className="admin-pim-editor">
          <p>
            Карточка сайта: <strong>{product.name}</strong> · SKU {variant.sku}
            {variant.label ? ` · ${variant.label}` : ""}
            {replacing ? ` · текущая запись СБИС ${variant.sabyId}` : ""}
          </p>
          <div>
            <input
              aria-label="Поиск номенклатуры СБИС"
              placeholder="Код, артикул или название в СБИС"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void search();
              }}
            />{" "}
            <button
              type="button"
              disabled={busy || query.trim().length < 2}
              onClick={() => void search()}
            >
              Найти
            </button>
          </div>
          {candidates.map((candidate) => {
            const occupiedByOther = Boolean(
              (candidate.linkedProductId && candidate.linkedProductId !== product.id) ||
                (candidate.mappedProductId && candidate.mappedProductId !== product.id),
            );
            return (
              <label key={candidate.id} className="admin-checkbox">
                <input
                  type="radio"
                  name={`saby-link-${variant.id}`}
                  disabled={
                    candidate.missing ||
                    Boolean(candidate.linkedVariantId) ||
                    occupiedByOther ||
                    busy
                  }
                  checked={selected?.id === candidate.id}
                  onChange={() => setSelected(candidate)}
                />
                <span>
                  {candidate.name} · {candidate.code || candidate.id}
                  {candidate.article ? ` · ${candidate.article}` : ""}
                  {candidate.missing
                    ? " · отсутствует в выгрузке"
                    : candidate.linkedVariantId
                      ? ` · уже связан с SKU #${candidate.linkedVariantId}`
                      : occupiedByOther
                        ? ` · уже связан с карточкой #${candidate.linkedProductId || candidate.mappedProductId}`
                        : ""}
                </span>
              </label>
            );
          })}
          {selected && (
            <div>
              <p>
                {replacing
                  ? "Старая запись должна отсутствовать в актуальной выгрузке СБИС. История связи сохранится; остаток обнулится до следующей синхронизации."
                  : "Остаток появится после следующей синхронизации СБИС."}{" "}
                Подтвердите связь <strong>{variant.sku}</strong> →{" "}
                <strong>
                  {selected.name} ({selected.code || selected.id})
                </strong>
                .
              </p>
              <button type="button" disabled={busy} onClick={() => void link()}>
                {busy ? "Связываем…" : replacing ? "Подтвердить перепривязку" : "Подтвердить связь"}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function AdminProductLinks({
  channel,
  canOpen,
  canManageAvito,
  canLinkSaby,
  onOpen,
  onError,
}: {
  channel: LinkChannel;
  canOpen: boolean;
  canManageAvito: boolean;
  canLinkSaby: boolean;
  onOpen: (productId: number) => void;
  onError: (message: string) => void;
}) {
  const [products, setProducts] = useState<ProductLink[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<{
    channel: LinkChannel;
    value: "all" | LinkState | "size_mismatch";
  }>({ channel, value: "all" });
  const activeFilter = filter.channel === channel ? filter.value : "all";
  const [revision, setRevision] = useState(0);
  const [scopeSaving, setScopeSaving] = useState<number | null>(null);
  const [scopeNotice, setScopeNotice] = useState("");
  useEffect(() => {
    let active = true;
    api<{ products: ProductLink[] }>("/api/v1/admin/products/links")
      .then((data) => {
        if (active) {
          setProducts(data.products || []);
          setError("");
        }
      })
      .catch((caught) => {
        if (active) {
          const message = (caught as Error).message;
          setError(message);
          onError(message);
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [onError, revision]);
  const counts = useMemo(
    () => ({
      linked: products.filter((product) => linkState(product, channel) === "linked").length,
      unlinked: products.filter((product) => linkState(product, channel) === "unlinked").length,
      site_only: products.filter((product) => linkState(product, channel) === "site_only").length,
      unknown: products.filter((product) => linkState(product, channel) === "unknown").length,
      conflict: products.filter((product) => linkState(product, channel) === "conflict").length,
      size_mismatch:
        channel === "wb" || channel === "ozon"
          ? products.filter((product) => sizeMismatch(product, channel)).length
          : 0,
    }),
    [products, channel],
  );
  const visible = useMemo(
    () =>
      products.filter((product) => {
        if (
          activeFilter === "size_mismatch"
            ? (channel !== "wb" && channel !== "ozon") || !sizeMismatch(product, channel)
            : activeFilter !== "all" && linkState(product, channel) !== activeFilter
        )
          return false;
        const channelIds = product.channels[channel]?.externalIds || [];
        const names = product.channels[channel]?.listingNames || [];
        const searchable = [
          product.name,
          product.id,
          product.slug,
          ...channelIds,
          ...names,
          ...product.variants.flatMap((variant) => [variant.sku, variant.label]),
        ]
          .join(" ")
          .toLowerCase();
        return searchable.includes(query.trim().toLowerCase());
      }),
    [products, channel, activeFilter, query],
  );

  return (
    <section className="admin-product-links" aria-label={`Связи ${channelNames[channel]}`}>
      <header className="admin-product-links-heading">
        <div>
          <h2>Связи · {channelNames[channel]}</h2>
          <p>
            {channel === "avito"
              ? "Одно объявление может предлагать несколько размеров растения. Связь Авито относится ко всей карточке; сопоставление выполняется в разделе Авито."
              : channel === "wb" || channel === "ozon"
                ? "Разрешённые каналы задаются на сайте по каждому SKU. «Нужна связь» показывает только размеры, которым разрешены маркетплейсы. Связь сама по себе не публикует объявление."
                : "Связи показаны по SKU каждого размера. Цены каналов взяты из последнего локального зеркала и не подтверждают отправку новой цены."}
          </p>
        </div>
      </header>
      {scopeNotice && (
        <p role="status" className="admin-hint">
          {scopeNotice}
        </p>
      )}
      <p className="admin-hint">
        Сопоставление, разрешение продажи и результат отправки — разные состояния. Предложения цен и
        их ошибки доступны в «Размеры и цены» карточки.{" "}
        <a href="/admin?section=marketplaces">Открыть журнал обмена</a>
      </p>
      <div className="admin-product-links-filters" role="group" aria-label="Состояние связи">
        <button
          type="button"
          className={activeFilter === "all" ? "active" : ""}
          aria-pressed={activeFilter === "all"}
          onClick={() => setFilter({ channel, value: "all" })}
        >
          Все <b>{products.length}</b>
        </button>
        <button
          type="button"
          className={activeFilter === "linked" ? "active" : ""}
          aria-pressed={activeFilter === "linked"}
          onClick={() => setFilter({ channel, value: "linked" })}
        >
          Связанные <b>{counts.linked}</b>
        </button>
        <button
          type="button"
          className={activeFilter === "unlinked" ? "active" : ""}
          aria-pressed={activeFilter === "unlinked"}
          onClick={() => setFilter({ channel, value: "unlinked" })}
        >
          {channel === "wb" || channel === "ozon" ? "Нужна связь" : "Не связанные"}{" "}
          <b>{counts.unlinked}</b>
        </button>
        {(channel === "wb" || channel === "ozon") && (
          <>
            <button
              type="button"
              className={activeFilter === "site_only" ? "active" : ""}
              aria-pressed={activeFilter === "site_only"}
              onClick={() => setFilter({ channel, value: "site_only" })}
            >
              Только сайт и Авито <b>{counts.site_only}</b>
            </button>
            <button
              type="button"
              className={activeFilter === "conflict" ? "active" : ""}
              aria-pressed={activeFilter === "conflict"}
              onClick={() => setFilter({ channel, value: "conflict" })}
            >
              Лишняя связь <b>{counts.conflict}</b>
            </button>
            <button
              type="button"
              className={activeFilter === "size_mismatch" ? "active" : ""}
              aria-pressed={activeFilter === "size_mismatch"}
              onClick={() => setFilter({ channel, value: "size_mismatch" })}
            >
              Проверить размер <b>{counts.size_mismatch}</b>
            </button>
            <button
              type="button"
              className={activeFilter === "unknown" ? "active" : ""}
              aria-pressed={activeFilter === "unknown"}
              onClick={() => setFilter({ channel, value: "unknown" })}
            >
              Не определены <b>{counts.unknown}</b>
            </button>
          </>
        )}
        <input
          aria-label="Поиск по товару или внешнему коду"
          placeholder="Название, SKU или внешний код"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      {loading ? (
        <p className="admin-hint">Загружаем связи…</p>
      ) : error ? (
        <p className="admin-inline-error" role="alert">
          Не удалось загрузить связи: {error}
        </p>
      ) : visible.length === 0 ? (
        <p className="admin-hint">По выбранному фильтру товаров нет.</p>
      ) : (
        <div className="admin-table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th>Товар сайта</th>
                <th>Состояние</th>
                <th>Связь с {channelNames[channel]}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {visible.map((product) => {
                const link = product.channels[channel];
                const state = linkState(product, channel);
                const partiallyLinked =
                  channel !== "avito" &&
                  channel !== "wb" &&
                  channel !== "ozon" &&
                  link?.linked &&
                  product.variants.some(
                    (variant) => !variant.channels[channel]?.externalIds.length,
                  );
                return (
                  <tr
                    key={product.id}
                    className={channel === "avito" || !canOpen ? "" : "clickable"}
                    onClick={channel === "avito" || !canOpen ? undefined : () => onOpen(product.id)}
                  >
                    <td>
                      {canOpen ? (
                        <button
                          type="button"
                          className="text-button"
                          onClick={(event) => {
                            event.stopPropagation();
                            onOpen(product.id);
                          }}
                        >
                          <strong>{product.name}</strong>
                        </button>
                      ) : (
                        <strong>{product.name}</strong>
                      )}
                      <small>
                        Карточка #{product.id} ·{" "}
                        {statusLabels[product.status as keyof typeof statusLabels] ||
                          product.status}
                      </small>
                      <small>
                        {product.variants.length}{" "}
                        {product.variants.length === 1 ? "размер" : "размеров"}:{" "}
                        {product.variants
                          .map(
                            (variant) =>
                              `${variant.sku}${variant.label ? ` (${variant.label})` : ""}`,
                          )
                          .join(", ")}
                      </small>
                    </td>
                    <td>
                      <span className={`admin-pill ${state === "linked" ? "published" : "draft"}`}>
                        {partiallyLinked ? "Часть размеров связана" : stateLabels[state]}
                      </span>
                      {(channel === "wb" || channel === "ozon") &&
                        sizeMismatch(product, channel) && (
                          <small role="alert">Размер в артикуле площадки не совпадает с SKU</small>
                        )}
                      {(channel === "wb" || channel === "ozon") &&
                        product.variants.map((variant) => (
                          <small key={variant.id}>
                            SKU {variant.sku}:{" "}
                            {variant.marketplaceScope === "marketplaces"
                              ? "WB и Ozon разрешены"
                              : variant.marketplaceScope === "site_only"
                                ? "только сайт и Авито"
                                : "каналы не определены"}
                          </small>
                        ))}
                    </td>
                    <td>
                      {link?.linked ? (
                        <>
                          {channel === "avito" && (link.listingNames || []).length > 0 && (
                            <strong>{link.listingNames?.join(", ")}</strong>
                          )}
                          <small>Коды: {link.externalIds.join(", ")}</small>
                          {channel !== "avito" &&
                            product.variants.map((variant) => (
                              <small key={variant.id}>
                                SKU {variant.sku}:{" "}
                                {variant.channels[channel]?.externalIds.length
                                  ? variant.channels[channel].externalIds.join(", ")
                                  : "нет связи"}{" "}
                                · сайт {rubles(variant.sitePriceRub)} · зеркало{" "}
                                {channelNames[channel]}{" "}
                                {variant.channelPricesRub?.[channel] != null
                                  ? rubles(variant.channelPricesRub[channel]!)
                                  : "нет цены"}
                              </small>
                            ))}
                        </>
                      ) : (
                        <small>Внешняя карточка не привязана</small>
                      )}
                    </td>
                    <td>
                      {channel === "avito" ? (
                        canManageAvito ? (
                          <a
                            className="text-button"
                            href={`/admin?section=avito&product=${product.id}&q=${encodeURIComponent(product.name)}`}
                          >
                            Сопоставить в Авито ↗
                          </a>
                        ) : null
                      ) : (
                        <>
                          {canOpen && (
                            <button
                              type="button"
                              className="text-button"
                              onClick={(event) => {
                                event.stopPropagation();
                                onOpen(product.id);
                              }}
                            >
                              Открыть связи
                            </button>
                          )}
                          {channel === "saby" &&
                            canLinkSaby &&
                            product.variants.map((variant) => (
                              <SabyLinkPicker
                                key={variant.id}
                                product={product}
                                variant={variant}
                                onLinked={() => setRevision((value) => value + 1)}
                                onError={onError}
                              />
                            ))}
                          {(channel === "wb" || channel === "ozon") &&
                            canLinkSaby &&
                            product.variants.map((variant) => (
                              <label key={variant.id} onClick={(event) => event.stopPropagation()}>
                                <small>Каналы SKU {variant.sku}</small>
                                <select
                                  disabled={scopeSaving === variant.id}
                                  aria-label={`Каналы SKU ${variant.sku}`}
                                  value={variant.marketplaceScope}
                                  onChange={async (event) => {
                                    const marketplaceScope = event.target.value as MarketplaceScope;
                                    setScopeSaving(variant.id);
                                    setScopeNotice("");
                                    try {
                                      await api(
                                        `/api/v1/admin/variants/${variant.id}/marketplace-scope`,
                                        {
                                          method: "PATCH",
                                          body: JSON.stringify({ marketplaceScope }),
                                        },
                                      );
                                      setRevision((value) => value + 1);
                                      setScopeNotice(
                                        `Артикул ${variant.sku}: разрешение каналов сохранено. Публикация проверяется отдельно.`,
                                      );
                                    } catch (error) {
                                      onError((error as Error).message);
                                      setRevision((value) => value + 1);
                                    } finally {
                                      setScopeSaving(null);
                                    }
                                  }}
                                >
                                  <option value="unknown">Не определены</option>
                                  <option value="site_only">Только сайт и Авито</option>
                                  <option value="marketplaces">WB и Ozon разрешены</option>
                                </select>
                              </label>
                            ))}
                        </>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
