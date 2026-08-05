import { useEffect, useState } from "react";
import { disablePush, enablePush, isIOS, isStandalone, pushState, pushSupported } from "./lib/pwa";

// Lets a customer turn order notifications on from their account page.
// Deliberately a plain switch with no persuasion: a shop that begs for the
// notification permission on arrival is the reason people block it forever.
export function PushToggle() {
  const [state, setState] = useState({ available: false, subscribed: false, blocked: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    pushState()
      .then((next) => { if (!cancelled) { setState(next); setReady(true); } })
      .catch(() => { if (!cancelled) setReady(true); });
    return () => { cancelled = true; };
  }, []);

  async function toggle() {
    setBusy(true);
    setError("");
    try {
      if (state.subscribed) await disablePush();
      else await enablePush();
      setState(await pushState());
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Не удалось изменить настройку");
    } finally {
      setBusy(false);
    }
  }

  if (!ready || !pushSupported() || !state.available) return null;

  // Safari only delivers notifications to a site kept on the home screen,
  // so on iPhone there is no point offering the switch before that.
  if (isIOS() && !isStandalone()) {
    return <div className="push-toggle">
      <div>
        <strong>Уведомления о заказе</strong>
        <span>Чтобы получать их на iPhone, добавьте «Фикусин» на домашний экран: «Поделиться» → «На экран „Домой“».</span>
      </div>
    </div>;
  }

  return <div className="push-toggle">
    <div>
      <strong>Уведомления о заказе</strong>
      <span>{state.blocked
        ? "Уведомления запрещены в настройках браузера — разрешите их для ficusin.ru."
        : "Сообщим, когда заказ будет готов, отправлен или получен. Ничего рекламного."}</span>
      {error && <em className="push-toggle-error">{error}</em>}
    </div>
    <button
      className={state.subscribed ? "ghost-button" : "primary-button"}
      onClick={toggle}
      disabled={busy || state.blocked}
    >{busy ? "…" : state.subscribed ? "Выключить" : "Включить"}</button>
  </div>;
}
