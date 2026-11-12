export const installOfferSeenKey = "ficusin:install-offer-seen:v1";
let seenWithoutStorage = false;
export function claimInstallOffer(
  storage: Pick<Storage, "getItem" | "setItem"> | null,
  installed: boolean,
): boolean {
  if (installed) return false;
  try {
    if (storage?.getItem(installOfferSeenKey) === "1") return false;
    storage?.setItem(installOfferSeenKey, "1");
  } catch {
    if (seenWithoutStorage) return false;
  }
  if (!storage && seenWithoutStorage) return false;
  seenWithoutStorage = true;
  return true;
}
