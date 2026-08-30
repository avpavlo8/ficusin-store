import { defineConfig, devices } from "@playwright/test";

// The tests run against the real built bundle served by Vite's preview
// server, so what they check is what a visitor would get.
const port = 4173;
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"], ["html", { outputFolder: "playwright-report", open: "never" }]] : [["list"]],
  use: {
    baseURL,
    trace: "on-first-retry",
    // Витрина регистрирует service worker, и через секунду после загрузки он
    // берёт страницу под контроль. Запросы такой страницы Playwright видит
    // только в Chromium — в WebKit они проходят мимо моков к preview-серверу,
    // тот проксирует /api на бэкенд, которого в проверке нет, и отвечает 502.
    // Корзина после перехода приходила пустой ровно поэтому. Выключаем worker:
    // моки без этого работают не везде, и подмена сети становится враньём.
    serviceWorkers: "block",
  },
  webServer: {
    command: `npm run preview -- --port ${port} --strictPort --host 127.0.0.1`,
    cwd: "../frontend",
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
  // Each project runs only the tests written for its screen size. Selecting
  // by title beats skipping inside the test: a skipped test still reports as
  // run, and it is easy to leave one skipped everywhere by accident.
  projects: [
    // Safari on an iPhone is the single most common way this shop is
    // opened, so it leads the list.
    { name: "iphone", use: { ...devices["iPhone 13"] }, grep: /@phone/ },
    { name: "android", use: { ...devices["Pixel 5"] }, grep: /@phone/ },
    { name: "desktop", use: { ...devices["Desktop Chrome"] }, grep: /@desktop/ },
  ],
});
