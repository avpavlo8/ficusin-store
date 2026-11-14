import type { FlowwowChannel } from "./flowwowTypes";
import type {
  MarketplaceCommon,
  SabyEditorConfig,
  SabyEditorTouched,
  ChannelCategory,
} from "./catalogEditorChannels";
import type { PlantPassport } from "./adminTypes";
import type { SabyStepDraft } from "./ProductCreationSabyStep";

export const creationSteps = [
  { id: "main", label: "Основное" },
  { id: "saby", label: "СБИС" },
  { id: "ozon", label: "Озон" },
  { id: "wb", label: "Валдберес" },
  { id: "attributes", label: "Характеристики" },
  { id: "media", label: "Медиа" },
  { id: "care", label: "Уход и FAQ" },
] as const;
export type CreationStep = (typeof creationSteps)[number]["id"];
type ChannelDraft = {
  enabled: boolean;
  name: string;
  category: string;
  price: string;
  article: string;
  description: string;
  brand?: string;
  model?: string;
  tnved?: string;
  country?: string;
  composition?: string;
};
export type DraftMedia = {
  id: string;
  name: string;
  type: string;
  size: number;
  source?: string;
  serverId?: number;
};
export type DraftVariant = {
  flowwow?: FlowwowChannel;
  marketplace?: MarketplaceCommon;
  sabyEditor?: SabyEditorConfig;
  // Local journal only: even clearing a field or clicking the default mode is
  // an explicit choice that a late editor-plan response must not replace.
  sabyEditorTouched?: SabyEditorTouched;
  removePending?: boolean;
  marketplacePriceMode?: "auto" | "manual";
  id: string;
  serverId?: number;
  label: string;
  sku: string;
  price: string;
  stock: number;
  wholesaleMinQty: string;
  active: boolean;
  archived: boolean;
  attributes: Record<string, string | number | boolean | string[] | null>;
  externalIds: Array<{ provider: string; type: string; externalId: string }>;
  media?: DraftMedia[];
  mediaInitialized?: boolean;
  mediaCustomized?: boolean;
  sabyId?: string;
  saby?: SabyStepDraft;
  ozon?: ChannelDraft;
  wb?: ChannelDraft;
};
export type ProductCreationDraft = {
  creationId?: string;
  remoteCreatedProductId?: number;
  sabyCategory?: ChannelCategory;
  sabyFolder?: ChannelCategory;
  version: 1;
  updatedAt: string;
  step: CreationStep;
  name: string;
  pot: string;
  height: string;
  price: string;
  categoryId?: number;
  description: string;
  latinName: string;
  saby: SabyStepDraft;
  ozon: ChannelDraft;
  wb: ChannelDraft;
  attributes: Record<string, string | number | boolean | string[] | null>;
  care: string;
  passport?: Partial<Omit<PlantPassport, "faq">>;
  collectionMemberships?: Record<string, boolean>;
  faq: Array<{ id: string; question: string; answer: string }>;
  media: DraftMedia[];
  cover?: DraftMedia | null;
  remoteInitialized?: boolean;
  editorTab?: "product" | "channels" | "faq";
  channelTab?: "saby" | "ozon" | "wb";
  variants?: DraftVariant[];
  selectedVariantId?: string;
  warnings?: string;
  plantAttributesVersion?: number;
};
export const draftStorageKey = "ficusin:product-creation-draft:v1";
const channel = (): ChannelDraft => ({
  enabled: false,
  name: "",
  category: "",
  price: "",
  article: "",
  description: "",
});
export const emptyProductDraft = (): ProductCreationDraft => ({
  version: 1,
  updatedAt: "",
  step: "main",
  name: "",
  pot: "",
  height: "",
  price: "",
  description: "",
  latinName: "",
  saby: { mode: "create", folderId: "", initialStock: "", query: "", selected: null },
  ozon: channel(),
  wb: channel(),
  attributes: {},
  care: "",
  faq: [],
  media: [],
});
export function loadProductDraft(
  key = draftStorageKey,
  fallback = emptyProductDraft(),
): { draft: ProductCreationDraft; error: string } {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return { draft: fallback, error: "" };
    const data = JSON.parse(raw);
    if (
      data?.version !== 1 ||
      typeof data.name !== "string" ||
      !Array.isArray(data.media) ||
      !Array.isArray(data.faq) ||
      !data.saby ||
      !data.ozon ||
      !data.wb
    )
      throw new Error("invalid draft");
    return { draft: { ...emptyProductDraft(), ...data }, error: "" };
  } catch {
    return {
      draft: emptyProductDraft(),
      error: "Не удалось прочитать сохранённый черновик. Проверьте доступ к хранилищу браузера.",
    };
  }
}
export function saveProductDraft(draft: ProductCreationDraft, key = draftStorageKey) {
  localStorage.setItem(key, JSON.stringify(draft));
}
export function readCreationStep(fallback: CreationStep): CreationStep {
  const id = new URLSearchParams(location.search).get("step");
  return (
    creationSteps.find((step) => step.id === id)?.id ||
    creationSteps.find((step) => step.id === fallback)?.id ||
    "main"
  );
}

// Files are stored separately from the synchronous text journal so large media
// cannot exhaust localStorage or delay a keystroke's save.
function mediaDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("ficusin-product-drafts", 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore("media");
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
export async function saveDraftMedia(id: string, file: Blob): Promise<void> {
  const db = await mediaDB();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("media", "readwrite");
      tx.objectStore("media").put(file, id);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}
export async function readDraftMedia(id: string): Promise<Blob | undefined> {
  const db = await mediaDB();
  try {
    return await new Promise<Blob | undefined>((resolve, reject) => {
      const tx = db.transaction("media", "readonly");
      const request = tx.objectStore("media").get(id);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
  } finally {
    db.close();
  }
}

export async function deleteDraftMedia(id: string): Promise<void> {
  const db = await mediaDB();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("media", "readwrite");
      tx.objectStore("media").delete(id);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}
