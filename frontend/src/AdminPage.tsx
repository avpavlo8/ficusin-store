import { useCallback, useEffect, useState } from "react";
import { currentAdminSection, canOpenSection } from "./adminNavigation";
import { WorkspaceState } from "./WorkspaceUI";
import { Categories, Collections, Products } from "./AdminCatalog";
import { Procurement } from "./AdminProcurement";
import { Customers, Orders } from "./AdminSales";
import { Dashboard } from "./AdminDashboard";
import { WorkspaceSidebar, WorkspaceToolbar } from "./AdminWorkspace";
import "./styles/admin-workspace.css";
import { Settings } from "./AdminSettings";
import { api, selectZeroNumberInput } from "./adminShared";
import type { AdminData, Section } from "./adminTypes";
import { Analytics } from "./AdminAnalytics";
import { AdminMarketplaces } from "./AdminMarketplaces";

export default function AdminPage() {
  const [data, setData] = useState<AdminData | null>(null);
  const [section, setSection] = useState<Section>(currentAdminSection);
  const [error, setError] = useState("");
  // Set when the operator arrives from a dashboard shortcut, so the target
  // section can open on the right row instead of a blank list.
  const [focusOrder, setFocusOrder] = useState("");
  const [wholesaleOnly, setWholesaleOnly] = useState(false);

  useEffect(() => {
    api<AdminData>("/api/v1/admin/dashboard")
      .then(setData)
      .catch((caught) => setError(caught instanceof Error ? caught.message : "Не удалось загрузить панель"));
  }, []);

  useEffect(() => { const read = () => setSection(currentAdminSection()); window.addEventListener("popstate", read); return () => window.removeEventListener("popstate", read); }, []);

  const go = (next: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => {
    setFocusOrder(options?.orderNumber || "");
    setWholesaleOnly(Boolean(options?.wholesaleOnly));
    setError("");
    window.history.pushState(null, "", `/admin?section=${next}`);
    setSection(next);
  };

  const can = useCallback((permission: string) => !!data?.permissions.includes(permission), [data]);

  if (!data) return <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
    <a className="workspace-loading-brand" href="/">Фикусин</a>
    <section className="account-shell"><div className="account-content"><WorkspaceState kind={error ? "error" : "loading"} title={error || "Загружаем панель…"} /></div></section>
  </main>;

  const allowed = canOpenSection(data, section);
  return (
    <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
      <section className="account-shell">
        <WorkspaceSidebar data={data} section={section} onNavigate={go} />
        <div className="account-content">
          <WorkspaceToolbar data={data} section={section} onNavigate={go} />
          <div className="workspace-body" id="workspace-content" tabIndex={-1}>
          {!allowed && <WorkspaceState kind="restricted" title="Доступ к разделу ограничен" detail="Этот раздел доступен владельцу." action={<button onClick={() => go("orders")}>К заказам</button>} />}
          {allowed && <>
          {["products", "categories", "collections"].includes(section) && <nav className="workspace-catalog-nav" aria-label="Каталог">{([{id:"products",label:"Товары"},{id:"categories",label:"Категории"},{id:"collections",label:"Подборки"}] as const).map(item => <a key={item.id} href={`/admin?section=${item.id}`} aria-current={section === item.id ? "page" : undefined} onClick={event => { event.preventDefault(); go(item.id); }}>{item.label}</a>)}</nav>}
          {error && <div className="admin-message error">{error}<button onClick={() => setError("")}>×</button></div>}
          {section === "returns" && <WorkspaceState kind="empty" title="Журнал возвратов ещё не подключён" detail="Физические возвраты растений будут доступны после настройки журнала и связи с документами СБИС." />}
          {section === "finance" && <WorkspaceState kind="unknown" title="Финансовый учёт ещё не подключён" detail="Для отчёта нужны банковские операции, подтверждённая себестоимость и удержания каналов." />}
          {section === "marketplaces" && <AdminMarketplaces onError={setError} />}
          {section === "dashboard" && <Dashboard data={data} onNavigate={go} />}
          {section === "analytics" && <Analytics onError={setError} />}
          {section === "customers" && <Customers can={can} wholesaleOnly={wholesaleOnly} onError={setError} />}
          {section === "orders" && <Orders focusOrder={focusOrder} onError={setError} />}
          {section === "procurement" && <Procurement onError={setError} />}
          {section === "products" && <Products can={can} onError={setError} />}
          {section === "settings" && data.role === "owner" && <Settings onError={setError} />}
          {section === "collections" && <Collections owner={data.role === "owner"} onError={setError} />}
          {section === "categories" && <Categories canEdit={can("products.edit")} owner={data.role === "owner"} onError={setError} />}
          </>}
          </div>
        </div>
      </section>
    </main>
  );
}
