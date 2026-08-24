import { useEffect, useRef, useState } from "react";
import { InstallHint } from "./InstallHint";
import { CatalogSearch } from "./CatalogSearch";
import { useSharedCart } from "./lib/cart";

export type StoreUser = {
  fullName: string;
  adminRole?: "manager" | "owner";
  // Set once a profile photo exists; the value doubles as a cache buster.
  avatarUpdatedAt?: string;
};

export type HeaderMenuItem = { id: number; label: string; slug?: string; children?: HeaderMenuItem[] };

function closeOtherHeaderMenus(current: HTMLDetailsElement) {
  if (!current.open) return;
  document.querySelectorAll<HTMLDetailsElement>(".header details[open]").forEach((details) => {
    if (details !== current && !details.contains(current)) details.removeAttribute("open");
  });
}

function HeaderMenuBranch({ item, onPick }: { item: HeaderMenuItem; onPick?: (id: number) => void }) {
  if (item.children?.length) return <details className="header-submenu" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>{item.label}<span>›</span></summary><div>{item.children.map((child) => <HeaderMenuBranch item={child} onPick={onPick} key={child.id} />)}</div></details>;
  return <a href={item.slug ? `/catalog/${encodeURIComponent(item.slug)}` : `/?category=${item.id}#catalog`} onClick={(event) => { if (onPick) { event.preventDefault(); onPick(item.id); } document.querySelectorAll<HTMLDetailsElement>(".header details[open]").forEach((details) => details.removeAttribute("open")); }}>{item.label}<span>→</span></a>;
}

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

// Favourites remain local for now. Cart contents are never stored here: the
// header reads the same server-backed cart as the catalogue and checkout.
export const STORAGE_EVENT = "ficusin-storage";

function readStoredCounts() {
  try {
    const favorites = JSON.parse(localStorage.getItem("ficusin-favorites") || "[]") as string[];
    return {
      favorites: favorites.length,
    };
  } catch {
    return { favorites: 0 };
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

export function AccountMenu({ user, iconOnly = false }: { user: StoreUser | null; iconOnly?: boolean }) {
  if (!user) return <a className={iconOnly ? "account-button icon-only" : "account-button"} href="/login" aria-label="Войти"><span>{iconOnly ? <Icon path={icons.person} /> : "◯"}</span>{!iconOnly && <span>Войти</span>}</a>;
  const staff = user.adminRole === "manager" || user.adminRole === "owner";
  const name = user.fullName.trim().split(/\s+/)[0] || "Профиль";
  if (!staff) return <a className={iconOnly ? "account-button icon-only" : "account-button"} href="/account" aria-label="Профиль">{iconOnly ? <Icon path={icons.person} /> : <><AccountBadge user={user} /><span>{name}</span></>}</a>;
  return <details className="account-menu" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary className={iconOnly ? "account-button icon-only" : "account-button"}>{iconOnly ? <Icon path={icons.person} /> : <><AccountBadge user={user} /><span>{name}</span></>}</summary>
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
    <span className="mobile-menu-heading">О нас</span>
    <a href="/#about" onClick={onClose}>О компании</a>
    <a href="/contacts">Контакты</a>
    <a href="/offer">Публичная оферта</a>
    <a href="/privacy">Политика конфиденциальности</a>
    <a href="/requisites">Реквизиты</a>
  </aside>;
}

function AboutMenu() {
  return <details className="header-dropdown" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>О нас <span>⌄</span></summary><div><a href="/#about">О компании</a><a href="/contacts">Контакты</a><a href="/offer">Публичная оферта</a><a href="/privacy">Политика конфиденциальности</a><a href="/requisites">Реквизиты</a></div></details>;
}

// The bar pinned to the bottom of a phone screen. Everything a shopper
// reaches for repeatedly sits here, within thumb reach, on every page.
function MobileTabBar({
  user,
  favorites,
  cart,
  onCartClick,
  onSearchClick,
}: {
  user: StoreUser | null;
  favorites: number;
  cart: number;
  onCartClick?: () => void;
  onSearchClick: () => void;
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
    <button type="button" onClick={onSearchClick}>
      <span className="tab-icon"><Icon path={icons.search} /></span>
      <small>Поиск</small>
    </button>
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
  homeNavigation = true,
  catalogMenuItems = [],
  plantMenuItems = [],
  onHomeCategoryPick,
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
  homeNavigation?: boolean;
  catalogMenuItems?: HeaderMenuItem[];
  plantMenuItems?: HeaderMenuItem[];
  onHomeCategoryPick?: (id: number) => void;
}) {
  const user = useStoreUser();
  // Pages that own the cart and favourites (the catalogue, a product card)
  // pass their live numbers in. The account area and the admin panel do not
  // keep that state, so the header reads it from storage itself instead of
  // showing zeroes.
  const stored = useStoredCounts();
  const [serverCart] = useSharedCart();
  const favorites = favoritesCount ?? stored.favorites;
  const cart = cartCount ?? Object.values(serverCart).reduce((sum, value) => sum + value, 0);
  const [menuOpen, setMenuOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [fallbackMenus, setFallbackMenus] = useState<{catalog:HeaderMenuItem[];plants:HeaderMenuItem[]}>({catalog:[],plants:[]});
  const headerRef = useRef<HTMLElement>(null);
  useBodyLock(menuOpen, "menu-open");
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!headerRef.current?.contains(event.target as Node)) headerRef.current?.querySelectorAll<HTMLDetailsElement>("details[open]").forEach((details) => details.removeAttribute("open"));
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);
  useEffect(() => {
    if (!homeNavigation || catalogMenuItems.length || plantMenuItems.length) return;
    fetch("/api/v1/categories", { cache: "no-store" }).then((response) => response.json()).then((body: {categories?:Array<{id:number;parentId:number|null;name:string;slug:string;sortOrder:number}>}) => {
      const categories = body.categories || [];
      const children = new Map<number|null,typeof categories>();
      categories.forEach((item) => children.set(item.parentId,[...(children.get(item.parentId)||[]),item]));
      const order = (items:typeof categories) => [...items].sort((a,b)=>a.sortOrder-b.sortOrder||a.name.localeCompare(b.name,"ru"));
      const branch = (item:typeof categories[number]):HeaderMenuItem => ({id:item.id,label:item.name,slug:item.slug,children:order(children.get(item.id)||[]).map(branch)});
      const catalog = order(children.get(null)||[]).map(branch);
      const plantRoot = categories.find((item)=>item.parentId==null&&/растен/i.test(item.name));
      const leaves = (parentId:number):typeof categories => order(children.get(parentId)||[]).flatMap((item)=>children.get(item.id)?.length?leaves(item.id):[item]);
      setFallbackMenus({catalog,plants:plantRoot?leaves(plantRoot.id).map((item)=>({id:item.id,label:item.name,slug:item.slug})):[]});
    }).catch(() => setFallbackMenus({catalog:[],plants:[]}));
  },[homeNavigation,catalogMenuItems.length,plantMenuItems.length]);

  const resolvedCatalogMenuItems = catalogMenuItems.length ? catalogMenuItems : fallbackMenus.catalog;
  const resolvedPlantMenuItems = plantMenuItems.length ? plantMenuItems : fallbackMenus.plants;
  const categoryPick = onHomeCategoryPick;

  const cartLabel = `Корзина, товаров: ${cart}`;
  return <><div className="announcement" hidden />
    <header className="header store-header" ref={headerRef}>
      <button
        className="menu-button"
        onClick={() => setMenuOpen(true)}
        aria-label="Открыть меню"
        aria-expanded={menuOpen}
      >☰</button>
      <a className="brand" href="/"><span className="brand-mark">⌇</span><span className="brand-text"><span>Фикусин</span><small>магазин растений</small></span></a>
      <nav className="desktop-nav">{homeNavigation ? <>
        <details className="header-dropdown" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>Каталог <span>⌄</span></summary><div>{resolvedCatalogMenuItems.map((item) => <HeaderMenuBranch item={item} onPick={categoryPick} key={item.id} />)}</div></details>
        <details className="header-dropdown" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>Растения <span>⌄</span></summary><div>{resolvedPlantMenuItems.map((item) => <HeaderMenuBranch item={item} onPick={categoryPick} key={item.id} />)}</div></details>
        <a href="/#care">Уход</a><a href="/delivery-and-returns">Доставка и оплата</a><a href="/#blog">Блог</a><AboutMenu/>
      </> : <><a href="/#catalog">Каталог</a><a href="/favorites">Избранное</a><a href="/delivery-and-returns">Доставка и возврат</a><AboutMenu/></>}</nav>
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
        <a className="favorites-button" href="/favorites" aria-label={`Избранное, товаров: ${favorites}`}><span aria-hidden="true"><Icon path={icons.heart} /></span>{favorites > 0 && <b>{favorites}</b>}</a>
        <AccountMenu user={user} iconOnly />
        {onCartClick
          ? <button className="cart-button" onClick={onCartClick} aria-label={cartLabel}><span><Icon path={icons.bag} /></span>{cart > 0 && <b>{cart}</b>}</button>
          : <a className="cart-button" href="/cart" aria-label={cartLabel}><span><Icon path={icons.bag} /></span>{cart > 0 && <b>{cart}</b>}</a>}
      </div>
    </header>
    {showSearch && searchOpen && <MobileSearch query={query} onQueryChange={onQueryChange} onClose={() => setSearchOpen(false)} />}
    {menuOpen && <>
      <button className="overlay" aria-label="Закрыть меню" onClick={() => setMenuOpen(false)} />
      <MobileMenu user={user} favorites={favorites} onClose={() => setMenuOpen(false)} />
    </>}
    {showTabBar && <>
      <InstallHint />
      <MobileTabBar user={user} favorites={favorites} cart={cart} onCartClick={onCartClick} onSearchClick={() => setSearchOpen(true)} />
    </>}
  </>;
}
