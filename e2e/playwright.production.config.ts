import { defineConfig, devices } from "@playwright/test";
import { stagingState } from "./staging-access";

const baseURL = process.env.FICUSIN_BASE_URL || "https://www.ficusin.ru";
const stagingToken = process.env.STAGING_ACCESS_TOKEN;
if (
  stagingToken &&
  !/^(https:\/\/[^/]+\.twc1\.net|https:\/\/staging\.ficusin\.ru)$/.test(baseURL)
) {
  throw new Error(
    "Staging credentials must only be sent to the staging origin",
  );
}

export default defineConfig({
  testDir: "./production-tests",
  ...(stagingToken ? { globalSetup: "./staging-gate.setup.ts" } : {}),
  fullyParallel: true,
  forbidOnly: true,
  retries: 1,
  reporter: [["github"], ["list"]],
  use: {
    baseURL,
    ...(stagingToken ? { storageState: stagingState() } : {}),
    serviceWorkers: "block",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "production-iphone",
      use: { ...devices["iPhone 13"], browserName: "webkit" },
    },
    {
      name: "production-android",
      use: { ...devices["Pixel 5"], browserName: "chromium" },
    },
    {
      name: "production-desktop",
      use: { ...devices["Desktop Chrome"], browserName: "chromium" },
    },
  ],
});
