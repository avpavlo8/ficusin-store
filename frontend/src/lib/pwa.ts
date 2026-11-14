// Registration of the service worker, plus the little bit of state the UI
// needs to offer "add to home screen".

export type InstallPromptEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

let storeInstallPrompt: InstallPromptEvent | null = null;
export const getStoreInstallPrompt = () => storeInstallPrompt;
export function clearStoreInstallPrompt() {
  storeInstallPrompt = null;
}
export function initializeStoreInstall() {
  // Admin owns a separate manifest and installation prompt.
  if (
    document
      .querySelector<HTMLLinkElement>('link[rel="manifest"]')
      ?.href.includes("admin.webmanifest")
  )
    return;
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    storeInstallPrompt = event as InstallPromptEvent;
    window.dispatchEvent(new Event("ficusin-install-ready"));
  });
  window.addEventListener("appinstalled", clearStoreInstallPrompt);
}

export function registerServiceWorker() {
  if (!("serviceWorker" in navigator)) return;
  // Registering after load keeps the worker from competing with the first
  // render for bandwidth.
  const register = () => {
    navigator.serviceWorker.register("/sw.js", { updateViaCache: "none" }).catch(() => {
      // A failed registration only costs the offline page; the shop works.
    });
  };
  if (document.readyState === "complete") register();
  else window.addEventListener("load", register, { once: true });
}

export function isStandalone() {
  return (
    window.matchMedia("(display-mode: standalone)").matches ||
    // Safari on iOS predates display-mode and reports it here instead.
    (window.navigator as { standalone?: boolean }).standalone === true
  );
}

export function isIOS() {
  return (
    /iphone|ipad|ipod/i.test(navigator.userAgent) ||
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1)
  );
}

// --- Push notifications -------------------------------------------------

// The browser wants the VAPID public key as raw bytes, but it travels as
// base64url, so it has to be unpacked here.
function decodeKey(base64url: string) {
  const padded = base64url
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .padEnd(base64url.length + ((4 - (base64url.length % 4)) % 4), "=");
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export function pushSupported() {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

async function pushRegistration() {
  await navigator.serviceWorker.register("/sw.js", { updateViaCache: "none" });
  return navigator.serviceWorker.ready;
}

export type PushChannel = "orders" | "chat" | "staff-chat";
export function pushPaused(channel: PushChannel) {
  return document.cookie.split("; ").includes(`ficusin_push_${channel}=paused`);
}
export function setPushPaused(channel: PushChannel, paused: boolean) {
  document.cookie = `ficusin_push_${channel}=${paused ? "paused" : ""}; Path=/; Max-Age=${paused ? 31536000 : 0}; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
}

let subscribing: Promise<PushSubscription> | null = null;
export function browserSubscription(publicKey: string): Promise<PushSubscription> {
  if (!subscribing)
    subscribing = (async () => {
      const manager = (await pushRegistration()).pushManager;
      return (
        (await manager.getSubscription()) ||
        (await manager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: decodeKey(publicKey),
        }))
      );
    })().finally(() => {
      subscribing = null;
    });
  return subscribing;
}

const pushMutations = new Map<PushChannel, Promise<unknown>>();
export function queuePushMutation<T>(
  channel: PushChannel,
  operation: () => Promise<T>,
): Promise<T> {
  const next = (pushMutations.get(channel) || Promise.resolve())
    .catch(() => undefined)
    .then(operation);
  pushMutations.set(channel, next);
  return next;
}

async function saveSubscription(subscription: PushSubscription, path = "/api/v1/push/subscribe") {
  const channel = path.includes("chat/notifications") ? "chat" : "orders";
  return queuePushMutation(channel, async () => {
    if (pushPaused(channel)) return;
    const response = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(subscription.toJSON()),
    });
    if (!response.ok) {
      const data = (await response.json().catch(() => ({}))) as { error?: string };
      throw new Error(data.error || "Не удалось сохранить уведомления. Попробуйте ещё раз.");
    }
  });
}

export async function saveOrderPushIfWanted(subscription: PushSubscription) {
  if (!pushPaused("orders")) await saveSubscription(subscription);
}

let restoring: Promise<PushSubscription | null> | null = null;
// Restore only an existing OS permission. A deliberate per-channel opt-out
// remains off; opening a page never opens the system permission prompt.
export function restoreDefaultPush(): Promise<PushSubscription | null> {
  if (
    !pushSupported() ||
    Notification.permission !== "granted" ||
    (isIOS() && !isStandalone()) ||
    (pushPaused("orders") && pushPaused("chat"))
  )
    return Promise.resolve(null);
  if (!restoring)
    restoring = (async () => {
      const response = await fetch("/api/v1/push/key", { cache: "no-store" });
      const config = (await response.json()) as { enabled: boolean; publicKey: string };
      if (!config.enabled || !config.publicKey) return null;
      const subscription = await browserSubscription(config.publicKey);
      // Sequential requests share the guest session established by the first.
      if (!pushPaused("orders")) await saveSubscription(subscription);
      if (!pushPaused("chat")) await saveSubscription(subscription, "/api/v1/chat/notifications");
      return subscription;
    })().finally(() => {
      restoring = null;
    });
  return restoring;
}
