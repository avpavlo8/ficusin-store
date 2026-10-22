import { expect, test } from "@playwright/test";

test("production installed-app and browser icons serve the approved logo", async ({ page, request }) => {
  await page.goto("/");
  const manifestURL = await page.locator('link[rel="manifest"]').getAttribute("href");
  expect(manifestURL).toBeTruthy();
  const manifestResponse = await request.get(manifestURL!);
  expect(manifestResponse.ok()).toBeTruthy();
  expect(manifestResponse.headers()["content-type"]).toContain("application/manifest+json");
  const manifest = await manifestResponse.json();
  expect(manifest.id).toBe("/?source=pwa");
  expect(manifest.icons.some((icon: { purpose: string }) => icon.purpose === "maskable")).toBe(true);
  const apple = await page.locator('link[rel="apple-touch-icon"]').getAttribute("href");
  const images = [...manifest.icons.map((icon: { src: string; sizes: string }) => ({ src: icon.src, size: Number(icon.sizes.split("x")[0]) })), { src: apple!, size: 180 }];
  for (const image of images) {
    expect(image.src).toMatch(/^\/assets\/brand\/graphite-v1\/app-/);
    const response = await request.get(image.src);
    expect(response.ok()).toBeTruthy();
    expect(response.headers()["content-type"]).toContain("image/png");
    const png = await response.body();
    expect(png.subarray(1, 4).toString()).toBe("PNG");
    expect(png.readUInt32BE(16)).toBe(image.size);
    expect(png.readUInt32BE(20)).toBe(image.size);
  }
  const favicon = await page.locator('link[rel="icon"][type="image/svg+xml"]').getAttribute("href");
  const response = await request.get(favicon!);
  expect(response.headers()["content-type"]).toContain("image/svg+xml");
  expect(await response.text()).toContain("#353630");
  await expect(page.locator("header .ficusin-logo")).toHaveAttribute("src", "/assets/brand/graphite-v1/wordmark-graphite.svg");
});
