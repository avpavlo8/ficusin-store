import { test as base, expect } from "@playwright/test";
import { installOfferSeenKey } from "../../frontend/src/lib/install-offer";

type VisitorOptions = { showInstallOffer: boolean };

// General live flows represent a returning visitor. First-visit onboarding is
// exercised separately with showInstallOffer=true, without disabling app code.
export const test = base.extend<VisitorOptions>({
  showInstallOffer: [false, { option: true }],
  page: async ({ page, showInstallOffer }, use) => {
    if (!showInstallOffer)
      await page.addInitScript(
        (key) => localStorage.setItem(key, "1"),
        installOfferSeenKey,
      );
    await use(page);
  },
});
export { expect };
