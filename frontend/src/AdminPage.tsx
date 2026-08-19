import { useEffect, useState } from "react";
import { StoreHeader, useStoreUser } from "./StoreHeader";
import { Categories, Collections, Products } from "./AdminCatalog";
import { Procurement } from "./AdminProcurement";
import { Customers, Dashboard, Orders } from "./AdminSales";
import { Settings } from "./AdminSettings";
import { api, roleLabel, selectZeroNumberInput } from "./adminShared";
import type { AdminData, Section } from "./adminTypes";

export default function AdminPage() {
  const [data, setData] = useState<AdminData | null>(null);
  const [section, setSection] = useState<Section>("dashboard");
  const [error, setError] = useState("");
  // Set when the operator arrives from a dashboard shortcut, so the target
  // section can open on the right row instead of a blank list.
  const [focusOrder, setFocusOrder] = useState("");
  const [wholesaleOnly, setWholesaleOnly] = useState(false);
  const user = useStoreUser();

  useEffect(() => {
    api<AdminData>("/api/v1/admin/dashboard")
      .then(setData)
      .catch((caught) => setError(caught instanceof Error ? caught.message : "Не удалось загрузить панель"));
  }, []);

  const go = (next: Section, options?: { orderNumber?: string; wholesaleOnly?: boolean }) => {
    setFocusOrder(options?.orderNumber || "");
    setWholesaleOnly(Boolean(options?.wholesaleOnly));
    setSection(next);
  };

  if (!data) return <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
    <StoreHeader showTabBar={false} />
    <section className="account-shell"><div className="account-content"><p>{error || "Загружаем панель…"}</p></div></section>
  </main>;

  const can = (permission: string) => data.permissions.includes(permission);
  const initial = data.user.fullName.trim().charAt(0).toUpperCase() || "Ф";
  return (
    <main className="account-page admin-page" onFocusCapture={selectZeroNumberInput} onClickCapture={selectZeroNumberInput}>
      <StoreHeader showTabBar={false} />
      <section className="account-shell">
        <aside className="account-sidebar">
          <div className="account-avatar">
            {user?.avatarUpdatedAt
              ? <img src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`} alt="" />
              : <span>{initial}</span>}
          </div>
          <h1>{data.user.fullName}</h1>
          <p>{roleLabel(data.role)}</p>
          <nav>
            <Nav active={section === "dashboard"} onClick={() => go("dashboard")}>Обзор</Nav>
            {can("products.read") && <Nav active={section === "products"} onClick={() => go("products")}>Товары</Nav>}
            {can("products.read") && <Nav active={section === "categories"} onClick={() => go("categories")}>Категории</Nav>}
            {can("products.read") && <Nav active={section === "collections"} onClick={() => go("collections")}>Подборки</Nav>}
            {can("orders.read") && <Nav active={section === "orders"} onClick={() => go("orders")}>Заказы</Nav>}
            {can("procurement.read") && <Nav active={section === "procurement"} onClick={() => go("procurement")}>Закупки</Nav>}
            {can("customers.read") && <Nav active={section === "customers"} onClick={() => go("customers")}>Клиенты</Nav>}
            {data.role === "owner" && <Nav active={section === "settings"} onClick={() => go("settings")}>Настройки</Nav>}
          </nav>
          <a className="account-switch" href="/account">Личный кабинет →</a>
          <a className="account-switch" href="/">Вернуться в магазин →</a>
        </aside>
        <div className="account-content">
          {error && <div className="admin-message error">{error}<button onClick={() => setError("")}>×</button></div>}
          {section === "dashboard" && <Dashboard data={data} onNavigate={go} />}
          {section === "customers" && <Customers can={can} wholesaleOnly={wholesaleOnly} onError={setError} />}
          {section === "orders" && <Orders focusOrder={focusOrder} onError={setError} />}
          {section === "procurement" && <Procurement onError={setError} />}
          {section === "products" && <Products can={can} onError={setError} />}
          {section === "settings" && data.role === "owner" && <Settings onError={setError} />}
          {section === "collections" && <Collections onError={setError} />}
          {section === "categories" && <Categories canEdit={can("products.edit")} onError={setError} />}
        </div>
      </section>
    </main>
  );
}

export function Nav({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return <button className={active ? "active" : ""} onClick={onClick}><span className="admin-nav-dot" aria-hidden="true" />{children}</button>;
}
