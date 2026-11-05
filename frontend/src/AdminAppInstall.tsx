import { useEffect, useState, useSyncExternalStore } from "react";
import {
  clearAdminInstallPrompt,
  getAdminInstallPrompt,
  subscribeAdminInstall,
} from "./lib/adminApp";
import { isIOS, isStandalone } from "./lib/pwa";

export function AdminAppInstall() {
  const prompt = useSyncExternalStore(subscribeAdminInstall, getAdminInstallPrompt);
  const [installed, setInstalled] = useState(isStandalone);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Предложение установки показано в двух местах (боковое меню и «Ещё»),
  // поэтому об установке сообщаем всем экземплярам сразу.
  useEffect(() => {
    const hide = () => setInstalled(true);
    window.addEventListener("admin:installed", hide);
    window.addEventListener("appinstalled", hide);
    return () => {
      window.removeEventListener("admin:installed", hide);
      window.removeEventListener("appinstalled", hide);
    };
  }, []);
  const install = async () => {
    if (!prompt) return;
    setBusy(true);
    setError("");
    try {
      await prompt.prompt();
      const choice = await prompt.userChoice;
      if (choice.outcome === "accepted") window.dispatchEvent(new Event("admin:installed"));
      clearAdminInstallPrompt();
    } catch {
      setError("Не удалось открыть установку. Попробуйте через меню браузера.");
    } finally {
      setBusy(false);
    }
  };
  if (installed) return null;
  return (
    <details className="admin-app-install">
      <summary>Приложение</summary>
      <div className="admin-app-install-panel">
        <img src="/assets/brand/admin-v1/app-192.png" alt="" width="48" height="48" />
        <strong>Фикусин — управление</strong>
        <p>Отдельная иконка для быстрого доступа к заказам, закупкам и каталогу.</p>
        {prompt ? (
          <button type="button" onClick={() => void install()} disabled={busy}>
            {busy ? "Открываем установку…" : "Установить"}
          </button>
        ) : isIOS() ? (
          <ol>
            <li>Откройте эту страницу в Safari.</li>
            <li>Нажмите «Поделиться» → «На экран Домой».</li>
            <li>Нажмите «Добавить» и откройте новую иконку.</li>
          </ol>
        ) : (
          <p>
            В меню браузера выберите «Установить приложение» или «Добавить на главный экран». Если
            такого пункта нет, откройте страницу в Chrome или Edge.
          </p>
        )}
        {error && <p role="alert">{error}</p>}
      </div>
    </details>
  );
}
