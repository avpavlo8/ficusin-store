import { useEffect, useState } from "react";
import {
  enablePush,
  clearStoreInstallPrompt,
  getStoreInstallPrompt,
  type InstallPromptEvent,
  isIOS,
  isStandalone,
  pushPaused,
  pushSupported,
  restoreDefaultPush,
} from "./lib/pwa";

const SNOOZE = "ficusin_install_snooze";
function snoozed() {
  return document.cookie.split("; ").includes(`${SNOOZE}=1`);
}

export function InstallHint({ inline = false }: { inline?: boolean }) {
  const [promptEvent, setPromptEvent] = useState<InstallPromptEvent | null>(getStoreInstallPrompt);
  const [mode, setMode] = useState<"install" | "notifications" | null>(() =>
    inline && !isStandalone() ? "install" : null,
  );
  const [steps, setSteps] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    // Permission already granted: reconnect the device automatically. The
    // system permission itself is requested only by an explicit button tap.
    void restoreDefaultPush().catch(() => undefined);
    if (!inline && snoozed()) return;
    let timer = 0;
    let active = true;
    const offerNotifications = () => {
      if (!pushSupported() || Notification.permission !== "default" || pushPaused("orders")) return;
      void fetch("/api/v1/push/key", { cache: "no-store" })
        .then((response) => (response.ok ? response.json() : null))
        .then((config: { enabled?: boolean; publicKey?: string } | null) => {
          if (active && config?.enabled && config.publicKey)
            timer = window.setTimeout(() => setMode("notifications"), 6_000);
        })
        .catch(() => undefined);
    };
    if (isStandalone()) {
      offerNotifications();
    } else if (isIOS()) {
      timer = window.setTimeout(() => setMode("install"), 8_000);
    }
    const onPrompt = () => {
      setPromptEvent(getStoreInstallPrompt());
      setMode("install");
    };
    const onInstalled = () => {
      window.clearTimeout(timer);
      setMode(null);
      setPromptEvent(null);
      offerNotifications();
      document.cookie = `${SNOOZE}=; Path=/; Max-Age=0; SameSite=Lax`;
    };
    const onPush = () => {
      if (pushSupported() && Notification.permission === "granted") setMode(null);
    };
    if (getStoreInstallPrompt() && !isStandalone()) timer = window.setTimeout(onPrompt, 0);
    window.addEventListener("ficusin-install-ready", onPrompt);
    window.addEventListener("appinstalled", onInstalled);
    window.addEventListener("ficusin-chat-push-changed", onPush);
    return () => {
      active = false;
      window.clearTimeout(timer);
      window.removeEventListener("ficusin-install-ready", onPrompt);
      window.removeEventListener("appinstalled", onInstalled);
      window.removeEventListener("ficusin-chat-push-changed", onPush);
    };
  }, [inline]);

  const dismiss = () => {
    document.cookie = `${SNOOZE}=1; Path=/; Max-Age=604800; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
    setMode(null);
  };
  const act = async () => {
    setError("");
    if (mode === "install" && !promptEvent) {
      setSteps((value) => !value);
      return;
    }
    setBusy(true);
    try {
      if (mode === "notifications") {
        await enablePush();
        setMode(null);
      } else if (promptEvent) {
        await promptEvent.prompt();
        const choice = await promptEvent.userChoice;
        clearStoreInstallPrompt();
        setPromptEvent(null);
        if (choice.outcome === "accepted") setMode(null);
        else if (!inline) dismiss();
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Попробуйте ещё раз");
    } finally {
      setBusy(false);
    }
  };
  if (!mode) return null;
  return (
    <aside
      className={inline ? "home-install" : "install-hint"}
      aria-label={mode === "install" ? "Установить приложение Фикусин" : "Уведомления Фикусина"}
    >
      <img src="/assets/brand/graphite-v1/app-192-v2.png" alt="" />
      <div>
        <strong>{mode === "install" ? "Фикусин на экране «Домой»" : "Оставайтесь на связи"}</strong>
        <span>
          {mode === "install"
            ? "Добавьте иконку, откройте Фикусин с неё и включите уведомления о заказах и сообщениях."
            : "Разрешите уведомления о новых сообщениях и вашем заказе."}
        </span>
        {steps && (
          <ol>
            <li>
              {isIOS() ? "Откройте меню «Поделиться» в браузере." : "Откройте меню браузера."}
            </li>
            <li>
              {isIOS()
                ? "Выберите «На экран Домой» и нажмите «Добавить»."
                : "Выберите «Установить приложение» или «Добавить на главный экран», если этот пункт доступен."}
            </li>
            <li>
              Откройте Фикусин с нового значка и нажмите «Включить» в предложении об уведомлениях.
            </li>
          </ol>
        )}
        {error && <span role="alert">{error}</span>}
      </div>
      <button className="install-hint-action" disabled={busy} onClick={() => void act()}>
        {busy
          ? "…"
          : mode === "notifications"
            ? "Включить"
            : promptEvent
              ? "Установить"
              : steps
                ? "Свернуть"
                : "Как установить"}
      </button>
      {!inline && (
        <button
          className="install-hint-close"
          onClick={dismiss}
          aria-label="Напомнить об установке позже"
        >
          ×
        </button>
      )}
    </aside>
  );
}
