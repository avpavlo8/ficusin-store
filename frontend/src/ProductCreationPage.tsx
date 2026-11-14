import {
  mergeMarketplaceCards,
  replaceMarketplaceCard,
  type RemoteMarketplaceCard,
} from "./marketplaceAutofill";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, money } from "./adminShared";
import { useProductMarketplacePricing } from "./useProductMarketplacePricing";
import { EditorPricePublications } from "./EditorPricePublications";
import { NumericInput } from "./NumericInput";
import { AutoTextarea } from "./AutoTextarea";
import { categoryVariantName } from "./variantNaming";
import { CategoryPicker } from "./CategoryPicker";
import { rootCategory } from "./adminCatalogLogic";
import { normalizePlantAttributes, plantEditorSchema } from "./plantCharacteristicSchema";
import { PlantCharacteristicsEditor } from "./PlantCharacteristicsEditor";
import { ProductPackagingEditor } from "./ProductPackagingEditor";
import { PlantWarningsEditor } from "./PlantWarningsEditor";
import { CareFieldDisclosure } from "./CareFieldDisclosure";
import { PlantCareEditor } from "./PlantCareEditor";
import { ProductCollectionsEditor } from "./ProductCollectionsEditor";
import { AttributeFields } from "./AdminCatalogDialogs";
import { ProductPhotoGallery } from "./ProductPhotoGallery";
import {
  appendGalleryMedia,
  replaceVariantGallery,
  variantGallery,
  variantMediaToPersist,
  commonCover,
} from "./galleryOrder";
import { MarketplaceEditor } from "./MarketplaceEditor";
import { SabyEditorFields } from "./SabyEditorFields";
import { mergeSabyEditor } from "./sabyEditorHydration";
import {
  marketplaceOf,
  sabyOf,
  marketplaceMissing,
  blankCategory,
  type MarketplaceCommon,
  type SabyEditorConfig,
  type SabyEditorField,
  type SabyReadLink,
} from "./catalogEditorChannels";
import type { Category, CategoryAttribute, Product } from "./adminTypes";
import type { AdminVariant } from "./AdminPim";
import type { ProductMedia } from "./ProductMediaManager";
import {
  draftStorageKey,
  loadProductDraft,
  saveProductDraft,
  emptyProductDraft,
  saveDraftMedia,
  readDraftMedia,
  deleteDraftMedia,
  type ProductCreationDraft,
  type DraftVariant,
  type DraftMedia,
} from "./productCreationDraft";
import { productToDraft, variantToDraft, seedVariant, attributeDelta } from "./productEditorModel";
import "./styles/product-creation.css";

export function ProductCreationPage({
  onClose,
  product,
  onSaved,
  canManage,
}: {
  onClose: () => void;
  product?: Product;
  onSaved?: (product: Product) => void;
  canManage: boolean;
}) {
  const productId = product?.id;
  const storageKey = product ? `ficusin:product-edit-draft:v1:${product.id}` : draftStorageKey;
  const [initial] = useState(() =>
    loadProductDraft(storageKey, product ? productToDraft(product) : emptyProductDraft()),
  );
  const [draft, setDraft] = useState<ProductCreationDraft>(() => {
    const recovered = initial.draft;
    return {
      ...recovered,
      attributes: normalizePlantAttributes(recovered.attributes, !recovered.plantAttributesVersion),
      plantAttributesVersion: 1,
      passport: { ...product?.passport, ...recovered.passport },
      variants: recovered.variants?.length ? recovered.variants : [seedVariant(recovered)],
    };
  });
  const [tab, setTab] = useState<"product" | "channels" | "faq">(() =>
    new URLSearchParams(location.search).get("editor") === "faq" ||
    initial.draft.editorTab === "faq"
      ? "faq"
      : new URLSearchParams(location.search).get("editor") === "channels" ||
          initial.draft.editorTab === "channels" ||
          ["ozon", "wb", "saby", "relationships"].includes(
            new URLSearchParams(location.search).get("step") ||
              new URLSearchParams(location.search).get("tab") ||
              "",
          )
        ? "channels"
        : "product",
  );
  const [saveError, setSaveError] = useState(initial.error);
  const [categories, setCategories] = useState<Category[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [catalogRevision, setCatalogRevision] = useState(0);
  const [schemaCategoryId, setSchemaCategoryId] = useState<number>();
  const [schemaRevision, setSchemaRevision] = useState(0);
  const [schema, setSchema] = useState<CategoryAttribute[]>([]);
  const [catalogError, setCatalogError] = useState("");
  const [schemaError, setSchemaError] = useState("");
  const [remoteError, setRemoteError] = useState("");
  const [remoteNotice, setRemoteNotice] = useState("");
  const [loading, setLoading] = useState(Boolean(product));
  const [remoteLoaded, setRemoteLoaded] = useState(!product);
  const [remoteRevision, setRemoteRevision] = useState(0);
  const [collectionsRevision, setCollectionsRevision] = useState(0);
  const [channelStatuses, setChannelStatuses] = useState<
    Array<{
      variantId: number;
      channel: string;
      status: string;
      message: string;
      externalIds?: { productId: string; offerId?: string };
    }>
  >([]);
  const [channelLinks, setChannelLinks] = useState<
    Record<string, Partial<Record<"wb" | "ozon", string>>>
  >({});
  const [sabyLinks, setSabyLinks] = useState<Record<string, SabyReadLink>>({});
  const [marketplaceCardError, setMarketplaceCardError] = useState("");
  const [marketplaceCardBusy, setMarketplaceCardBusy] = useState(false);
  const [marketplaceCardRevision, setMarketplaceCardRevision] = useState(0);
  const loadedMarketplaceCards = useRef(new Set<string>());
  const [publicationMode, setPublicationMode] = useState("disabled");
  const [channelBarcodes, setChannelBarcodes] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [mediaBusy, setMediaBusy] = useState(false);
  const [mediaError, setMediaError] = useState("");
  const latest = useRef(draft);
  const original = useRef(product);
  const serverVariants = useRef<AdminVariant[]>([]);
  const serverMedia = useRef<ProductMedia[]>([]);
  const serverVariantMedia = useRef(new Map<number, ProductMedia[]>());
  const update = useCallback(
    (patch: Partial<ProductCreationDraft>) => {
      const next = { ...latest.current, ...patch, updatedAt: new Date().toISOString() };
      latest.current = next;
      setDraft(next);
      try {
        saveProductDraft(next, storageKey);
        setSaveError("");
        return true;
      } catch {
        setSaveError("Черновик не сохранён: хранилище браузера заполнено или недоступно.");
        return false;
      }
    },
    [storageKey],
  );
  useEffect(() => {
    const receive = (event: StorageEvent) => {
      if (event.key !== storageKey || !event.newValue) return;
      const data = loadProductDraft(storageKey);
      if (data.error) {
        setSaveError(data.error);
        return;
      }
      latest.current = data.draft;
      setDraft(data.draft);
    };
    window.addEventListener("storage", receive);
    return () => window.removeEventListener("storage", receive);
  }, [storageKey]);
  useEffect(() => {
    let active = true;
    api<{ categories: Category[] }>("/api/v1/admin/categories", {
      signal: AbortSignal.timeout(15000),
    })
      .then((data) => {
        if (active) setCategories(data.categories || []);
      })
      .catch(() => {
        if (active)
          setCatalogError(
            "Не удалось загрузить категории. Черновик сохранён. Перед сохранением товара повторите загрузку.",
          );
      })
      .finally(() => {
        if (active) setCatalogLoading(false);
      });
    return () => {
      active = false;
    };
  }, [catalogRevision]);
  useEffect(() => {
    if (!productId) return;
    let active = true;
    const controller = new AbortController();
    Promise.all([
      api<{ variants: AdminVariant[] }>(`/api/v1/admin/products/${productId}/variants`, {
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
      }),
      api<{ media: ProductMedia[] }>(`/api/v1/admin/products/${productId}/media`, {
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
      }),
    ])
      .then(async ([rows, media]) => {
        const mediaEntries: Array<[number, ProductMedia[]]> = [];
        for (let offset = 0; offset < (rows.variants || []).length; offset += 2) {
          if (!active) return;
          const chunk = await Promise.all(
            (rows.variants || []).slice(offset, offset + 2).map(async (row) => {
              const data = await api<{ media: ProductMedia[] }>(
                `/api/v1/admin/variants/${row.id}/media`,
                { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]) },
              );
              return [row.id, data.media || []] as [number, ProductMedia[]];
            }),
          );
          mediaEntries.push(...chunk);
        }

        if (!active) return;
        serverVariants.current = rows.variants || [];
        serverVariantMedia.current = new Map(mediaEntries);
        const ownedIds = new Set(mediaEntries.flatMap(([, items]) => items.map((item) => item.id)));
        serverMedia.current = (media.media || []).filter((item) => !ownedIds.has(item.id));
        const asDraft = (items: ProductMedia[]): DraftMedia[] =>
          [...items]
            .sort((a, b) => Number(b.primary) - Number(a.primary) || a.sortOrder - b.sortOrder)
            .map((item) => ({
              id: `server-${item.id}`,
              serverId: item.id,
              source: item.url,
              name: "Фото варианта",
              type: "image/jpeg",
            }));
        if (!latest.current.remoteInitialized) {
          const placeholder = latest.current.variants?.find((item) => item.id === "first");
          const variants = serverVariants.current.map((row, index) => {
            const next = variantToDraft(row);
            if (index !== 0 || !placeholder) return next;
            return {
              ...next,
              ...(placeholder.price !== String(original.current?.price)
                ? { price: placeholder.price }
                : {}),
              ...(placeholder.label !== "Основной вариант" ? { label: placeholder.label } : {}),
              sabyEditor: placeholder.sabyEditor,
              sabyEditorTouched: placeholder.sabyEditorTouched,
              ozon: placeholder.ozon,
              wb: placeholder.wb,
            };
          });
          const newVariants =
            latest.current.variants?.filter((item) => !item.serverId && item.id !== "first") || [];
          const localMedia = latest.current.media.filter((item) => !item.source);

          update({
            remoteInitialized: true,
            ...(variants.length
              ? { variants: [...variants, ...newVariants], selectedVariantId: variants[0].id }
              : {}),
            ...(serverMedia.current.length
              ? {
                  media: [
                    ...[...serverMedia.current]
                      .sort(
                        (a, b) =>
                          Number(b.primary) - Number(a.primary) || a.sortOrder - b.sortOrder,
                      )
                      .map((item) => ({
                        id: `server-${item.id}`,
                        serverId: item.id,
                        source: item.url,
                        name: "Фото товара",
                        type: "image/jpeg",
                      })),
                    ...localMedia,
                  ],
                }
              : {}),
          });
        }
        update({
          media: latest.current.media.filter(
            (item) => !item.serverId || !ownedIds.has(item.serverId),
          ),
          variants: latest.current.variants?.map((item) =>
            item.serverId && !item.mediaInitialized
              ? {
                  ...item,
                  media: [
                    ...asDraft(serverVariantMedia.current.get(item.serverId) || []),
                    ...(item.media || []).filter((photo) => !photo.source),
                  ],
                  mediaInitialized: true,
                }
              : item,
          ),
        });
        setLoading(false);
        setRemoteLoaded(true);
        setRemoteError("");
      })
      .catch((cause) => {
        if (active) {
          setLoading(false);
          setRemoteLoaded(false);
          setRemoteError(
            (cause as Error).name === "TimeoutError"
              ? "Тестовый сервер не ответил за 15 секунд при загрузке вариантов или фотографий. Черновик сохранён. Повторите загрузку."
              : `Не удалось загрузить варианты или фотографии: ${(cause as Error).message}`,
          );
        }
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [productId, initial.draft.updatedAt, update, remoteRevision]);
  useEffect(() => {
    if (!draft.categoryId) return;
    let active = true;
    api<{ attributes: CategoryAttribute[] }>(
      `/api/v1/admin/categories/${draft.categoryId}/attributes`,
      { signal: AbortSignal.timeout(15000) },
    )
      .then((data) => {
        if (active) {
          setSchema(data.attributes || []);
          setSchemaCategoryId(draft.categoryId);
          setSchemaError("");
        }
      })
      .catch(() => {
        if (active) setSchemaError("Не удалось загрузить характеристики категории.");
      });
    return () => {
      active = false;
    };
  }, [draft.categoryId, schemaRevision]);
  useEffect(() => {
    if (!saveError && !mediaBusy && !saving) return;
    const protect = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, [saveError, mediaBusy, saving]);
  useEffect(() => {
    if (!productId || !remoteLoaded) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const read = () =>
      api<{
        config: {
          variants: Array<{
            variantId: number;
            priceMode?: "auto" | "manual";
            ozonPrice: string;
            wbPrice: string;
            saby: SabyEditorConfig;
            marketplace: MarketplaceCommon;
          }>;
        };
        publications: Array<{
          variantId: number;
          channel: string;
          status: string;
          message: string;
          externalIds?: { productId: string; offerId?: string };
        }>;
        links: Record<string, Partial<Record<"wb" | "ozon", string>>>;
        sabyLinks?: Record<string, SabyReadLink>;
        publicationMode: string;
        barcodes: Record<string, string>;
      }>(`/api/v1/admin/products/${productId}/editor-plan`)
        .then((data) => {
          if (!active) return;
          setChannelLinks(data.links || {});
          setSabyLinks(data.sabyLinks || {});
          setPublicationMode(data.publicationMode || "disabled");
          setChannelStatuses(data.publications || []);
          setChannelBarcodes(data.barcodes || {});
          const current = latest.current;
          const hydrated = current.variants?.map((row) => {
            const saved = data.config?.variants?.find((v) => v.variantId === row.serverId);
            const linked =
              data.sabyLinks?.[String(row.serverId)] ||
              (row.sabyId
                ? {
                    id: row.sabyId,
                    code: "",
                    category: blankCategory(),
                    folder: blankCategory(),
                  }
                : undefined);
            const sabyEditor =
              row.sabyEditor || saved?.saby || linked
                ? mergeSabyEditor({
                    local: row.sabyEditor,
                    saved: saved?.saby,
                    linked,
                    touched: row.sabyEditorTouched,
                    defaults: { category: current.sabyCategory, folder: current.sabyFolder },
                  })
                : undefined;
            // Older plans stored amounts without their mode. Preserve that
            // intent until the owner explicitly chooses Auto; do not infer that
            // an old amount came from the current formula.
            const savedPriceMode =
              saved?.priceMode ||
              ([saved?.ozonPrice, saved?.wbPrice].some(
                (value) => typeof value === "string" && value.trim() !== "",
              )
                ? "manual"
                : undefined);
            // Formula previews may have filled a fresh draft before the plan
            // arrives. Later polls must preserve local edits, including zero.
            const restoreManualPrices =
              row.marketplacePriceMode === undefined && savedPriceMode === "manual";
            const next = {
              ...row,
              ...(restoreManualPrices && typeof saved?.ozonPrice === "string"
                ? { ozon: { ...(row.ozon || current.ozon), price: saved.ozonPrice } }
                : {}),
              ...(restoreManualPrices && typeof saved?.wbPrice === "string"
                ? { wb: { ...(row.wb || current.wb), price: saved.wbPrice } }
                : {}),
              marketplacePriceMode: row.marketplacePriceMode ?? savedPriceMode,
              marketplace: row.marketplace || saved?.marketplace,
              sabyEditor,
            };
            return JSON.stringify(next) === JSON.stringify(row) ? row : next;
          });
          if (hydrated?.some((row, index) => row !== current.variants?.[index]))
            update({ variants: hydrated });
          if (
            data.publications?.some(
              (p) => p.status === "prepared" || p.status === "queued" || p.status === "processing",
            )
          )
            timer = setTimeout(() => void read(), 3000);
        })
        .catch(() => {});
    void read();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [productId, update, remoteLoaded, collectionsRevision]);
  const categorySection =
    rootCategory(categories, draft.categoryId)?.slug || product?.catalogSection;
  const plant = categorySection === "plants";
  const categoryLoadMessage =
    catalogError ||
    (!catalogLoading && draft.categoryId && !rootCategory(categories, draft.categoryId)
      ? "Выбранная категория недоступна. Выберите другую или повторите загрузку категорий."
      : "");
  const metadataReady = Boolean(
    !catalogLoading &&
      !catalogError &&
      draft.categoryId &&
      rootCategory(categories, draft.categoryId)?.slug &&
      schemaCategoryId === draft.categoryId &&
      !schemaError,
  );
  const submissionSection = () => {
    const current = latest.current;
    const section = rootCategory(categories, current.categoryId)?.slug;
    if (
      catalogLoading ||
      catalogError ||
      !section ||
      schemaCategoryId !== current.categoryId ||
      schemaError
    ) {
      setRemoteError(
        categoryLoadMessage ||
          schemaError ||
          "Дождитесь загрузки категории и её полей. Черновик сохранён.",
      );
      return;
    }
    return section;
  };
  const variants = (draft.variants?.length ? draft.variants : [seedVariant(draft)]).map((item) => ({
    ...item,
    label: categoryVariantName(item, categorySection),
  }));
  const variant = variants.find((item) => item.id === draft.selectedVariantId) || variants[0];
  const marketplaceLinkKey = JSON.stringify(channelLinks[String(variant.serverId)] || {});
  useEffect(() => {
    const variantId = variant.serverId;
    if (!variantId || !productId || !remoteLoaded || marketplaceLinkKey === "{}") {
      const reset = setTimeout(() => {
        setMarketplaceCardBusy(false);
        setMarketplaceCardError("");
      }, 0);
      return () => clearTimeout(reset);
    }
    const key = `articles-v2:${variantId}:${marketplaceLinkKey}:${marketplaceCardRevision}`;
    if (loadedMarketplaceCards.current.has(key)) {
      const reset = setTimeout(() => {
        setMarketplaceCardBusy(false);
        setMarketplaceCardError("");
      }, 0);
      return () => clearTimeout(reset);
    }
    let active = true;
    const timer = setTimeout(() => {
      setMarketplaceCardBusy(true);
      setMarketplaceCardError("");
      api<{
        cards: Partial<Record<"wb" | "ozon", RemoteMarketplaceCard>>;
        errors: Record<string, string>;
      }>(`/api/v1/admin/products/${productId}/marketplace-cards?variantId=${variantId}`)
        .then((data) => {
          if (!active) return;
          const current = latest.current;
          const rows = current.variants || [];
          const row = rows.find((item) => item.serverId === variantId);
          if (!row) return;
          const value = marketplaceOf(row, current);
          const merged = mergeMarketplaceCards(value, data.cards, {
            name: current.name,
            description: current.description,
            originalName: original.current?.name,
            originalDescription: original.current?.description,
            sku: row.sku,
          });
          if (JSON.stringify(merged) !== JSON.stringify(value))
            update({
              variants: rows.map((item) =>
                item.serverId === variantId ? { ...item, marketplace: merged } : item,
              ),
            });
          if (Object.keys(data.errors).length === 0) loadedMarketplaceCards.current.add(key);
          setMarketplaceCardError(
            Object.entries(data.errors)
              .map(([channel, error]) => `${channel === "wb" ? "Wildberries" : "Ozon"}: ${error}`)
              .join(". "),
          );
        })
        .catch((e) => {
          if (active) setMarketplaceCardError((e as Error).message);
        })
        .finally(() => {
          if (active) setMarketplaceCardBusy(false);
        });
    }, 0);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [
    variant.serverId,
    productId,
    remoteLoaded,
    marketplaceLinkKey,
    marketplaceCardRevision,
    update,
  ]);
  const manualMarketplacePrices = variant.marketplacePriceMode === "manual";
  const pricingKey = JSON.stringify({
    id: variant.id,
    price: variant.price,
    attributes: Object.fromEntries(
      [
        "package_length_cm",
        "package_width_cm",
        "package_height_cm",
        "package_winter_length_cm",
        "package_winter_width_cm",
        "package_winter_height_cm",
      ].map((code) => [code, variant.attributes[code] ?? null]),
    ),
  });
  const applyCalculatedPrices = useCallback(
    (key: string, prices: { wb: string; ozon: string }) => {
      const input = JSON.parse(key);
      const current = latest.current;
      const row = current.variants?.find((item) => item.id === input.id);
      if (
        !row ||
        row.marketplacePriceMode === "manual" ||
        row.removePending ||
        row.price !== input.price ||
        Object.entries(input.attributes).some(
          ([code, value]) => (row.attributes[code] ?? null) !== value,
        )
      )
        return;
      if (
        (row.wb || current.wb).price === prices.wb &&
        (row.ozon || current.ozon).price === prices.ozon
      )
        return;
      update({
        variants: current.variants?.map((item) =>
          item.id === row.id
            ? {
                ...item,
                wb: { ...(item.wb || current.wb), price: prices.wb },
                ozon: { ...(item.ozon || current.ozon), price: prices.ozon },
              }
            : item,
        ),
      });
    },
    [update],
  );
  const marketplacePricing = useProductMarketplacePricing(
    pricingKey,
    !manualMarketplacePrices && !variant.removePending && !variant.archived,
    applyCalculatedPrices,
  );
  const changeVariant = (patch: Partial<DraftVariant>) =>
    update({
      variants: (latest.current.variants || variants).map((item) =>
        item.id === variant.id ? { ...item, ...patch } : item,
      ),
    });
  const changeAttributes = (code: string, value: string | number | boolean | string[] | null) =>
    changeVariant({ attributes: { ...variant.attributes, [code]: value } });
  const editorSchema = plant && draft.categoryId ? plantEditorSchema(schema) : schema;
  const productSchema = editorSchema.filter((item) => item.scope === "product");
  const variantSchema = schema.filter(
    (item) => item.scope === "variant" && !/^(package_|shipping_)/.test(item.code),
  );
  const packagingSchema = schema.filter(
    (item) => item.scope === "variant" && /^(package_|shipping_)/.test(item.code),
  );
  const ownGallery = Boolean(variant.mediaCustomized || variant.media?.length);
  const gallery = variantGallery(draft, variant.id);
  const changeGallery = (media: DraftMedia[]) =>
    update(replaceVariantGallery(latest.current, variant.id, media));
  const selectTab = (value: "product" | "channels" | "faq") => {
    setTab(value);
    update({ editorTab: value });
    const url = new URL(location.href);
    url.searchParams.set("editor", value);
    url.searchParams.delete("step");
    history.replaceState(null, "", url.pathname + url.search);
  };
  const close = () => {
    if (mediaBusy || saving) return;
    if (update({})) onClose();
  };
  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setMediaBusy(true);
    setMediaError("");
    try {
      for (const file of Array.from(files)) {
        if (!file.type.startsWith("image/") && !file.type.startsWith("video/")) {
          setMediaError("Добавляйте изображения или видео.");
          continue;
        }
        const id = crypto.randomUUID();
        await saveDraftMedia(id, file);
        const photo: DraftMedia = { id, name: file.name, type: file.type };
        update(appendGalleryMedia(latest.current, variant.id, photo));
      }
    } catch {
      setMediaError("Не удалось сохранить файл. Проверьте свободное место в браузере.");
    } finally {
      setMediaBusy(false);
    }
  };
  const removeMedia = (id: string) => {
    const currentGallery = variantGallery(latest.current, variant.id);
    const item = currentGallery.find((photo) => photo.id === id);
    if (!item) return;
    if (!canManage && item.serverId) return;
    const media = currentGallery.filter((photo) => photo.id !== id);
    const saved = update(replaceVariantGallery(latest.current, variant.id, media));
    if (!saved) return;
    const retained =
      latest.current.media.some((photo) => photo.id === id) ||
      latest.current.variants?.some((row) => row.media?.some((photo) => photo.id === id));
    if (!item.source && !retained)
      void deleteDraftMedia(id).catch(() =>
        setMediaError("Не удалось освободить место в браузере."),
      );
  };
  const addVariant = () => {
    const next: DraftVariant = {
      ...seedVariant({ ...draft, price: variant.price }),
      id: crypto.randomUUID(),
      label: `Вариант ${variants.length + 1}`,
      attributes: {},
      media: [],
      mediaInitialized: true,
      mediaCustomized: true,
      price: variant.price,
      active: false,
      ozon: emptyProductDraft().ozon,
      wb: emptyProductDraft().wb,
    };
    update({ variants: [...variants, next], selectedVariantId: next.id });
  };
  const apply = async (created?: Product) => {
    const targetProduct = created || product;
    const section = submissionSection();
    if (!section) return;
    const plant = section === "plants";
    const variants = (latest.current.variants || []).map((row) => ({
      ...row,
      label: categoryVariantName(row, section),
    }));
    const variant =
      variants.find((row) => row.id === latest.current.selectedVariantId) || variants[0];
    if (!targetProduct || saving || loading || !remoteLoaded || mediaBusy) return;
    if (!canManage) {
      const removesSavedPhoto = (wanted: DraftMedia[], known: ProductMedia[]) =>
        known.some((photo) => !wanted.some((item) => item.serverId === photo.id));
      if (
        variants.some((item) => item.removePending) ||
        removesSavedPhoto(latest.current.media, serverMedia.current) ||
        variants.some(
          (item) =>
            item.serverId &&
            item.mediaInitialized &&
            removesSavedPhoto(
              variantMediaToPersist(latest.current, item.id),
              serverVariantMedia.current.get(item.serverId) || [],
            ),
        )
      ) {
        setRemoteError(
          "Удаление сохранённых вариантов и фотографий доступно владельцу. Восстановите их в черновике перед сохранением.",
        );
        return;
      }
    }
    if (!draft.name.trim() || !draft.categoryId) {
      setRemoteError("Укажите название и категорию товара.");
      return;
    }
    if (draft.faq.some((item) => !item.question.trim() || !item.answer.trim())) {
      setRemoteError("Заполните вопрос и ответ в каждой строке FAQ.");
      return;
    }
    if (plant && variants.some((item) => !item.removePending && !item.archived && !item.label)) {
      setRemoteError("Укажите диаметр горшка для каждого варианта в характеристиках.");
      return;
    }
    if (
      variants.some(
        (item) =>
          !item.removePending &&
          !item.archived &&
          (!item.label.trim() ||
            !item.price.trim() ||
            !Number.isFinite(Number(item.price)) ||
            Number(item.price) < 0 ||
            !Number.isInteger(Number(item.wholesaleMinQty)) ||
            Number(item.wholesaleMinQty) < 1),
      )
    ) {
      setRemoteError("Проверьте названия вариантов, цены и оптовый минимум.");
      return;
    }
    setSaving(true);
    setRemoteError("");
    setRemoteNotice("");
    try {
      const base = original.current!;
      const body: Record<string, unknown> = {};
      if (draft.name !== base.name) body.name = draft.name.trim();
      if (draft.description !== base.description) body.description = draft.description;
      if (draft.categoryId !== base.categoryId) {
        body.categoryId = draft.categoryId;
        body.catalogSection = section;
      }
      if (plant) {
        if (draft.latinName !== base.latinName) body.latinName = draft.latinName;
        if (draft.care !== base.careInstructions) body.careInstructions = draft.care;
        const warnings = (draft.warnings || "")
          .split("\n")
          .map((item) => item.trim())
          .filter(Boolean);
        if (warnings.length > 4) throw new Error("Оставьте не более четырёх предупреждений.");
        if (JSON.stringify(warnings) !== JSON.stringify(base.importantWarnings || []))
          body.importantWarnings = warnings;
      }
      const faq = draft.faq.map(({ question, answer }) => ({ question, answer }));
      const passport = { ...base.passport, ...(plant ? draft.passport : {}), faq };
      if (JSON.stringify(passport) !== JSON.stringify(base.passport || {}))
        body.passport = passport;
      const attrs = attributeDelta(productSchema, draft.attributes, base.attributes || {});
      if (Object.keys(attrs).length) body.attributes = attrs;
      const edits = [];
      for (const item of variants) {
        if (item.removePending && item.serverId) {
          edits.push({ id: item.serverId, remove: true });
          continue;
        }
        const old = serverVariants.current.find((row) => row.id === item.serverId);
        if (!old) {
          if (item.serverId)
            throw new Error("Список вариантов изменился. Откройте карточку заново.");
          if (!canManage) throw new Error("Создание вариантов доступно владельцу.");
          edits.push({
            id: -(edits.length + 1),
            label: item.label,
            priceMinor: Math.round(Number(item.price) * 100),
            wholesaleMinQty: Number(item.wholesaleMinQty),
            active: item.active,
            attributes: attributeDelta(
              schema.filter((field) => field.scope === "variant"),
              item.attributes,
              {},
            ),
            externalIds: [],
          });
          continue;
        }
        const attributes = attributeDelta(
          schema.filter((field) => field.scope === "variant"),
          item.attributes,
          old.attributes,
        );
        if (
          item.label === old.label &&
          Number(item.price) === old.price &&
          Number(item.wholesaleMinQty) === old.wholesaleMinQty &&
          item.active === old.active &&
          item.archived === old.archived &&
          !Object.keys(attributes).length
        )
          continue;
        edits.push(
          canManage
            ? {
                id: item.serverId,
                label: item.label,
                priceMinor: Math.round(Number(item.price) * 100),
                wholesaleMinQty: Number(item.wholesaleMinQty),
                active: item.active,
                archived: item.archived,
                attributes,
                externalIds: old.externalIds,
              }
            : { id: item.serverId, label: item.label, attributes },
        );
      }
      if (edits.length) body.variants = edits;
      if (Object.keys(body).length) {
        const result = await api<{ product: Product }>(
          `/api/v1/admin/products/${targetProduct.id}`,
          {
            method: "PATCH",
            body: JSON.stringify(body),
          },
        );
        original.current = result.product;
        if (!created) onSaved?.(result.product);
      }
      const rows = await api<{ variants: AdminVariant[] }>(
        `/api/v1/admin/products/${targetProduct.id}/variants`,
      );
      serverVariants.current = rows.variants || [];
      const selectedIndex = variants.findIndex((item) => item.id === variant.id);
      const nextVariants = serverVariants.current.map((row) => {
        const previous =
          variants.find((item) => item.serverId === row.id) ||
          variants.find((item) => !item.serverId && item.label === row.label);
        return {
          ...variantToDraft(row),
          ...(previous
            ? {
                marketplacePriceMode: previous.marketplacePriceMode,
                marketplace: previous.marketplace,
                sabyEditor: previous.sabyEditor,
                sabyEditorTouched: previous.sabyEditorTouched,
                ozon: previous.ozon,
                wb: previous.wb,
                media: previous.media,
                mediaInitialized: previous.mediaInitialized,
                mediaCustomized: previous.mediaCustomized,
              }
            : {}),
        };
      });
      update({
        variants: nextVariants,
        selectedVariantId: nextVariants[selectedIndex]?.id || nextVariants[0]?.id,
      });
      const syncGallery = async (
        initialItems: DraftMedia[],
        known: ProductMedia[],
        path: string,
        primaryMethod: "POST" | "PATCH",
        persist: (items: DraftMedia[]) => void,
      ) => {
        let items = [...initialItems];
        const synced = [...known];
        for (const item of [...items]) {
          if (item.source) {
            if (known.some((row) => row.id === item.serverId)) continue;
            if (!item.serverId) throw new Error("Не удалось определить исходную фотографию.");
            const copied = await api<{ media: ProductMedia }>(`${path}/copy`, {
              method: "POST",
              body: JSON.stringify({ mediaId: item.serverId }),
            });
            synced.push(copied.media);
            items = items.map((photo) =>
              photo.id === item.id
                ? { ...photo, source: copied.media.url, serverId: copied.media.id }
                : photo,
            );
            persist(items);
            continue;
          }
          const blob = await readDraftMedia(item.id);
          if (!blob) throw new Error(`Не найден файл ${item.name}. Добавьте его ещё раз.`);
          if (!item.type.startsWith("image/"))
            throw new Error(
              "Текст и варианты сохранены. Видео осталось в черновике: его отправка пока недоступна.",
            );
          let uploadBlob = blob;
          if (!["image/jpeg", "image/png", "image/gif"].includes(blob.type)) {
            const bitmap = await createImageBitmap(blob);
            const canvas = document.createElement("canvas");
            canvas.width = bitmap.width;
            canvas.height = bitmap.height;
            const context = canvas.getContext("2d");
            if (!context) {
              bitmap.close();
              throw new Error("Не удалось подготовить фотографию.");
            }
            context.fillStyle = "#fff";
            context.fillRect(0, 0, canvas.width, canvas.height);
            context.drawImage(bitmap, 0, 0);
            bitmap.close();
            uploadBlob = await new Promise<Blob>((resolve, reject) =>
              canvas.toBlob(
                (result) =>
                  result
                    ? resolve(result)
                    : reject(new Error("Не удалось подготовить фотографию.")),
                "image/jpeg",
                0.92,
              ),
            );
          }
          const formData = new FormData();
          formData.append("file", uploadBlob, item.name);
          const uploaded = await api<{ media: ProductMedia }>(path, {
            method: "POST",
            body: formData,
          });
          synced.push(uploaded.media);
          items = items.map((photo) =>
            photo.id === item.id
              ? { ...photo, source: uploaded.media.url, serverId: uploaded.media.id }
              : photo,
          );
          persist(items);
        }
        const wantedIds = new Set(items.map((item) => item.serverId).filter(Boolean));
        for (const removed of [...synced]) {
          if (wantedIds.has(removed.id)) continue;
          if (!canManage) throw new Error("Удаление сохранённых фотографий доступно владельцу.");
          await api(`${path}/${removed.id}`, { method: "DELETE" });
          synced.splice(
            synced.findIndex((item) => item.id === removed.id),
            1,
          );
        }
        const cover = items.find((item) => item.type.startsWith("image/"));
        if (cover?.serverId && !synced.find((item) => item.id === cover.serverId)?.primary)
          await api(`${path}/${cover.serverId}/primary`, {
            method: primaryMethod,
            ...(primaryMethod === "PATCH" ? { body: "{}" } : {}),
          });
        return synced;
      };
      serverMedia.current = await syncGallery(
        latest.current.media,
        serverMedia.current,
        `/api/v1/admin/products/${targetProduct.id}/media`,
        "POST",
        (media) => {
          const cover = commonCover(latest.current);
          update({
            media,
            cover: cover ? media.find((item) => item.id === cover.id) || cover : null,
          });
        },
      );
      for (const item of latest.current.variants || []) {
        if (!item.serverId || !item.mediaInitialized || item.archived || item.removePending)
          continue;
        const media = await syncGallery(
          variantMediaToPersist(latest.current, item.id),
          serverVariantMedia.current.get(item.serverId) || [],
          `/api/v1/admin/variants/${item.serverId}/media`,
          "PATCH",
          (media) =>
            update({
              variants: latest.current.variants?.map((row) =>
                row.id === item.id ? { ...row, media } : row,
              ),
            }),
        );
        serverVariantMedia.current.set(item.serverId, media);
      }
      const primary = commonCover(latest.current);
      if (original.current && primary?.source) {
        original.current = { ...original.current, image: primary.source };
        if (!created) onSaved?.(original.current);
      }
      for (const [collectionId, included] of Object.entries(
        latest.current.collectionMemberships || {},
      )) {
        await api(`/api/v1/admin/products/${targetProduct.id}/collections/${collectionId}`, {
          method: "PUT",
          body: JSON.stringify({ included }),
        });
      }
      const planVariants = (latest.current.variants || [])
        .filter((row) => row.serverId && !row.archived && !row.removePending)
        .map((row) => ({
          variantId: row.serverId,
          priceMode: row.marketplacePriceMode || "auto",
          saby: sabyOf(row, latest.current),
          marketplace: marketplaceOf(row, latest.current),
          ozonPrice: (row.ozon || latest.current.ozon).price,
          wbPrice: (row.wb || latest.current.wb).price,
        }));
      if (canManage) {
        await api(`/api/v1/admin/products/${targetProduct.id}/editor-plan`, {
          method: "PUT",
          body: JSON.stringify({ config: { variants: planVariants }, submit: true }),
        });
      } else {
        try {
          const sync = await api<{ warnings?: string[] }>(
            `/api/v1/admin/products/${targetProduct.id}/editor-plan/sync-existing`,
            { method: "POST", body: "{}" },
          );
          if (sync.warnings?.length) setRemoteError(sync.warnings.join(" "));
        } catch (cause) {
          throw new Error(
            `Изменения товара сохранены. Не удалось поставить обновление площадок в очередь: ${(cause as Error).message}. Повторите сохранение.`,
          );
        }
      }
      setCollectionsRevision((value) => value + 1);
      const removed = variants.filter((item) => item.removePending && item.serverId);
      const archived = removed.filter((item) =>
        serverVariants.current.some((row) => row.id === item.serverId && row.archived),
      );
      const deleted = removed.filter(
        (item) => !serverVariants.current.some((row) => row.id === item.serverId),
      );
      if (removed.length !== archived.length + deleted.length)
        throw new Error(
          "Сервер не обработал удаление вариантов. Для этой функции нужен обновлённый локальный бэкенд.",
        );
      if (created) {
        saveProductDraft(
          { ...latest.current, remoteInitialized: true },
          `ficusin:product-edit-draft:v1:${targetProduct.id}`,
        );
        // The receipt remains recoverable until every creation step succeeds.
        // Afterwards the next new card needs its own draft and creation identity.
        localStorage.removeItem(draftStorageKey);
        onSaved?.(original.current || targetProduct);
      }
      setRemoteNotice(
        `Изменения товара сохранены.${deleted.length ? ` Удалено вариантов: ${deleted.length}.` : ""}${archived.length ? ` В архиве из-за связей: ${archived.map((item) => item.label || item.sku).join(", ")}.` : ""}${canManage ? " Запущена подготовка выбранных площадок." : ""}`,
      );
    } catch (cause) {
      setRemoteError((cause as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const createProduct = async () => {
    if (saving || mediaBusy) return;
    const current = latest.current;
    if (!current.name.trim() || !current.categoryId) {
      setRemoteError("Укажите название и категорию товара.");
      return;
    }
    const section = submissionSection();
    if (!section) return;
    const currentVariants = (current.variants || []).filter(
      (row) => !row.removePending && !row.archived,
    );
    for (const row of currentVariants) {
      const saby = sabyOf(row, current);
      if (saby.mode === "link" ? !saby.existingCode.trim() : !saby.category.id || !saby.folder.id) {
        setRemoteError("Выберите категорию и раздел СБИС или укажите код уже созданного товара.");
        return;
      }
    }
    // Persist identity before the first request. If its response is lost, the
    // same draft replays the server receipt instead of creating another card.
    const creationId = current.creationId || crypto.randomUUID();
    if (!current.creationId && !update({ creationId })) {
      setRemoteError(
        "Не удалось сохранить ключ создания. Освободите хранилище браузера и повторите.",
      );
      return;
    }
    setSaving(true);
    setRemoteError("");
    try {
      let created: Product;
      if (current.remoteCreatedProductId) {
        const result = await api<{ products: Product[] }>("/api/v1/admin/products");
        const found = result.products.find((p) => p.id === current.remoteCreatedProductId);
        if (!found) throw new Error("Созданная карточка не найдена");
        created = found;
      } else {
        const first = currentVariants[0];
        if (!first) throw new Error("Добавьте вариант товара");
        const result = await api<{ product: Product }>("/api/v1/admin/products", {
          method: "POST",
          body: JSON.stringify({
            creationId,
            name: current.name,
            description: current.description,
            latinName: current.latinName,
            categoryId: current.categoryId,
            catalogSection: section,
            priceMinor: Math.round(Number(first.price) * 100),
            attributes: current.attributes,
            variantAttributes: first.attributes,
          }),
        });
        created = result.product;
        update({ remoteCreatedProductId: created.id });
        const initial = await api<{ variants: AdminVariant[] }>(
          `/api/v1/admin/products/${created.id}/variants`,
        );
        let mapped = [{ ...first, serverId: initial.variants[0].id }];
        for (const row of currentVariants.slice(1)) {
          const result = await api<{ variant: AdminVariant }>(
            `/api/v1/admin/products/${created.id}/variants`,
            {
              method: "POST",
              body: JSON.stringify({
                label: categoryVariantName(row, categorySection) || row.label,
                priceMinor: Math.round(Number(row.price) * 100),
                wholesaleMinQty: Number(row.wholesaleMinQty) || 1,
                active: false,
                attributes: row.attributes,
              }),
            },
          );
          mapped = [...mapped, { ...row, serverId: result.variant.id }];
        }
        update({ variants: mapped, remoteInitialized: true });
      }
      original.current = created;
      const rows = await api<{ variants: AdminVariant[] }>(
        `/api/v1/admin/products/${created.id}/variants`,
      );
      serverVariants.current = rows.variants;
      update({
        variants: latest.current.variants?.map((row, i) => ({
          ...row,
          serverId: row.serverId || rows.variants[i]?.id,
        })),
      });
      setSaving(false);
      await apply(created);
    } catch (error) {
      setRemoteError((error as Error).message);
      setSaving(false);
    }
  };
  const marketplace = marketplaceOf(variant, draft);
  const linkedMarketplaces = channelLinks[String(variant.serverId)] || {};
  const creationChannels = (
    [
      ["ozon", "Ozon"],
      ["wb", "Wildberries"],
    ] as const
  ).filter(([channel]) => !linkedMarketplaces[channel] && marketplace[channel].mode === "create");
  const sabyEditor = sabyOf(variant, draft);
  const changeMarketplace = (value: MarketplaceCommon) => {
    changeVariant({ marketplace: value });
    setRemoteError((message) => (message.startsWith("Заполните:") ? "" : message));
  };
  const changeSaby = (value: SabyEditorConfig, field: SabyEditorField) => {
    const current = latest.current;
    update({
      ...(field === "category" ? { sabyCategory: value.category } : {}),
      ...(field === "folder" ? { sabyFolder: value.folder } : {}),
      variants: (current.variants || variants).map((row) =>
        row.id === variant.id
          ? {
              ...row,
              sabyEditor: { ...sabyOf(row, current), [field]: value[field] },
              sabyEditorTouched: { ...row.sabyEditorTouched, [field]: true },
            }
          : row,
      ),
    });
  };
  const marketplacePriceField = (provider: "wb" | "ozon", label: string) => {
    return (
      <label>
        {label}
        <NumericInput
          readOnly={
            !manualMarketplacePrices || !canManage || variant.removePending || variant.archived
          }
          min="0"
          step="0.01"
          value={
            manualMarketplacePrices || variant.removePending || variant.archived
              ? (variant[provider] || draft[provider]).price
              : marketplacePricing.result?.[provider] || ""
          }
          placeholder={
            manualMarketplacePrices
              ? "Укажите цену"
              : marketplacePricing.result
                ? "Нет расчёта"
                : "Рассчитываем…"
          }
          onChange={(event) =>
            changeVariant({
              [provider]: { ...(variant[provider] || draft[provider]), price: event.target.value },
            })
          }
        />
      </label>
    );
  };
  const priceField = (
    <label>
      Цена на сайте, ₽ *
      <NumericInput
        required
        disabled={Boolean(product) && !canManage}
        min="0"
        step="0.01"
        value={variant.price}
        onChange={(event) => {
          changeVariant({ price: event.target.value });
          if (
            event.target.value.trim() &&
            Number.isFinite(Number(event.target.value)) &&
            Number(event.target.value) >= 0
          )
            setRemoteError((message) =>
              message.startsWith("Укажите цену на сайте") ? "" : message,
            );
        }}
      />
    </label>
  );
  return (
    <div
      className="product-creation-page editor-page"
      data-refresh-blocked={saving || mediaBusy || !!saveError ? "true" : undefined}
    >
      <header className="editor-heading">
        <div>
          <h1>{product ? draft.name || "Редактирование товара" : "Новый товар"}</h1>
          <p>
            {product
              ? `Артикул ${variant.sku || product.sku} · ${product.status === "published" ? "Опубликован" : "Черновик"}`
              : "Черновик · ещё не опубликован"}
          </p>
        </div>
        <div className="editor-heading-actions">
          <span role="status">
            {saveError
              ? "Не сохранено"
              : mediaBusy
                ? "Сохраняем файлы…"
                : draft.updatedAt
                  ? "Черновик сохранён"
                  : "Автосохранение включено"}
          </span>
          <button
            type="button"
            className="editor-close"
            aria-label="Закрыть редактор"
            disabled={mediaBusy || saving}
            onClick={close}
          >
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="m6 6 12 12M18 6 6 18" />
            </svg>
          </button>
        </div>
      </header>
      {(saveError || remoteError) && (
        <p role="alert" className="editor-alert">
          {saveError || remoteError}
          {product && !remoteLoaded && !loading && (
            <button
              type="button"
              className="admin-action"
              onClick={() => {
                setLoading(true);
                setRemoteError("");
                setRemoteRevision((value) => value + 1);
              }}
            >
              Повторить загрузку
            </button>
          )}
        </p>
      )}
      {remoteNotice && (
        <p role="status" className="editor-notice">
          {remoteNotice}
        </p>
      )}
      <div className="editor-tabs" role="tablist" aria-label="Разделы товара">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "product"}
          onClick={() => selectTab("product")}
        >
          Товар
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "channels"}
          onClick={() => selectTab("channels")}
        >
          Маркетплейсы
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "faq"}
          onClick={() => selectTab("faq")}
        >
          Вопросы и ответы
        </button>
      </div>
      <section className="editor-variants" aria-label="Варианты товара">
        <div>
          <h2>
            Варианты товара <span>{variants.length}</span>
          </h2>
          <span>Размер, фасовка или исполнение</span>
        </div>
        <div className="editor-variant-buttons">
          {variants.map((item) => (
            <div
              className="editor-variant-card"
              key={item.id}
              data-removable={
                (canManage && !item.archived && (Boolean(item.serverId) || variants.length > 1)) ||
                undefined
              }
            >
              <button
                type="button"
                aria-pressed={variant.id === item.id}
                onClick={() => update({ selectedVariantId: item.id })}
              >
                <strong>
                  {item.label || "Диаметр не указан"}
                  {item.removePending
                    ? " · будет удалён или архивирован"
                    : item.archived
                      ? " · архив"
                      : ""}
                </strong>
                <small>
                  {money.format(Number(item.price) || 0)}
                  {item.sku ? ` · ${item.sku}` : ""}
                </small>
              </button>
              {canManage && !item.archived && (Boolean(item.serverId) || variants.length > 1) && (
                <button
                  type="button"
                  className="editor-remove-variant"
                  aria-label={`${item.removePending ? "Отменить удаление варианта" : "Удалить вариант"} ${item.label || "без названия"}`}
                  title={
                    item.removePending
                      ? "Отменить удаление"
                      : "Удалить или архивировать при сохранении"
                  }
                  disabled={saving || mediaBusy}
                  onClick={() =>
                    update({
                      variants: item.serverId
                        ? variants.map((row) =>
                            row.id === item.id
                              ? { ...row, removePending: !row.removePending }
                              : row,
                          )
                        : variants.filter((row) => row.id !== item.id),
                      selectedVariantId:
                        variant.id === item.id
                          ? variants.find(
                              (row) => row.id !== item.id && !row.removePending && !row.archived,
                            )?.id || item.id
                          : variant.id,
                    })
                  }
                >
                  {item.removePending ? (
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      aria-hidden="true"
                    >
                      <path d="M9 10H4V5m0 5 4-4a7 7 0 1 1-1 11" />
                    </svg>
                  ) : (
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      aria-hidden="true"
                    >
                      <path d="m6 6 12 12M18 6 6 18" />
                    </svg>
                  )}
                </button>
              )}
            </div>
          ))}
          <button
            type="button"
            className="editor-add-variant"
            disabled={Boolean(product) && !canManage}
            onClick={addVariant}
          >
            + Добавить вариант
          </button>
        </div>
      </section>
      <div className="editor-layout">
        <ProductPhotoGallery
          items={gallery}
          cover={commonCover(draft)}
          variantLabel={variant.label || "Диаметр не указан"}
          inherited={!ownGallery && gallery.length > 0}
          busy={mediaBusy || saving}
          disabled={loading || (Boolean(product) && !remoteLoaded)}
          canRemove={(item) => canManage || !item.serverId}
          error={mediaError}
          onChange={changeGallery}
          onRemove={removeMedia}
          onUpload={(files) => void upload(files)}
        />
        <div
          className="editor-information"
          key={tab}
          role="tabpanel"
          aria-label={
            tab === "product" ? "Товар" : tab === "faq" ? "Вопросы и ответы" : "Маркетплейсы"
          }
        >
          {tab === "product" ? (
            <>
              <section className="editor-section">
                <h2>Основное</h2>
                <div className="editor-fields editor-main-fields">
                  <label className="wide">
                    Название товара *
                    <input
                      className="editor-product-name"
                      value={draft.name}
                      onChange={(event) => update({ name: event.target.value })}
                      placeholder="Название товара"
                    />
                  </label>
                  <div className="wide admin-field">
                    <span className="admin-field-label">Категория на сайте *</span>
                    {categoryLoadMessage && (
                      <p role="alert">
                        {categoryLoadMessage}{" "}
                        <button
                          type="button"
                          className="admin-action"
                          onClick={() => {
                            setCatalogLoading(true);
                            setCatalogError("");
                            setRemoteError("");
                            setCatalogRevision((value) => value + 1);
                          }}
                        >
                          Повторить загрузку категорий
                        </button>
                      </p>
                    )}
                    <CategoryPicker
                      categories={categories}
                      value={draft.categoryId}
                      onChange={(categoryId) => {
                        if (categoryId === draft.categoryId) return;
                        setSchema([]);
                        setSchemaCategoryId(undefined);
                        setSchemaError("");
                        update({ categoryId });
                      }}
                    />
                  </div>
                  {product && !remoteLoaded ? (
                    <div className="editor-saby-fields wide" role="status" aria-busy={loading}>
                      <p className="editor-secondary">
                        {loading
                          ? "Загружаем данные связи СБИС…"
                          : "Данные связи СБИС пока не загружены."}
                      </p>
                    </div>
                  ) : (
                    <fieldset
                      className="editor-protected-fields wide"
                      disabled={!canManage}
                      aria-label="Связь с СБИС"
                    >
                      <SabyEditorFields
                        value={sabyEditor}
                        onChange={changeSaby}
                        linked={
                          sabyLinks[String(variant.serverId)] ||
                          (variant.sabyId ? { id: variant.sabyId, code: "" } : undefined)
                        }
                        barcode={channelBarcodes[String(variant.serverId)]}
                      />
                    </fieldset>
                  )}
                  <label className="wide">
                    {plant ? "О растении" : "Описание"}
                    <AutoTextarea
                      rows={5}
                      value={draft.description}
                      onChange={(event) => update({ description: event.target.value })}
                    />
                  </label>
                  {plant && (
                    <label>
                      Латинское название
                      <input
                        value={draft.latinName}
                        onChange={(event) => update({ latinName: event.target.value })}
                      />
                    </label>
                  )}
                </div>
              </section>
              <section className="editor-section">
                <h2>Цена и вариант</h2>
                <div className="editor-fields">
                  {!plant && (
                    <label>
                      Название варианта
                      <input
                        value={variant.label}
                        onChange={(event) => changeVariant({ label: event.target.value })}
                        placeholder="Например, пакет 5 л"
                      />
                    </label>
                  )}
                  {priceField}
                  <p className="editor-secondary">
                    Цены площадок подтверждаются отдельно во вкладке «Маркетплейсы» после
                    сохранения.
                  </p>
                  <label>
                    Оптовый минимум, шт.
                    <NumericInput
                      disabled={!canManage}
                      min="1"
                      step="1"
                      value={variant.wholesaleMinQty}
                      onChange={(event) => changeVariant({ wholesaleMinQty: event.target.value })}
                    />
                  </label>
                  {marketplacePriceField("wb", "Цена Wildberries, ₽")}
                  {marketplacePriceField("ozon", "Цена Ozon, ₽")}
                  <div
                    className="editor-marketplace-price-mode"
                    role="group"
                    aria-label="Режим цен маркетплейсов"
                  >
                    {(
                      [
                        ["auto", "Авто"],
                        ["manual", "Вручную"],
                      ] as const
                    ).map(([mode, label]) => (
                      <button
                        type="button"
                        key={mode}
                        disabled={!canManage}
                        aria-pressed={(variant.marketplacePriceMode || "auto") === mode}
                        onClick={() => changeVariant({ marketplacePriceMode: mode })}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                  <div className="editor-marketplace-pricing-status" role="status">
                    {variant.removePending ? (
                      "Вариант будет удалён или архивирован после сохранения"
                    ) : variant.archived ? (
                      "Вариант в архиве"
                    ) : manualMarketplacePrices ? (
                      "Цены заданы вручную"
                    ) : marketplacePricing.result ? (
                      <>
                        {marketplacePricing.result.notes.map((note) => (
                          <p key={note}>{note}</p>
                        ))}
                        {marketplacePricing.result.error && (
                          <p>{marketplacePricing.result.error}</p>
                        )}
                        {(marketplacePricing.result.error ||
                          !marketplacePricing.result.wb ||
                          !marketplacePricing.result.ozon) && (
                          <button
                            className="admin-action"
                            type="button"
                            onClick={marketplacePricing.retry}
                          >
                            Повторить расчёт
                          </button>
                        )}
                      </>
                    ) : (
                      "Рассчитываем цены WB и Ozon…"
                    )}
                  </div>
                </div>
              </section>
              <section className="editor-section">
                <h2>Габариты упаковки</h2>
                {packagingSchema.length ? (
                  <ProductPackagingEditor
                    schema={packagingSchema}
                    values={variant.attributes}
                    onChange={changeAttributes}
                  />
                ) : (
                  <p className="editor-secondary">Поля упаковки появятся после выбора категории.</p>
                )}
              </section>
              <section className="editor-section">
                <h2>{plant ? "Характеристики растения" : "Характеристики"}</h2>
                {!draft.categoryId ? (
                  <p className="editor-secondary">
                    Выберите категорию, чтобы появились характеристики товара.
                  </p>
                ) : schemaError ? (
                  <p role="alert" className="editor-alert">
                    {schemaError}{" "}
                    <button
                      type="button"
                      className="admin-action"
                      onClick={() => {
                        setSchemaCategoryId(undefined);
                        setSchemaError("");
                        setRemoteError("");
                        setSchemaRevision((value) => value + 1);
                      }}
                    >
                      Повторить загрузку характеристик
                    </button>
                  </p>
                ) : plant ? (
                  <PlantCharacteristicsEditor
                    schema={editorSchema}
                    values={Object.fromEntries(
                      editorSchema.map((field) => [
                        field.code,
                        field.scope === "variant"
                          ? variant.attributes[field.code]
                          : draft.attributes[field.code],
                      ]),
                    )}
                    onChange={(code, value) =>
                      editorSchema.find((field) => field.code === code)?.scope === "variant"
                        ? changeAttributes(code, value)
                        : update({ attributes: { ...draft.attributes, [code]: value } })
                    }
                  />
                ) : (
                  <AttributeFields
                    schema={[...productSchema, ...variantSchema]}
                    values={{ ...draft.attributes, ...variant.attributes }}
                    onChange={(code, value) =>
                      variantSchema.some((item) => item.code === code)
                        ? changeAttributes(code, value)
                        : update({ attributes: { ...draft.attributes, [code]: value } })
                    }
                  />
                )}
              </section>
              {plant && (
                <section className="editor-section">
                  <h2>Уход</h2>
                  <PlantCareEditor
                    passport={draft.passport || {}}
                    onChange={(passport) => update({ passport })}
                  />
                  <div className="editor-fields">
                    <CareFieldDisclosure title="На заметку" filled={Boolean(draft.care.trim())}>
                      <AutoTextarea
                        aria-label="На заметку"
                        rows={6}
                        value={draft.care}
                        onChange={(event) => update({ care: event.target.value })}
                      />
                    </CareFieldDisclosure>
                    <CareFieldDisclosure
                      title="Важно перед началом"
                      filled={Boolean((draft.warnings || "").replace(/[•●▪\s]/g, ""))}
                    >
                      <PlantWarningsEditor
                        value={draft.warnings || ""}
                        onChange={(warnings) => update({ warnings })}
                      />
                    </CareFieldDisclosure>
                  </div>
                </section>
              )}
              <section className="editor-section">
                <h2>Подборки</h2>
                <ProductCollectionsEditor
                  productId={productId}
                  membership={draft.collectionMemberships || {}}
                  onChange={(collectionMemberships) => update({ collectionMemberships })}
                  revision={collectionsRevision}
                  disabled={saving}
                />
              </section>
            </>
          ) : tab === "faq" ? (
            <section className="editor-section">
              <h2>Вопросы и ответы</h2>
              {draft.faq.map((item, i) => (
                <fieldset className="editor-faq" key={item.id}>
                  <legend>Вопрос {i + 1}</legend>
                  <label>
                    Вопрос
                    <input
                      value={item.question}
                      onChange={(event) =>
                        update({
                          faq: draft.faq.map((row) =>
                            row.id === item.id ? { ...row, question: event.target.value } : row,
                          ),
                        })
                      }
                    />
                  </label>
                  <label>
                    Ответ
                    <AutoTextarea
                      rows={3}
                      value={item.answer}
                      onChange={(event) =>
                        update({
                          faq: draft.faq.map((row) =>
                            row.id === item.id ? { ...row, answer: event.target.value } : row,
                          ),
                        })
                      }
                    />
                  </label>
                  <button
                    type="button"
                    aria-label={`Удалить вопрос ${i + 1}`}
                    title="Удалить вопрос"
                    onClick={() => update({ faq: draft.faq.filter((row) => row.id !== item.id) })}
                  >
                    <svg
                      width="20"
                      height="20"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2.5"
                      strokeLinecap="round"
                      aria-hidden="true"
                    >
                      <path d="m6 6 12 12M18 6 6 18" />
                    </svg>
                  </button>
                </fieldset>
              ))}
              <button
                type="button"
                className="admin-action"
                onClick={() =>
                  update({
                    faq: [...draft.faq, { id: crypto.randomUUID(), question: "", answer: "" }],
                  })
                }
              >
                Добавить вопрос
              </button>
            </section>
          ) : (
            <>
              {publicationMode !== "live" && (
                <p className="editor-publication-mode" role="status">
                  {publicationMode === "mock"
                    ? "Локальная проверка API. Реальные товары на площадках не создаются."
                    : "Отправка на площадки отключена. Данные сохраняются для подготовки."}
                </p>
              )}
              {marketplaceCardBusy && (
                <p className="editor-secondary" role="status">
                  Загружаем данные карточек площадок…
                </p>
              )}
              {marketplaceCardError && (
                <div className="editor-secondary" role="alert">
                  {marketplaceCardError}{" "}
                  <button
                    type="button"
                    className="admin-action"
                    onClick={() => setMarketplaceCardRevision((value) => value + 1)}
                  >
                    Повторить загрузку
                  </button>
                </div>
              )}
              {!canManage && (
                <p className="editor-secondary">Цены и связи площадок изменяет владелец.</p>
              )}
              <fieldset
                className="editor-protected-fields"
                disabled={!canManage}
                aria-label="Настройки маркетплейсов"
              >
                <MarketplaceEditor
                  productId={productId || undefined}
                  variantId={variant.serverId}
                  linked={linkedMarketplaces}
                  value={marketplace}
                  onChange={changeMarketplace}
                  onCardResolved={(channel, remote) => {
                    const current = latest.current;
                    const rows = current.variants || [];
                    const row = rows.find((item) => item.id === variant.id);
                    if (!row) return;
                    const value = marketplaceOf(row, current);
                    const replacement = replaceMarketplaceCard(value, channel, remote);
                    if (replacement === value) return;
                    update({
                      variants: rows.map((item) =>
                        item.id === row.id ? { ...item, marketplace: replacement } : item,
                      ),
                    });
                  }}
                  name={draft.name}
                  description={draft.description}
                  prices={{
                    ozon: variant.ozon?.price || draft.ozon.price,
                    wb: variant.wb?.price || draft.wb.price,
                  }}
                />
              </fieldset>
              {typeof productId === "number" && productId > 0 && (
                <EditorPricePublications
                  productId={productId}
                  variantId={variant.serverId}
                  draftPriceMinor={Math.round(Number(variant.price) * 100)}
                  saving={saving}
                  revision={collectionsRevision}
                  canManage={canManage}
                />
              )}
              {channelStatuses
                .filter((status) => status.variantId === variant.serverId)
                .map((status) => (
                  <p className="editor-secondary" role="status" key={status.channel}>
                    {status.channel === "wb"
                      ? "Wildberries"
                      : status.channel === "ozon"
                        ? "Ozon"
                        : "СБИС"}
                    : {status.message}
                    {status.channel === "saby"
                      ? status.externalIds?.offerId && <> · Код {status.externalIds.offerId}</>
                      : status.externalIds?.productId && <> · ID {status.externalIds.productId}</>}
                    {canManage &&
                      (status.status === "failed" ||
                        (status.status === "needs_attention" &&
                          (status.channel !== "saby" || status.externalIds?.productId))) && (
                        <button
                          className="admin-action"
                          type="button"
                          disabled={saving}
                          onClick={async () => {
                            if (!variant.serverId) return;
                            setSaving(true);
                            setRemoteError("");
                            try {
                              await api(
                                `/api/v1/admin/products/${product?.id || draft.remoteCreatedProductId}/editor-publications/${status.channel}/retry`,
                                {
                                  method: "POST",
                                  body: JSON.stringify({ variantId: variant.serverId }),
                                },
                              );
                              setCollectionsRevision((v) => v + 1);
                            } catch (e) {
                              setRemoteError((e as Error).message);
                            } finally {
                              setSaving(false);
                            }
                          }}
                        >
                          {status.channel === "saby"
                            ? status.status === "failed"
                              ? "Повторить обмен"
                              : "Проверить состояние"
                            : "Повторить проверку"}
                        </button>
                      )}
                  </p>
                ))}
            </>
          )}
        </div>
      </div>
      <footer className="editor-save-actions">
        {!metadataReady && !categoryLoadMessage && !schemaError && draft.categoryId && (
          <span className="editor-secondary" role="status">
            Готовим поля выбранной категории…
          </span>
        )}
        {canManage && (!product || creationChannels.length > 0) && (
          <div className="editor-destinations" role="group" aria-label="Где создать товар">
            {!product && (
              <>
                <label>
                  <input type="checkbox" checked disabled />
                  Сайт
                </label>
                <label>
                  <input type="checkbox" checked disabled />
                  СБИС
                </label>
              </>
            )}
            {creationChannels.map(([id, title]) => (
              <label key={id}>
                <input
                  type="checkbox"
                  checked={marketplace[id].enabled}
                  onChange={(e) =>
                    changeMarketplace({
                      ...marketplace,
                      [id]: { ...marketplace[id], enabled: e.target.checked },
                    })
                  }
                />
                {title}
              </label>
            ))}
          </div>
        )}
        <button
          type="button"
          className="admin-primary"
          disabled={loading || !remoteLoaded || saving || mediaBusy || !metadataReady}
          onClick={() => {
            const missing = canManage
              ? variants
                  .filter((row) => !row.removePending && !row.archived)
                  .flatMap((row) => {
                    const m = marketplaceOf(row, draft),
                      links = channelLinks[String(row.serverId)] || {};
                    return marketplaceMissing(m, links);
                  })
              : [];
            if (missing.length) {
              setRemoteError(`Заполните: ${[...new Set(missing)].join(", ")}.`);
              return;
            }
            const invalidPrice = variants.find(
              (item) =>
                !item.price.trim() ||
                !Number.isFinite(Number(item.price)) ||
                Number(item.price) < 0,
            );
            if (invalidPrice) {
              setRemoteError(
                `Укажите цену на сайте для варианта «${invalidPrice.label || "Диаметр не указан"}».`,
              );
              return;
            }
            if (product) void apply();
            else void createProduct();
          }}
        >
          {saving ? "Сохраняем…" : product ? "Сохранить" : "Создать товар"}
        </button>
      </footer>
    </div>
  );
}
