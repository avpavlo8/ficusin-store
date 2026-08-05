import { useEffect, useState } from "react";
import { InstallPromptEvent, isIOS, isStandalone } from "./lib/pwa";

const DISMISSED = "ficusin-install-dismissed";

// A one-line invitation to keep the shop on the home screen. It appears only
// on a phone, only once, and never when the shop is already installed —
// nobody needs to be nagged about something they already did.
export function InstallHint() {
  const [promptEvent, setPromptEvent] = useState<InstallPromptEvent | null>(null);
  const [showIOSSteps, setShowIOSSteps] = useState(false);
  const [hidden, setHidden] = useState(true);

  useEffect(() => {
    if (isStandalone() || localStorage.getItem(DISMISSED)) return;

    // Chrome hands us the prompt when it decides the site qualifies.
    const onPrompt = (event: Event) => {
      event.preventDefault();
      setPromptEvent(event as InstallPromptEvent);
      setHidden(false);
    };
    window.addEventListener("beforeinstallprompt", onPrompt);

    // Safari has no such event, so on an iPhone we explain the two taps
    // instead — but only after a short delay, so it does not greet a
    // first-time visitor before they have seen anything.
    let timer = 0;
    if (isIOS()) {
      timer = window.setTimeout(() => {
        setShowIOSSteps(true);
        setHidden(false);
      }, 20_000);
    }

    const onInstalled = () => setHidden(true);
    window.addEventListener("appinstalled", onInstalled);
    return () => {
      window.removeEventListener("beforeinstallprompt", onPrompt);
      window.removeEventListener("appinstalled", onInstalled);
      window.clearTimeout(timer);
    };
  }, []);

  function dismiss() {
    localStorage.setItem(DISMISSED, "1");
    setHidden(true);
  }

  async function install() {
    if (!promptEvent) return;
    await promptEvent.prompt();
    await promptEvent.userChoice;
    dismiss();
  }

  if (hidden) return null;

  return <aside className="install-hint" role="complementary">
    <img src="/icon-192.png" alt="" />
    <div>
      <strong>Фикусин на домашний экран</strong>
      {showIOSSteps
        ? <span>Нажмите «Поделиться», затем «На экран „Домой“».</span>
        : <span>Открывается как приложение, без адресной строки.</span>}
    </div>
    {promptEvent && <button className="install-hint-action" onClick={install}>Добавить</button>}
    <button className="install-hint-close" onClick={dismiss} aria-label="Скрыть подсказку">×</button>
  </aside>;
}
