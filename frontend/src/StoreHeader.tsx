import { useCallback, useEffect, useRef, useState } from "react";
import { InstallHint } from "./InstallHint";
import { CatalogSearch } from "./CatalogSearch";
import { useSharedCart } from "./lib/cart";
import { useFavorites, useAvailableFavoriteCount, FAVORITES_EVENT } from "./lib/favorites";
import { useModalFocus } from "./lib/useModalFocus";
import { sharedAPIJSON } from "./lib/api";
import { orderStorefrontCategories } from "./lib/category-order";

export type StoreUser = {
  fullName: string;
  adminRole?: "manager" | "administrator" | "owner";
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
  if (item.children?.length) return <details className="header-submenu" onToggle={(event) => { event.stopPropagation(); closeOtherHeaderMenus(event.currentTarget); }}><summary>{item.label}<span>›</span></summary><div>{item.children.map((child) => <HeaderMenuBranch item={child} onPick={onPick} key={child.id} />)}</div></details>;
  return <a href={item.slug ? `/catalog/${encodeURIComponent(item.slug)}` : `/?category=${item.id}#catalog`} onClick={(event) => { if (onPick && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) { event.preventDefault(); onPick(item.id); } document.querySelectorAll<HTMLDetailsElement>(".header details[open]").forEach((details) => details.removeAttribute("open")); }}>{item.label}<span>→</span></a>;
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
export const STORAGE_EVENT = FAVORITES_EVENT;

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
  const staff = user.adminRole === "manager" || user.adminRole === "administrator" || user.adminRole === "owner";
  const name = user.fullName.trim().split(/\s+/)[0] || "Профиль";
  if (!staff) return <a className={iconOnly ? "account-button icon-only" : "account-button"} href="/account" aria-label="Профиль">{iconOnly ? <Icon path={icons.person} /> : <><AccountBadge user={user} /><span>{name}</span></>}</a>;
  return <details className="account-menu" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary aria-label="Меню профиля" className={iconOnly ? "account-button icon-only" : "account-button"}>{iconOnly ? <Icon path={icons.person} /> : <><AccountBadge user={user} /><span>{name}</span></>}</summary>
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
  onSearch,
  onClose,
}: {
  query?: string;
  onQueryChange?: (value: string) => void;
  onSearch?: (value: string) => void;
  onClose: () => void;
}) {
  return <div className="mobile-search" onKeyDown={(event) => { if (event.key === "Escape") onClose(); }}>
    <CatalogSearch value={query} onChange={onQueryChange} onSearch={onSearch} className="mobile-catalog-search" autoFocus />
    <button type="button" onClick={onClose} aria-label="Закрыть поиск">×</button>
  </div>;
}

function MobileMenu({ view, user, favorites, items, state, onRetry, onView, onClose, onPick }: {
  view: "menu" | "catalog"; user: StoreUser | null; favorites: number;
  items: HeaderMenuItem[]; state: "loading" | "ready" | "error";
  onPick?: (id: number) => void;
  onRetry: () => void; onView: (view: "menu" | "catalog") => void; onClose: () => void;
}) {
  const ref = useModalFocus(onClose, view === "catalog");
  useEffect(() => {
    if (view !== "menu") return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const outside = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node) && !(event.target instanceof Element && event.target.closest(".menu-button"))) onClose();
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
      if (previous?.isConnected) previous.focus({ preventScroll: true });
    };
  }, [view, onClose, ref]);
  const [query, setQuery] = useState("");
  useEffect(() => { ref.current?.querySelector<HTMLButtonElement>("button")?.focus(); }, [view, ref]);
  let roots = items;
  while (roots.length === 1 && roots[0].children?.length) roots = roots[0].children;
  const links = (nodes: HeaderMenuItem[]): HeaderMenuItem[] => nodes.flatMap(node => [node, ...links(node.children || [])]);
  const visible = links(roots).filter(item => item.label.toLocaleLowerCase("ru").includes(query.trim().toLocaleLowerCase("ru")));
  return <aside ref={ref} className="mobile-menu open" role="dialog" aria-modal={view === "catalog" ? true : undefined} aria-label={view === "catalog" ? "Категории каталога" : "Меню магазина"}>
    <div className="mobile-menu-title"><h2>{view === "catalog" ? "Каталог" : "Меню"}</h2><button type="button" onClick={onClose} aria-label={view === "catalog" ? "Закрыть каталог" : "Закрыть меню"}><Icon path="m6 6 12 12M18 6 6 18" /></button></div>
    {view === "catalog" ? <>
      <input className="mobile-category-search" aria-label="Найти категорию" placeholder="Найти категорию" value={query} onChange={event => setQuery(event.target.value)} />
      <nav className="mobile-category-links" aria-label="Категории товаров"><a href="/#catalog" onClick={onClose}>Весь каталог <span aria-hidden="true">→</span></a>
        {visible.map(item => <a key={item.id} href={item.slug ? `/catalog/${encodeURIComponent(item.slug)}` : `/?category=${item.id}#catalog`} onClick={event => { if (onPick && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) { event.preventDefault(); onPick(item.id); } onClose(); }}>{item.label}<span aria-hidden="true">→</span></a>)}
        {items.length === 0 && state === "loading" && <p role="status">Загружаем категории…</p>}
        {items.length === 0 && state === "error" && <div role="alert"><p>Не удалось загрузить категории</p><button type="button" onClick={onRetry}>Повторить</button></div>}
        {items.length > 0 && visible.length === 0 && <p role="status">Категорий с таким названием нет</p>}
      </nav>
    </> : <>
      <div className="mobile-menu-account">{user ? <><a href="/account"><strong>{user.fullName.trim().split(/\s+/)[0] || "Профиль"}</strong><small>Личный кабинет</small></a>{user.adminRole && <a className="mobile-menu-admin" href="/admin">Панель управления <span aria-hidden="true">↗</span></a>}</> : <><a href="/login">Войти в аккаунт</a><a href="/register">Регистрация</a></>}</div>
      <nav className="mobile-menu-links" aria-label="Покупки"><button type="button" onClick={() => onView("catalog")}>Каталог <span aria-hidden="true">→</span></button><a href="/discounts">Растения со скидкой</a><a href="/favorites">Избранное <small>{favorites}</small></a></nav>
      <nav className="mobile-menu-links" aria-label="Помощь"><a href="/delivery-and-returns">Доставка и возврат</a><a href="/contacts">Контакты</a></nav>
      <details className="mobile-menu-documents"><summary>О магазине</summary><a href="/offer">Публичная оферта</a><a href="/privacy">Политика конфиденциальности</a><a href="/requisites">Реквизиты</a></details>
    </>}
  </aside>;
}

function AboutMenu() {
  return <details className="header-dropdown" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>О магазине <span>⌄</span></summary><div><a href="/contacts">Контакты</a><a href="/offer">Публичная оферта</a><a href="/privacy">Политика конфиденциальности</a><a href="/requisites">Реквизиты</a></div></details>;
}

// The bar pinned to the bottom of a phone screen. Everything a shopper
// reaches for repeatedly sits here, within thumb reach, on every page.
function MobileTabBar({
  user,
  favorites,
  cart,
  onCartClick,
  onSearchClick,
  onCatalogClick,
  catalogOpen,
}: {
  user: StoreUser | null;
  favorites: number;
  cart: number;
  onCartClick?: () => void;
  onSearchClick: (trigger: HTMLButtonElement) => void;
  onCatalogClick: () => void;
  catalogOpen: boolean;
}) {
  const cartInside = <>
    <span className="tab-icon">
      <Icon path={icons.bag} />
      {cart > 0 && <b>{cart}</b>}
    </span>
    <small>Корзина</small>
  </>;
  return <nav className="tab-bar" aria-label="Разделы магазина">
    <button type="button" className={window.location.pathname === "/" || window.location.pathname.startsWith("/catalog/") ? "active" : undefined} aria-haspopup="dialog" aria-expanded={catalogOpen} onClick={event => { event.currentTarget.focus(); onCatalogClick(); }}>
      <span className="tab-icon"><Icon path={icons.catalog} /></span>
      <small>Каталог</small>
    </button>
    <button type="button" onClick={event => onSearchClick(event.currentTarget)}>
      <span className="tab-icon"><Icon path={icons.search} /></span>
      <small>Поиск</small>
    </button>
    <a href="/favorites" className={window.location.pathname === "/favorites" ? "active" : undefined}>
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
  onSearch,
  onSearchClose,
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
  onSearch?: (value: string) => void;
  onSearchClose?: () => void;
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
  const [storedFavorites] = useFavorites();
  const [serverCart] = useSharedCart();
  const availableFavorites = useAvailableFavoriteCount(storedFavorites);
  const favorites = availableFavorites ?? favoritesCount ?? storedFavorites.size;
  const cart = cartCount ?? Object.values(serverCart).reduce((sum, value) => sum + value, 0);
  const [navigation, setNavigation] = useState<"menu" | "catalog" | null>(null);
  const menuOpen = navigation !== null;
  const closeMenu = useCallback(() => setNavigation(null), []);
  const [searchOpen, setSearchOpen] = useState(false);
  const searchTrigger = useRef<HTMLButtonElement | null>(null);
  const openSearch = (trigger: HTMLButtonElement) => { searchTrigger.current = trigger; setNavigation(null); setSearchOpen(true); };
  const closeSearch = () => { setSearchOpen(false); searchTrigger.current?.focus({ preventScroll: true }); onSearchClose?.(); };
  const [fallbackMenus, setFallbackMenus] = useState<{catalog:HeaderMenuItem[];plants:HeaderMenuItem[]}>({catalog:[],plants:[]});
  const [menuState, setMenuState] = useState<"loading" | "ready" | "error">("loading");
  const [menuReload, setMenuReload] = useState(0);
  const headerRef = useRef<HTMLElement>(null);
  useBodyLock(navigation === "catalog", "menu-open");
  useEffect(() => { document.body.classList.toggle("has-store-tabs", showTabBar); return () => document.body.classList.remove("has-store-tabs"); }, [showTabBar]);
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!headerRef.current?.contains(event.target as Node) || (event.target instanceof Element && event.target.closest(".catalog-search"))) headerRef.current?.querySelectorAll<HTMLDetailsElement>("details[open]").forEach((details) => details.removeAttribute("open"));
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      const open = headerRef.current?.querySelectorAll<HTMLDetailsElement>("details[open]");
      const last = open?.[open.length - 1];
      if (last) { last.open = false; last.querySelector("summary")?.focus(); }
    };
    document.addEventListener("keydown", escape);
    document.addEventListener("pointerdown", close);
    return () => { document.removeEventListener("pointerdown", close); document.removeEventListener("keydown", escape); };
  }, []);
  useEffect(() => {
    if ((!homeNavigation && !showTabBar) || catalogMenuItems.length || plantMenuItems.length) return;
    sharedAPIJSON<{categories?:Array<{id:number;parentId:number|null;name:string;slug:string;sortOrder:number}>}>("/api/v1/categories").then(body => {
      if (!Array.isArray(body.categories)) throw new Error("Invalid categories");
      const categories = body.categories;
      const children = new Map<number|null,typeof categories>();
      categories.forEach((item) => children.set(item.parentId,[...(children.get(item.parentId)||[]),item]));
      const order = orderStorefrontCategories<typeof categories[number]>;
      const branch = (item:typeof categories[number]):HeaderMenuItem => ({id:item.id,label:item.name,slug:item.slug,children:order(children.get(item.id)||[]).map(branch)});
      const catalog = order(children.get(null)||[]).map(branch);
      const plantRoot = categories.find((item)=>item.parentId==null&&/растен/i.test(item.name));
      const leaves = (parentId:number):typeof categories => order(children.get(parentId)||[]).flatMap((item)=>children.get(item.id)?.length?leaves(item.id):[item]);
      setFallbackMenus({catalog,plants:plantRoot?order(leaves(plantRoot.id)).map((item)=>({id:item.id,label:item.name,slug:item.slug})):[]});
      setMenuState("ready");
    }).catch(() => { setFallbackMenus({catalog:[],plants:[]}); setMenuState("error"); });
  },[homeNavigation,showTabBar,catalogMenuItems.length,plantMenuItems.length,menuReload]);

  useEffect(() => { headerRef.current?.querySelectorAll<HTMLDetailsElement>("details[open]").forEach((details) => { details.open = false; }); }, [query]);

  const resolvedCatalogMenuItems = catalogMenuItems.length ? catalogMenuItems : fallbackMenus.catalog;
  const categoryPick = onHomeCategoryPick;
  const closeHeaderMenus = () => headerRef.current?.querySelectorAll<HTMLDetailsElement>("details[open]").forEach((details) => { details.open = false; });
  const search = (value: string) => {
    closeHeaderMenus();
    setSearchOpen(false);
    if (onSearch) onSearch(value);
    else window.location.assign(`/?q=${encodeURIComponent(value)}#catalog`);
  };

  const cartLabel = `Корзина, товаров: ${cart}`;
  return <><div className="announcement" hidden />
    <header className="header store-header" ref={headerRef} onFocusCapture={(event) => { if (event.target.closest(".catalog-search")) closeHeaderMenus(); }}>
      <button
        className="menu-button"
        onClick={(event) => { event.currentTarget.focus(); setSearchOpen(false); setNavigation(value => value === "menu" ? null : "menu"); }}
        aria-label="Открыть меню"
        aria-expanded={menuOpen}
      >☰</button>
      <a className="brand" href="/"><span className="brand-mark">⌇</span><span className="brand-text"><span>Фикусин</span><small>магазин растений</small></span></a>
      <nav className="desktop-nav">{homeNavigation ? <>
        <details className="header-dropdown" onToggle={(event) => closeOtherHeaderMenus(event.currentTarget)}><summary>Каталог <span>⌄</span></summary><div><a href="/#catalog">Весь каталог<span>→</span></a>{resolvedCatalogMenuItems.map((item) => <HeaderMenuBranch item={item} onPick={categoryPick} key={item.id} />)}<a href="/discounts">Растения со скидкой<span>→</span></a></div></details>

        <a href="/delivery-and-returns">Доставка и оплата</a><AboutMenu/>
      </> : <><a href="/#catalog">Каталог</a><a href="/favorites">Избранное</a><a href="/delivery-and-returns">Доставка и возврат</a><AboutMenu/></>}</nav>
      <div className="header-actions">
        {showSearch && <>
          <CatalogSearch value={query} onChange={onQueryChange} onSearch={search} />
          <button
            className="search-toggle"
            onClick={event => searchOpen ? closeSearch() : openSearch(event.currentTarget)}
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
    {showSearch && searchOpen && <MobileSearch query={query} onQueryChange={onQueryChange} onSearch={search} onClose={closeSearch} />}
    <button className={`overlay mobile-menu-backdrop${navigation === "catalog" ? " open" : ""}`} aria-label="Закрыть меню" aria-hidden={navigation !== "catalog"} inert={navigation !== "catalog"} onClick={closeMenu} />
    {navigation && <MobileMenu view={navigation} user={user} favorites={favorites} items={resolvedCatalogMenuItems} state={resolvedCatalogMenuItems.length ? "ready" : menuState}
      onRetry={() => { setMenuState("loading"); setMenuReload(value => value + 1); }} onView={setNavigation} onClose={closeMenu} onPick={categoryPick} />}
    {showTabBar && <>
      {!/^\/(cart|checkout)(\/|$)/.test(window.location.pathname) && <a className="floating-cart" href="/cart" aria-label={`Открыть корзину, товаров: ${cart}`}><Icon path="M3 4h2l2.2 11.4h10.6L20 7H6M9 20h.01M17 20h.01" />{cart > 0 && <b>{cart}</b>}</a>}
      <InstallHint />
      <MobileTabBar user={user} favorites={favorites} cart={cart} onCartClick={onCartClick} onSearchClick={openSearch} onCatalogClick={() => { setSearchOpen(false); setNavigation("catalog"); }} catalogOpen={navigation === "catalog"} />
    </>}
  </>;
}
