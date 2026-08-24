export type CommerceEventName =
  | "page_view" | "view_item_list" | "select_item" | "search" | "filter"
  | "view_item" | "add_to_cart" | "remove_from_cart" | "view_cart"
  | "begin_checkout" | "checkout_step" | "add_shipping_info"
  | "add_payment_info" | "checkout_error" | "payment_redirect";

type EventInput = {
  productCode?: string;
  sku?: string;
  value?: number;
  quantity?: number;
  properties?: Record<string, unknown>;
};

export type Attribution = {
  visitorId: string;
  sessionId: string;
  source: string;
  medium: string;
  campaign: string;
  content: string;
  term: string;
  referrer: string;
};

type QueuedEvent = EventInput & Attribution & {
  eventId: string;
  name: CommerceEventName;
  occurredAt: string;
  pagePath: string;
  pageTitle: string;
};

const VISITOR_KEY = "ficusin-analytics-visitor";
const SESSION_KEY = "ficusin-analytics-session";
const FIRST_TOUCH_KEY = "ficusin-analytics-first-touch";
const LAST_TOUCH_KEY = "ficusin-analytics-last-touch";
const SESSION_STARTED_KEY = "ficusin-analytics-session-started";
const SESSION_TTL = 30 * 60 * 1000;
const queue: QueuedEvent[] = [];
let flushTimer = 0;
let yandexCounter = 0;

function id() {
  return crypto.randomUUID();
}

function storageJSON(key: string): Partial<Attribution> {
  try { return JSON.parse(localStorage.getItem(key) || "{}") as Partial<Attribution>; }
  catch { return {}; }
}

function visitorID() {
  let value = localStorage.getItem(VISITOR_KEY) || "";
  if (!value) { value = id(); localStorage.setItem(VISITOR_KEY, value); }
  return value;
}

function sessionID() {
  const now = Date.now();
  const started = Number(sessionStorage.getItem(SESSION_STARTED_KEY) || 0);
  let value = sessionStorage.getItem(SESSION_KEY) || "";
  if (!value || !started || now - started > SESSION_TTL) {
    value = id();
    sessionStorage.setItem(SESSION_KEY, value);
  }
  sessionStorage.setItem(SESSION_STARTED_KEY, String(now));
  return value;
}

function touchFromLocation(): Omit<Attribution, "visitorId" | "sessionId"> {
  const params = new URLSearchParams(location.search);
  let referrer = document.referrer || "";
  try { if (referrer && new URL(referrer).origin === location.origin) referrer = ""; }
  catch { referrer = ""; }
  let source = params.get("utm_source") || "";
  let medium = params.get("utm_medium") || "";
  if (!source && referrer) {
    try { source = new URL(referrer).hostname.replace(/^www\./, ""); medium = "referral"; }
    catch { /* malformed referrers stay empty */ }
  }
  return {
    source,
    medium,
    campaign: params.get("utm_campaign") || "",
    content: params.get("utm_content") || "",
    term: params.get("utm_term") || "",
    referrer,
  };
}

function captureTouch() {
  const touch = touchFromLocation();
  const hasCampaign = Boolean(touch.source || touch.medium || touch.campaign || touch.referrer);
  if (!localStorage.getItem(FIRST_TOUCH_KEY)) localStorage.setItem(FIRST_TOUCH_KEY, JSON.stringify(touch));
  if (hasCampaign) localStorage.setItem(LAST_TOUCH_KEY, JSON.stringify(touch));
}

export function getAttribution(): Attribution {
  captureTouch();
  const first = storageJSON(FIRST_TOUCH_KEY);
  const last = storageJSON(LAST_TOUCH_KEY);
  const touch = Object.keys(last).length ? last : first;
  return {
    visitorId: visitorID(), sessionId: sessionID(), source: touch.source || "",
    medium: touch.medium || "", campaign: touch.campaign || "",
    content: touch.content || "", term: touch.term || "", referrer: touch.referrer || "",
  };
}

function yandexGoal(name: CommerceEventName, input: EventInput) {
  const ym = (window as unknown as { ym?: (...args: unknown[]) => void }).ym;
  if (yandexCounter && ym) ym(yandexCounter, "reachGoal", name, input);
}

function flush(useBeacon = false) {
  if (!queue.length) return;
  const events = queue.splice(0, 25);
  const body = JSON.stringify({ events });
  if (useBeacon && navigator.sendBeacon) {
    navigator.sendBeacon("/api/v1/analytics/events", new Blob([body], { type: "application/json" }));
  } else {
    void fetch("/api/v1/analytics/events", { method: "POST", credentials: "same-origin", keepalive: true, headers: { "Content-Type": "application/json" }, body }).catch(() => undefined);
  }
}

export function track(name: CommerceEventName, input: EventInput = {}) {
  const privacy = navigator as Navigator & { globalPrivacyControl?: boolean };
  if (privacy.globalPrivacyControl || navigator.doNotTrack === "1") return;
  const attribution = getAttribution();
  queue.push({ eventId: id(), name, occurredAt: new Date().toISOString(), pagePath: location.pathname + location.search, pageTitle: document.title, ...attribution, ...input });
  yandexGoal(name, input);
  if (queue.length >= 10) flush();
  else { window.clearTimeout(flushTimer); flushTimer = window.setTimeout(() => flush(), 900); }
}

export function initAnalytics() {
  captureTouch();
  void fetch("/api/v1/analytics/config", { cache: "no-store" }).then((response)=>response.json()).then((config:{yandexMetrikaId?:number})=>{
		yandexCounter = Number(config.yandexMetrikaId) || 0;
  }).catch(()=>undefined);
  track("page_view");
  addEventListener("pagehide", () => flush(true));
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") flush(true); });
}
