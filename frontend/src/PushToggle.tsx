import { useCallback, useEffect, useState } from "react";
import {
  browserSubscription,
  isIOS,
  isStandalone,
  pushPaused,
  pushSupported,
  queuePushMutation,
  setPushPaused,
} from "./lib/pwa";
import { notificationMutation, type CustomerPushChannel } from "./lib/app-notifications";

type State = {
  ready: boolean;
  available: boolean;
  key: string;
  orders: boolean;
  chat: boolean;
  blocked: boolean;
};
export function PushToggle() {
  const [state, setState] = useState<State>({
    ready: false,
    available: false,
    key: "",
    orders: false,
    chat: false,
    blocked: false,
  });
  const [busy, setBusy] = useState<CustomerPushChannel | null>(null);
  const [error, setError] = useState("");
  const refresh = useCallback(async () => {
    try {
      const response = await fetch("/api/v1/push/key", { cache: "no-store" });
      if (!response.ok) throw new Error("Не удалось проверить настройки уведомлений");
      const config = (await response.json()) as { enabled: boolean; publicKey: string };
      let orders = false,
        chat = false;
      if (
        config.enabled &&
        pushSupported() &&
        Notification.permission === "granted" &&
        (!isIOS() || isStandalone())
      ) {
        const registration = await navigator.serviceWorker.register("/sw.js", {
          updateViaCache: "none",
        });
        const subscription = await registration.pushManager.getSubscription();
        orders = !!subscription && !pushPaused("orders");
        if (subscription && !pushPaused("chat")) {
          const response = await fetch("/api/v1/chat/notifications/state", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({ endpoint: subscription.endpoint }),
          });
          if (!response.ok) throw new Error("Не удалось проверить уведомления о сообщениях");
          chat = ((await response.json()) as { subscribed: boolean }).subscribed;
        }
      }
      setState({
        ready: true,
        available: !!config.enabled,
        key: config.publicKey || "",
        orders,
        chat,
        blocked: pushSupported() && Notification.permission === "denied",
      });
    } catch (caught) {
      setState((current) => ({ ...current, ready: true }));
      setError(caught instanceof Error ? caught.message : "Не удалось проверить уведомления");
    }
  }, []);
  useEffect(() => {
    void refresh();
    window.addEventListener("ficusin-chat-push-changed", refresh);
    return () => window.removeEventListener("ficusin-chat-push-changed", refresh);
  }, [refresh]);
  const toggle = async (channel: CustomerPushChannel) => {
    if (!pushSupported() || (isIOS() && !isStandalone()) || !state.available || !state.key) return;
    const enabled = !state[channel],
      other = channel === "orders" ? "chat" : "orders";
    const paused = pushPaused(channel),
      otherPaused = pushPaused(other);
    // One switch controls one channel. A newly granted device permission must
    // not let automatic chat/order restoration enable the other switch.
    if (!enabled) setPushPaused(channel, true);
    if (enabled && !state[other]) setPushPaused(other, true);
    setBusy(channel);
    setError("");
    try {
      if (
        enabled &&
        Notification.permission !== "granted" &&
        (await Notification.requestPermission()) !== "granted"
      )
        throw new Error("Разрешите уведомления для Фикусина в настройках браузера.");
      const subscription = enabled
        ? await browserSubscription(state.key)
        : await (await navigator.serviceWorker.register("/sw.js")).pushManager.getSubscription();
      if (subscription) {
        const mutation = notificationMutation(channel, enabled, subscription);
        await queuePushMutation(channel, async () => {
          const response = await fetch(mutation.url, {
            method: mutation.method,
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify(mutation.body),
          });
          if (!response.ok)
            throw new Error("Не удалось сохранить настройку уведомлений. Попробуйте ещё раз.");
        });
      }
      setPushPaused(channel, !enabled);
      setState((current) => ({ ...current, [channel]: enabled, blocked: false }));
      window.dispatchEvent(new Event("ficusin-chat-push-changed"));
    } catch (caught) {
      setPushPaused(channel, paused);
      setPushPaused(other, otherPaused);
      setError(caught instanceof Error ? caught.message : "Не удалось изменить настройку");
      setState((current) => ({ ...current, blocked: Notification.permission === "denied" }));
    } finally {
      setBusy(null);
    }
  };
  const needsInstall = isIOS() && !isStandalone();
  const supported = pushSupported();
  const unavailable = !state.ready
    ? "Проверяем доступность уведомлений…"
    : needsInstall
      ? "На iPhone сначала добавьте Фикусин на экран «Домой» и откройте с иконки."
      : !supported
        ? "Этот браузер не поддерживает push-уведомления. Попробуйте Safari или Chrome."
        : !state.available
          ? "Уведомления пока не настроены на сервере."
          : state.blocked
            ? "Уведомления запрещены в настройках браузера. Разрешите их для ficusin.ru."
            : "";
  return (
    <div className="customer-push-settings">
      {unavailable && <p className="customer-push-note">{unavailable}</p>}
      {(
        [
          ["orders", "Уведомления о заказах", "Готовность заказа, отправка и получение."],
          ["chat", "Уведомления о сообщениях", "Новые ответы магазина в чате."],
        ] as const
      ).map(([channel, title, note]) => (
        <div className="push-toggle" key={channel}>
          <div>
            <strong>{title}</strong>
            <span>{note}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={state[channel]}
            aria-label={title}
            className={state[channel] ? "ghost-button" : "primary-button"}
            disabled={!!busy || !state.ready || !!unavailable}
            onClick={() => void toggle(channel)}
          >
            {busy === channel ? "…" : state[channel] ? "Выключить" : "Включить"}
          </button>
        </div>
      ))}
      {error && (
        <p className="push-toggle-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
