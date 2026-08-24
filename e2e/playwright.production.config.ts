import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./production-tests",
  fullyParallel: true,
  forbidOnly: true,
  retries: 1,
  reporter: [["github"], ["list"]],
  use: {
    baseURL: "https://www.ficusin.ru",
    serviceWorkers: "block",
    trace: "on-first-retry",
  },
  projects: [
    { name: "production-iphone", use: { ...devices["iPhone 13"], browserName: "webkit" } },
    { name: "production-android", use: { ...devices["Pixel 5"], browserName: "chromium" } },
    { name: "production-desktop", use: { ...devices["Desktop Chrome"], browserName: "chromium" } },
  ],
});
