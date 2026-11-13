import { useEffect, useState } from "react";
import {
  clearStoreInstallPrompt,
  getStoreInstallPrompt,
  type InstallPromptEvent,
  isIOS,
  isStandalone,
} from "./lib/pwa";

// Installation is offered by the first-visit dialog or explicitly in Account.
// Neither this component nor a page load requests notification permission.
export function InstallHint({
  inline = false,
  onInstalled,
}: {
  inline?: boolean;
  onInstalled?: () => void;
}) {
  const [promptEvent, setPromptEvent] = useState<InstallPromptEvent | null>(getStoreInstallPrompt);
  const [installed, setInstalled] = useState(isStandalone);
  const [steps, setSteps] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const ready = () => setPromptEvent(getStoreInstallPrompt());
    const complete = () => {
      setInstalled(true);
      setPromptEvent(null);
      onInstalled?.();
    };
    window.addEventListener("ficusin-install-ready", ready);
    window.addEventListener("appinstalled", complete);
    return () => {
      window.removeEventListener("ficusin-install-ready", ready);
      window.removeEventListener("appinstalled", complete);
    };
  }, [onInstalled]);
  const install = async () => {
    setError("");
    if (!promptEvent) {
      setSteps((value) => !value);
      return;
    }
    setBusy(true);
    try {
      await promptEvent.prompt();
      const choice = await promptEvent.userChoice;
      clearStoreInstallPrompt();
      setPromptEvent(null);
      if (choice.outcome === "accepted") {
        setInstalled(true);
        onInstalled?.();
      }
    } catch (caught) {
      setError(
        caught instanceof Error
          ? caught.message
          : "Не удалось открыть установку. Попробуйте ещё раз.",
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <aside
      className={inline ? "home-install" : "install-hint"}
      aria-label="Установить приложение Фикусин"
    >
      <img src="/assets/brand/graphite-v1/app-192-v2.png" alt="" />
      <div>
        <strong>{installed ? "Фикусин уже установлен" : "Фикусин на экране «Домой»"}</strong>
        <span>
          {installed
            ? "Открывайте приложение с иконки. Уведомления о заказах и сообщениях настраиваются ниже."
            : "Добавьте Фикусин на главный экран, чтобы открывать магазин одним нажатием и получать уведомления."}
        </span>
        {steps && !installed && (
          <ol>
            <li>{isIOS() ? "Откройте меню «Поделиться» в Safari." : "Откройте меню браузера."}</li>
            <li>
              {isIOS()
                ? "Выберите «На экран Домой» и нажмите «Добавить»."
                : "Выберите «Установить приложение» или «Добавить на главный экран», если этот пункт доступен."}
            </li>
            <li>
              Откройте Фикусин с нового значка. В личном кабинете включите нужные уведомления.
            </li>
          </ol>
        )}
        {error && <span role="alert">{error}</span>}
      </div>
      {!installed && (
        <button
          type="button"
          className="install-hint-action"
          disabled={busy}
          onClick={() => void install()}
        >
          {busy ? "…" : promptEvent ? "Установить" : steps ? "Свернуть" : "Как установить"}
        </button>
      )}
    </aside>
  );
}
