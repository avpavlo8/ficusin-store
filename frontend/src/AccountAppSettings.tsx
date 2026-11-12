import { InstallHint } from "./InstallHint";
import { PushToggle } from "./PushToggle";
import "./styles/app-discovery.css";
export function AccountAppSettings() {
  return (
    <section className="account-app-settings" aria-labelledby="account-app-title">
      <h2 id="account-app-title">Приложение</h2>
      <InstallHint inline />
      <h3>Уведомления</h3>
      <PushToggle />
    </section>
  );
}
