import type { DraftMedia, ProductCreationDraft } from "./productCreationDraft";

export function moveGalleryPhoto(
  items: DraftMedia[],
  id: string,
  targetId: string | null,
): DraftMedia[] {
  if (id === targetId) return items;
  const moved = items.find((item) => item.id === id);
  if (!moved) return items;
  if (targetId === null && !moved.type.startsWith("image/")) return items;
  const rest = items.filter((item) => item.id !== id);
  const index = targetId === null ? 0 : rest.findIndex((item) => item.id === targetId);
  if (index < 0) return items;
  return [...rest.slice(0, index), moved, ...rest.slice(index)];
}

export function commonCover(draft: ProductCreationDraft): DraftMedia | undefined {
  return draft.cover === undefined
    ? draft.media.find((item) => item.type.startsWith("image/"))
    : draft.cover || undefined;
}

const samePhoto = (a: DraftMedia, b: DraftMedia) =>
  a.id === b.id || Boolean(a.source && a.source === b.source);

export function variantGallery(draft: ProductCreationDraft, variantId: string): DraftMedia[] {
  const variant = draft.variants?.find((item) => item.id === variantId);
  const photos =
    variant?.mediaCustomized || variant?.media?.length ? variant.media || [] : draft.media;
  const cover = commonCover(draft);
  return cover ? [cover, ...photos.filter((item) => !samePhoto(item, cover))] : photos;
}

// The shared cover is persisted once at product scope. It is part of each
// displayed gallery, but must not be copied into every SKU during Save.
export function variantMediaToPersist(
  draft: ProductCreationDraft,
  variantId: string,
): DraftMedia[] {
  const cover = commonCover(draft);
  return variantGallery(draft, variantId).filter((photo) => !cover || !samePhoto(photo, cover));
}

export function replaceVariantGallery(
  draft: ProductCreationDraft,
  variantId: string,
  media: DraftMedia[],
): Partial<ProductCreationDraft> {
  const previous = commonCover(draft);
  const cover = media.find((item) => item.type.startsWith("image/")) || null;
  const previousRetained = previous && media.some((item) => samePhoto(item, previous));
  const shared = draft.media.filter(
    (item) => !previous || previousRetained || !samePhoto(item, previous),
  );
  return {
    cover,
    media: cover ? [cover, ...shared.filter((item) => !samePhoto(item, cover))] : shared,
    variants: draft.variants?.map((item) =>
      item.id === variantId
        ? {
            ...item,
            media: media.filter((photo) => !cover || !samePhoto(photo, cover)),
            mediaInitialized: true,
            mediaCustomized: true,
          }
        : !item.mediaCustomized && !item.media?.length
          ? {
              ...item,
              media: draft.media.filter((photo) => !previous || !samePhoto(photo, previous)),
              mediaInitialized: true,
              mediaCustomized: true,
            }
          : item,
    ),
  };
}

export function appendGalleryMedia(
  draft: ProductCreationDraft,
  variantId: string,
  photo: DraftMedia,
): Partial<ProductCreationDraft> {
  return replaceVariantGallery(draft, variantId, [...variantGallery(draft, variantId), photo]);
}
