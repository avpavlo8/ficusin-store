import { useEffect, useState } from "react";
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

export default function AdminPage() {
  const [data, setData] = useState<AdminData | null>(null);
  const [section, setSection] = useState<Section>("dashboard");
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

  const go = (next: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => {
    setFocusOrder(options?.orderNumber || "");
    setWholesaleOnly(Boolean(options?.wholesaleOnly));
    setError("");
    setSection(next);
  };

  if (!data) return <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
    <a className="workspace-loading-brand" href="/">Фикусин</a>
    <section className="account-shell"><div className="account-content"><p>{error || "Загружаем панель…"}</p></div></section>
  </main>;

  const can = (permission: string) => data.permissions.includes(permission);
  return (
    <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
      <section className="account-shell">
        <WorkspaceSidebar data={data} section={section} onNavigate={go} />
        <div className="account-content">
          <WorkspaceToolbar data={data} section={section} onNavigate={go} />
          <div className="workspace-body">
          {error && <div className="admin-message error">{error}<button onClick={() => setError("")}>×</button></div>}
          {section === "dashboard" && <Dashboard data={data} onNavigate={go} />}
          {section === "analytics" && <Analytics onError={setError} />}
          {section === "customers" && <Customers can={can} wholesaleOnly={wholesaleOnly} onError={setError} />}
          {section === "orders" && <Orders focusOrder={focusOrder} onError={setError} />}
          {section === "procurement" && <Procurement onError={setError} />}
          {section === "products" && <Products can={can} onError={setError} />}
          {section === "settings" && data.role === "owner" && <Settings onError={setError} />}
          {section === "collections" && <Collections onError={setError} />}
          {section === "categories" && <Categories canEdit={data.role === "owner"} owner={data.role === "owner"} onError={setError} />}
          </div>
        </div>
      </section>
    </main>
  );
}
