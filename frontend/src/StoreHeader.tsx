import { useEffect, useState } from "react";
import { InstallHint } from "./InstallHint";
import { CatalogSearch } from "./CatalogSearch";

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

// Keeps the page behind an open panel from scrolling. It toggles a class
// rather than an inline style because the cart drawer does the same thing:
// two inline styles would fight over who gets to clear it.
function useBodyLock(locked: boolean, name: string) {
  useEffect(() => {
    document.body.classList.toggle(name, locked);
    return () => document.body.classList.remove(name);
  }, [locked, name]);
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

// Line icons for the bottom bar. Drawn rather than typed because the text
// symbols a phone substitutes for these glyphs differ wildly between
// Android and iOS.
const icons = {
  catalog: "M3 4h7v7H3V4Zm11 0h7v7h-7V4ZM3 15h7v7H3v-7Zm11 0h7v7h-7v-7Z",
  heart: "M12 20.4 4.3 12.8a4.7 4.7 0 0 1 6.7-6.6l1 1 1-1a4.7 4.7 0 0 1 6.7 6.6L12 20.4Z",
  bag: "M5.6 8h12.8l1.1 12.2H4.5L5.6 8Zm3.4 0V6.6a3 3 0 0 1 6 0V8",
  person: "M12 11.8a3.9 3.9 0 1 0 0-7.8 3.9 3.9 0 0 0 0 7.8ZM4.2 20a7.8 7.8 0 0 1 15.6 0",
  search: "M11 4.2a6.8 6.8 0 1 0 0 13.6 6.8 6.8 0 0 0 0-13.6ZM15.9 15.9 20 20",
};

function Icon({ path }: { path: string }) {
  return <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path d={path} fill="none" stroke="currentColor" strokeWidth="1.7"
      strokeLinecap="round" strokeLinejoin="round" />
  </svg>;
}

// Pages without their own product list (a product card, the account area)
// still offer search: submitting hands the query to the catalogue, which is
// the only page that can display results.
// On a phone the search box hides behind the magnifier so the header keeps
// room for the brand. Opening it focuses the field straight away, because
// the only reason to tap the magnifier is to start typing.
function MobileSearch({
  query,
  onQueryChange,
  onClose,
}: {
  query?: string;
  onQueryChange?: (value: string) => void;
  onClose: () => void;
}) {
  return <div className="mobile-search">
    <CatalogSearch value={query} onChange={onQueryChange} className="mobile-catalog-search" autoFocus />
    <button type="button" onClick={onClose} aria-label="Закрыть поиск">×</button>
  </div>;
}

function MobileMenu({
  user,
  favorites,
  onClose,
}: {
  user: StoreUser | null;
  favorites: number;
  onClose: () => void;
}) {
  return <aside className="mobile-menu open">
    <button onClick={onClose} aria-label="Закрыть меню">×</button>
    {user ? <>
      <a href="/account">{user.fullName.trim().split(/\s+/)[0] || "Профиль"}</a>
      {(user.adminRole === "manager" || user.adminRole === "owner") &&
        <a href="/admin">Панель управления</a>}
    </> : <>
      <a href="/login">Войти</a>
      <a href="/register">Регистрация</a>
    </>}
    <a href="/#catalog" onClick={onClose}>Каталог</a>
    <a href="/favorites">Избранное ({favorites})</a>
    <a href="/delivery-and-returns">Доставка и возврат</a>
  </aside>;
}

// The bar pinned to the bottom of a phone screen. Everything a shopper
// reaches for repeatedly sits here, within thumb reach, on every page.
function MobileTabBar({
  user,
  favorites,
  cart,
  onCartClick,
}: {
  user: StoreUser | null;
  favorites: number;
  cart: number;
  onCartClick?: () => void;
}) {
  const cartInside = <>
    <span className="tab-icon">
      <Icon path={icons.bag} />
      {cart > 0 && <b>{cart}</b>}
    </span>
    <small>Корзина</small>
  </>;
  return <nav className="tab-bar" aria-label="Разделы магазина">
    <a href="/#catalog">
      <span className="tab-icon"><Icon path={icons.catalog} /></span>
      <small>Каталог</small>
    </a>
    <a href="/favorites">
      <span className="tab-icon">
        <Icon path={icons.heart} />
        {favorites > 0 && <b>{favorites}</b>}
      </span>
      <small>Избранное</small>
    </a>
    {onCartClick
      ? <button type="button" onClick={onCartClick}>{cartInside}</button>
      : <a href="/cart">{cartInside}</a>}
    <a href={user ? "/account" : "/login"}>
      <span className="tab-icon"><Icon path={icons.person} /></span>
      <small>{user ? "Профиль" : "Войти"}</small>
    </a>
  </nav>;
}

export function StoreHeader({
  cartCount,
  favoritesCount,
  query,
  onQueryChange,
  onCartClick,
  showTabBar = true,
  showSearch = true,
}: {
  cartCount?: number;
  favoritesCount?: number;
  query?: string;
  onQueryChange?: (value: string) => void;
  // Supplied by the storefront, which opens the cart as a drawer instead of
  // navigating away from the catalogue.
  onCartClick?: () => void;
  showTabBar?: boolean;
  // The storefront carries its own search bar under the header, so it asks
  // for the header field to step aside. Every other page keeps it: there it
  // is the only way to search at all.
  showSearch?: boolean;
}) {
  const user = useStoreUser();
  // Pages that own the cart and favourites (the catalogue, a product card)
  // pass their live numbers in. The account area and the admin panel do not
  // keep that state, so the header reads it from storage itself instead of
  // showing zeroes.
  const stored = useStoredCounts();
  const favorites = favoritesCount ?? stored.favorites;
  const cart = cartCount ?? stored.cart;
  const [menuOpen, setMenuOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  useBodyLock(menuOpen, "menu-open");

  const cartLabel = `Корзина, товаров: ${cart}`;
  return <><div className="announcement"><span>Бережно упакуем каждое растение</span><span>Доставка по Рязани и всей России</span></div>
    <header className="header">
      <button
        className="menu-button"
        onClick={() => setMenuOpen(true)}
        aria-label="Открыть меню"
        aria-expanded={menuOpen}
      >☰</button>
      <a className="brand" href="/"><span className="brand-mark">⌇</span><span className="brand-text"><span>Фикусин</span><small>магазин растений</small></span></a>
      <nav className="desktop-nav"><a href="/#catalog">Каталог</a><a href="/favorites">Избранное</a><a href="/delivery-and-returns">Доставка и возврат</a></nav>
      <div className="header-actions">
        {showSearch && <>
          <CatalogSearch value={query} onChange={onQueryChange} />
          <button
            className="search-toggle"
            onClick={() => setSearchOpen((open) => !open)}
            aria-label="Поиск по каталогу"
            aria-expanded={searchOpen}
          ><Icon path={icons.search} /></button>
        </>}
        <AccountMenu user={user} />
        <a className="favorites-button" href="/favorites" aria-label={`Избранное, товаров: ${favorites}`}><span aria-hidden="true">♥</span><b>{favorites}</b></a>
        {onCartClick
          ? <button className="cart-button" onClick={onCartClick} aria-label={cartLabel}><span>Корзина</span><b>{cart}</b></button>
          : <a className="cart-button" href="/cart" aria-label={cartLabel}><span>Корзина</span><b>{cart}</b></a>}
      </div>
    </header>
    {showSearch && searchOpen && <MobileSearch query={query} onQueryChange={onQueryChange} onClose={() => setSearchOpen(false)} />}
    {menuOpen && <>
      <button className="overlay" aria-label="Закрыть меню" onClick={() => setMenuOpen(false)} />
      <MobileMenu user={user} favorites={favorites} onClose={() => setMenuOpen(false)} />
    </>}
    {showTabBar && <>
      <InstallHint />
      <MobileTabBar user={user} favorites={favorites} cart={cart} onCartClick={onCartClick} />
    </>}
  </>;
}
