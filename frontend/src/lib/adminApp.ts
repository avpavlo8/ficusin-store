export function initializeAdminApp() {
  const path = location.pathname;
  const target = new URLSearchParams(location.search).get("returnTo") || "";
  const adminPath = (value: string) =>
    value === "/admin" || value.startsWith("/admin/") || value.startsWith("/admin?");
  if (!adminPath(path) && !(["/login", "/register"].includes(path) && adminPath(target))) return;

  // Production sends these in the initial HTML too. This also covers Vite's
  // development/preview server, which serves the unmodified storefront shell.
  document.title = "Фикусин — управление";
  const manifest = document.querySelector<HTMLLinkElement>('link[rel="manifest"]');
  if (manifest) manifest.href = "/admin.webmanifest?v=admin-v1";
  const icon = document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]');
  if (icon) icon.href = "/assets/brand/admin-v1/app-180.png";
  document
    .querySelector('meta[name="apple-mobile-web-app-title"]')
    ?.setAttribute("content", "Управление");
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", "#244734");
  // Keep installation available through the browser menu without a promotion.
  window.addEventListener("beforeinstallprompt", (event) => event.preventDefault());
}
