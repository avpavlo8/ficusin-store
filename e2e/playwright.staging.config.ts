import { defineConfig, devices } from "@playwright/test";
import { stagingState } from "./staging-access";
import { isStagingOrigin } from "./staging-target";

const baseURL = process.env.FICUSIN_BASE_URL || "";
if (
  !isStagingOrigin(baseURL) ||
  !process.env.STAGING_ACCESS_TOKEN
) {
  throw new Error(
    "Isolated staging URL and access token required; never run on production",
  );
}
export default defineConfig({
  testDir: "./staging-tests",
  globalSetup: "./staging-owner.setup.ts",
  timeout: 60_000,
  retries: 0,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL,
    storageState: stagingState(true),
    trace: "retain-on-failure",
    serviceWorkers: "block",
  },
  projects: [
    { name: "staging-desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "staging-mobile", use: { ...devices["Pixel 5"] } },
  ],
});
