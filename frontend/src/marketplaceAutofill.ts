import type { MarketplaceCommon, MarketplaceChannel } from "./catalogEditorChannels";
export type RemoteMarketplaceCard = {
  card: {
    name: string;
    description: string;
    marketplace: MarketplaceCommon;
    channel: MarketplaceChannel;
  };
  externalIds: { productId: string; offerId: string };
};
export function mergeMarketplaceCards(
  current: MarketplaceCommon,
  cards: Partial<Record<"wb" | "ozon", RemoteMarketplaceCard>>,
  site: {
    name: string;
    description: string;
    originalName?: string;
    originalDescription?: string;
    sku: string;
  },
): MarketplaceCommon {
  const merged: MarketplaceCommon = {
    ...current,
    wb: { ...current.wb },
    ozon: { ...current.ozon },
  };
  const untouchedOzon = !current.ozon.category.id && !current.model;
  for (const channel of ["ozon", "wb"] as const) {
    const remote = cards[channel];
    if (!remote) continue;
    const source = remote.card.marketplace,
      local = merged[channel];
    const remoteArticle = remote.externalIds.offerId;
    const switching = local.replaceLink && local.article?.trim() !== remoteArticle;
    if (switching) continue;
    merged[channel] = {
      ...local,
      article: local.replaceLink ? remoteArticle : (local.article ?? remoteArticle),
      linkedArticle: remoteArticle,
      replaceLink: false,
      mode: "create",
      existingId:
        local.existingId ||
        (channel === "wb" ? remote.externalIds.productId : remote.externalIds.offerId),
      category: local.category.id ? local.category : remote.card.channel.category,
      attributes: { ...remote.card.channel.attributes, ...local.attributes },
    };
    if (!merged.article || merged.article === site.sku)
      merged.article = source.article || remote.externalIds.offerId;
    for (const key of ["brand", "tnved", "model"] as const)
      if (!merged[key] && source[key]) merged[key] = source[key];
    if (!merged.brandId && merged.brand === source.brand) merged.brandId = source.brandId;
    if (!merged.tnvedId && merged.tnved === source.tnved) merged.tnvedId = source.tnvedId;
    if (channel === "ozon" && untouchedOzon && source.units > 0) merged.units = source.units;
    const remoteName = source.name || remote.card.name,
      remoteDescription = source.description || remote.card.description;
    if (!merged.name && site.name === site.originalName && remoteName && remoteName !== site.name)
      merged.name = remoteName;
    if (
      !merged.description &&
      site.description === site.originalDescription &&
      remoteDescription &&
      remoteDescription !== site.description
    )
      merged.description = remoteDescription;
  }
  return merged;
}

// Switching identity replaces content rather than merging it with the old card.
export function replaceMarketplaceCard(
  current: MarketplaceCommon,
  channel: "wb" | "ozon",
  remote: RemoteMarketplaceCard,
): MarketplaceCommon {
  const local = current[channel];
  if (local.article?.trim() !== remote.externalIds.offerId) return current;
  const source = remote.card.marketplace;
  return {
    ...current,
    name: source.name || remote.card.name || "",
    description: source.description || remote.card.description || "",
    contentFromMarketplace: true,
    brand: source.brand || "",
    brandId: source.brandId,
    tnved: source.tnved || "",
    tnvedId: source.tnvedId,
    ...(channel === "ozon" ? { model: source.model || "", units: source.units || 0 } : {}),
    [channel]: {
      ...local,
      existingId: channel === "wb" ? remote.externalIds.productId : remote.externalIds.offerId,
      category: { ...remote.card.channel.category },
      attributes: { ...remote.card.channel.attributes },
    },
  };
}
