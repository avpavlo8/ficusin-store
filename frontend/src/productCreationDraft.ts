import type { FlowwowChannel } from "./flowwowTypes";
import type {
  MarketplaceCommon,
  SabyEditorConfig,
  SabyEditorTouched,
  ChannelCategory,
} from "./catalogEditorChannels.ts";
import type { PlantPassport } from "./adminTypes";
import { normalizeProductDraft } from "./productDraftMigration.ts";
type ChannelDraft = {
  price: string;
};
export type DraftMedia = {
  id: string;
  name: string;
  type: string;
  source?: string;
  serverId?: number;
};
export type DraftVariant = {
  flowwow?: FlowwowChannel;
  marketplace: MarketplaceCommon;
  marketplaceInitialized?: boolean;
  sabyEditor: SabyEditorConfig;

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
  wholesaleMinQty: string;
  active: boolean;
  archived: boolean;
  attributes: Record<string, string | number | boolean | string[] | null>;
  externalIds: Array<{ provider: string; type: string; externalId: string }>;
  media?: DraftMedia[];
  mediaInitialized?: boolean;
  mediaCustomized?: boolean;
  sabyId?: string;
  ozon: ChannelDraft;
  wb: ChannelDraft;
};
export type ProductCreationDraft = {
  creationId?: string;
  remoteCreatedProductId?: number;
  sabyDefaults?: { category?: ChannelCategory; folder?: ChannelCategory };
  version: 2;
  updatedAt: string;
  name: string;
  price: string;
  categoryId?: number;
  description: string;
  latinName: string;
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
  variants?: DraftVariant[];
  selectedVariantId?: string;
  warnings?: string;
  plantAttributesVersion?: number;
};
export const draftStorageKey = "ficusin:product-creation-draft:v1";
const channel = (): ChannelDraft => ({
  price: "",
});
export const emptyProductDraft = (): ProductCreationDraft => {
  const base: ProductCreationDraft = {
    version: 2,
    updatedAt: "",
    name: "",
    price: "",
    description: "",
    latinName: "",
    ozon: channel(),
    wb: channel(),
    attributes: {},
    care: "",
    faq: [],
    media: [],
  };
  return normalizeProductDraft(base, base);
};
export function loadProductDraft(
  key = draftStorageKey,
  fallback = emptyProductDraft(),
): { draft: ProductCreationDraft; error: string; blocked?: boolean } {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return { draft: normalizeProductDraft(fallback, emptyProductDraft()), error: "" };
    const data = JSON.parse(raw);
    const draft = normalizeProductDraft(data, emptyProductDraft());
    if (data.version === 1) {
      preserveDraftBackup(key, raw);
      if (localStorage.getItem(key) !== raw) throw new Error("Draft changed in another tab");
      localStorage.setItem(key, JSON.stringify(draft));
    }
    return { draft, error: "" };
  } catch {
    return {
      draft: normalizeProductDraft(fallback, emptyProductDraft()),
      error:
        "Не удалось безопасно перенести черновик. Исходные данные не удалены. Проверьте свободное место и доступ к хранилищу браузера, затем обновите страницу.",
      blocked: true,
    };
  }
}
function preserveDraftBackup(key: string, raw: string) {
  const prefix = `${key}:migration-backup:v1`;
  for (let slot = 0; slot < 1000; slot++) {
    const name = slot === 0 ? prefix : `${prefix}:${slot}`;
    const saved = localStorage.getItem(name);
    if (saved === raw) return;
    if (saved === null) {
      localStorage.setItem(name, raw);
      return;
    }
  }
  throw new Error("Draft backup limit reached");
}
export function saveProductDraft(draft: ProductCreationDraft, key = draftStorageKey) {
  localStorage.setItem(key, JSON.stringify(draft));
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
  // WebKit can reject Blob/File values during IndexedDB serialization. Keep
  // their exact bytes and MIME type; legacy Blob records remain readable below.
  const stored = { version: 1, bytes: await file.arrayBuffer(), type: file.type };
  const db = await mediaDB();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("media", "readwrite");
      const request = tx.objectStore("media").put(stored, id);
      request.onerror = () => reject(request.error);
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
    const stored = await new Promise<unknown>((resolve, reject) => {
      const tx = db.transaction("media", "readonly");
      const request = tx.objectStore("media").get(id);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    if (stored == null) return undefined;
    if (stored instanceof Blob) {
      const binary = { version: 1, bytes: await stored.arrayBuffer(), type: stored.type };
      // File IDs are immutable. Backup and replacement share one transaction;
      // quota/abort leaves the original Blob and all earlier backups untouched.
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction("media", "readwrite"),
          store = tx.objectStore("media");
        const request = store.get(id);
        request.onsuccess = () => {
          const current = request.result;
          if (
            !(current instanceof Blob) ||
            current.size !== stored.size ||
            current.type !== stored.type
          )
            return;
          const backupKey = `migration-backup:v1:${id}`,
            backup = store.get(backupKey);
          backup.onsuccess = () => {
            if (backup.result != null) return;
            store.put(binary, backupKey);
            store.put(binary, id);
          };
        };
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
        tx.onabort = () => reject(tx.error);
      });
      return new Blob([binary.bytes], { type: binary.type });
    }
    if (
      typeof stored === "object" &&
      "version" in stored &&
      stored.version === 1 &&
      "bytes" in stored &&
      stored.bytes instanceof ArrayBuffer &&
      "type" in stored &&
      typeof stored.type === "string"
    )
      return new Blob([stored.bytes], { type: stored.type });
    throw new Error("Файл черновика повреждён");
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
