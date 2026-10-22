import { useEffect, useState } from "react";
import { enablePush, type InstallPromptEvent, isIOS, isStandalone, pushPaused, pushSupported, restoreDefaultPush } from "./lib/pwa";

const SNOOZE = "ficusin_install_snooze";
function snoozed() { return document.cookie.split("; ").includes(`${SNOOZE}=1`); }

export function InstallHint() {
  const [promptEvent, setPromptEvent] = useState<InstallPromptEvent | null>(null);
  const [mode, setMode] = useState<"install" | "notifications" | null>(null);
  const [steps, setSteps] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    // Permission already granted: reconnect the device automatically. The
    // system permission itself is requested only by an explicit button tap.
    void restoreDefaultPush().catch(() => undefined);
    if (snoozed()) return;
    let timer = 0;
    if (isStandalone()) {
      if (pushSupported() && Notification.permission === "default" && !pushPaused("orders")) timer = window.setTimeout(() => setMode("notifications"), 6_000);
    } else if (isIOS()) {
      timer = window.setTimeout(() => setMode("install"), 8_000);
    }
    const onPrompt = (event: Event) => { event.preventDefault(); setPromptEvent(event as InstallPromptEvent); setMode("install"); };
    const onInstalled = () => { setMode(null); document.cookie = `${SNOOZE}=; Path=/; Max-Age=0; SameSite=Lax`; };
    const onPush = () => { if (pushSupported() && Notification.permission === "granted") setMode(null); };
    window.addEventListener("beforeinstallprompt", onPrompt);
    window.addEventListener("appinstalled", onInstalled);
    window.addEventListener("ficusin-chat-push-changed", onPush);
    return () => { window.clearTimeout(timer); window.removeEventListener("beforeinstallprompt", onPrompt); window.removeEventListener("appinstalled", onInstalled); window.removeEventListener("ficusin-chat-push-changed", onPush); };
  }, []);

  const dismiss = () => {
    document.cookie = `${SNOOZE}=1; Path=/; Max-Age=604800; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
    setMode(null);
  };
  const act = async () => {
    setError("");
    if (mode === "install" && !promptEvent) { setSteps(value => !value); return; }
    setBusy(true);
    try {
      if (mode === "notifications") { await enablePush(); setMode(null); }
      else if (promptEvent) { await promptEvent.prompt(); const choice = await promptEvent.userChoice; if (choice.outcome === "accepted") setMode(null); else dismiss(); }
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Попробуйте ещё раз"); }
    finally { setBusy(false); }
  };
  if (!mode) return null;
  return <aside className="install-hint" aria-label={mode === "install" ? "Установить приложение Фикусин" : "Уведомления Фикусина"}>
    <img src="/assets/brand/graphite-v1/app-192-v2.png" alt="" />
    <div><strong>{mode === "install" ? "Фикусин как приложение" : "Оставайтесь на связи"}</strong>
      <span>{mode === "install" ? "Добавьте на экран «Домой» — чат и заказы всегда под рукой." : "Разрешите уведомления о новых сообщениях и вашем заказе."}</span>
      {steps && <ol><li>Откройте меню «Поделиться» в браузере.</li><li>Выберите «На экран Домой» и нажмите «Добавить».</li><li>Откройте Фикусин с нового значка.</li></ol>}
      {error && <span role="alert">{error}</span>}
    </div>
    <button className="install-hint-action" disabled={busy} onClick={() => void act()}>{busy ? "…" : mode === "notifications" ? "Включить" : promptEvent ? "Установить" : steps ? "Свернуть" : "Как установить"}</button>
    <button className="install-hint-close" onClick={dismiss} aria-label="Напомнить об установке позже">×</button>
  </aside>;
}
