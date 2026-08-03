import { useEffect, useState } from "react";

export type StoreUser = {
  fullName: string;
  adminRole?: "manager" | "owner";
  // Set once a profile photo exists; the value doubles as a cache buster.
  avatarUpdatedAt?: string;
};

function AccountBadge({ user }: { user: StoreUser }) {
  if (user.avatarUpdatedAt) {
    return <img
      className="account-button-photo"
      src={`/api/v1/account/avatar?v=${user.avatarUpdatedAt}`}
      alt=""
    />;
  }
  return <span className="account-button-photo placeholder">
    {user.fullName.trim().charAt(0).toUpperCase() || "◯"}
  </span>;
}

// Cart and favourites live in localStorage, shared by every page. Changes
// made in this tab fire "ficusin-storage"; changes made in another tab
// arrive as the browser's own "storage" event.
export const STORAGE_EVENT = "ficusin-storage";

function readStoredCounts() {
  try {
    const favorites = JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[];
    const cart = JSON.parse(localStorage.getItem("ficusin-cart") || "{}") as Record<string, number>;
    return {
      favorites: favorites.length,
      cart: Object.values(cart).reduce((sum, value) => sum + value, 0),
    };
  } catch {
    return { favorites: 0, cart: 0 };
  }
}

function useStoredCounts() {
  const [counts, setCounts] = useState(readStoredCounts);
  useEffect(() => {
    const sync = () => setCounts(readStoredCounts());
    window.addEventListener("storage", sync);
    window.addEventListener(STORAGE_EVENT, sync);
    return () => {
      window.removeEventListener("storage", sync);
      window.removeEventListener(STORAGE_EVENT, sync);
    };
  }, []);
  return counts;
}

export function useStoreUser() {
  const [user, setUser] = useState<StoreUser | null>(null);
  useEffect(() => {
    fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => response.ok ? (await response.json() as { user: StoreUser }).user : null)
      .then(setUser).catch(() => setUser(null));
  }, []);
  return user;
}

export function AccountMenu({ user }: { user: StoreUser | null }) {
  if (!user) return <a className="account-button" href="/login"><span>◯</span><span>Войти</span></a>;
  const staff = user.adminRole === "manager" || user.adminRole === "owner";
  const name = user.fullName.trim().split(/\s+/)[0] || "Профиль";
  if (!staff) return <a className="account-button" href="/account"><AccountBadge user={user} /><span>{name}</span></a>;
  return <details className="account-menu"><summary className="account-button"><AccountBadge user={user} /><span>{name}</span></summary>
    <div><a href="/account">Личный профиль</a><a href="/admin">Панель управления</a></div>
  </details>;
}

// Pages without their own product list (a product card, the account area)
// still show the search box: submitting it hands the query to the catalog,
// which is the only page that can display results.
function CatalogSearchForm() {
  const [value, setValue] = useState("");
  return <form
    className="header-search"
    onSubmit={(event) => {
      event.preventDefault();
      const trimmed = value.trim();
      if (!trimmed) return;
      window.location.assign(`/?q=${encodeURIComponent(trimmed)}#catalog`);
    }}
  >
    <span aria-hidden="true">⌕</span>
    <input value={value} onChange={(event) => setValue(event.target.value)} placeholder="Поиск по каталогу" />
  </form>;
}

export function StoreHeader({ cartCount, favoritesCount, query, onQueryChange }: { cartCount?: number; favoritesCount?: number; query?: string; onQueryChange?: (value: string) => void }) {
  const user = useStoreUser();
  // Pages that own the cart and favourites (the catalogue, a product card)
  // pass their live numbers in. The account area and the admin panel do not
  // keep that state, so the header reads it from storage itself instead of
  // showing zeroes.
  const stored = useStoredCounts();
  const favorites = favoritesCount ?? stored.favorites;
  const cart = cartCount ?? stored.cart;
  return <><div className="announcement"><span>Бережно упакуем каждое растение</span><span>Доставка по Рязани и всей России</span></div>
    <header className="header"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a>
      <nav className="desktop-nav"><a href="/#catalog">Каталог</a><a href="/#care">Уход</a><a href="/#delivery">Доставка</a></nav>
      <div className="header-actions">
        {onQueryChange
          ? <label className="header-search"><span>⌕</span><input value={query || ""} onChange={(event) => onQueryChange(event.target.value)} placeholder="Поиск по каталогу" /></label>
          : <CatalogSearchForm />}
        <AccountMenu user={user} />
        <a className="favorites-button" href="/favorites" aria-label={`Избранное, товаров: ${favorites}`}><span>♥</span><b>{favorites}</b></a>
        <a className="cart-button" href="/?cart=1"><span>Корзина</span><b>{cart}</b></a>
      </div>
    </header></>;
}

