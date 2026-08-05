// Registration of the service worker, plus the little bit of state the UI
// needs to offer "add to home screen".

export type InstallPromptEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

export function registerServiceWorker() {
  if (!("serviceWorker" in navigator)) return;
  // Registering after load keeps the worker from competing with the first
  // render for bandwidth.
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // A failed registration only costs the offline page; the shop works.
    });
  });
}

export function isStandalone() {
  return window.matchMedia("(display-mode: standalone)").matches ||
    // Safari on iOS predates display-mode and reports it here instead.
    (window.navigator as { standalone?: boolean }).standalone === true;
}

export function isIOS() {
  return /iphone|ipad|ipod/i.test(navigator.userAgent);
}

// --- Push notifications -------------------------------------------------

// The browser wants the VAPID public key as raw bytes, but it travels as
// base64url, so it has to be unpacked here.
function decodeKey(base64url: string) {
  const padded = base64url.replace(/-/g, "+").replace(/_/g, "/")
    .padEnd(base64url.length + (4 - base64url.length % 4) % 4, "=");
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export function pushSupported() {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

export async function pushState() {
  if (!pushSupported()) return { available: false, subscribed: false, blocked: false };
  const response = await fetch("/api/v1/push/key", { cache: "no-store" });
  const { enabled } = await response.json() as { enabled: boolean };
  const registration = await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.getSubscription();
  return {
    available: enabled,
    subscribed: Boolean(subscription),
    blocked: Notification.permission === "denied",
  };
}

export async function enablePush() {
  const response = await fetch("/api/v1/push/key", { cache: "no-store" });
  const { publicKey } = await response.json() as { publicKey: string };
  if (!publicKey) throw new Error("Уведомления пока не настроены");

  const permission = await Notification.requestPermission();
  if (permission !== "granted") {
    throw new Error(permission === "denied"
      ? "Уведомления запрещены в настройках браузера"
      : "Разрешение не выдано");
  }

  const registration = await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: decodeKey(publicKey),
  });

  const saved = await fetch("/api/v1/push/subscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    body: JSON.stringify(subscription.toJSON()),
  });
  if (!saved.ok) {
    await subscription.unsubscribe();
    throw new Error("Не удалось включить уведомления");
  }
}

export async function disablePush() {
  const registration = await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.getSubscription();
  if (!subscription) return;
  await fetch("/api/v1/push/unsubscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    body: JSON.stringify({ endpoint: subscription.endpoint }),
  }).catch(() => undefined);
  await subscription.unsubscribe();
}
