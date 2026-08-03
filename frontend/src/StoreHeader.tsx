import { useEffect, useState } from "react";

export type StoreUser = { fullName: string; adminRole?: "manager" | "owner" };

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
  if (!staff) return <a className="account-button" href="/account"><span>◯</span><span>{name}</span></a>;
  return <details className="account-menu"><summary className="account-button"><span>◯</span><span>{name}</span></summary>
    <div><a href="/account">Личный профиль</a><a href="/admin">Админка</a></div>
  </details>;
}

export function StoreHeader({ cartCount = 0, favoritesCount = 0, query, onQueryChange }: { cartCount?: number; favoritesCount?: number; query?: string; onQueryChange?: (value: string) => void }) {
  const user = useStoreUser();
  return <><div className="announcement"><span>Бережно упакуем каждое растение</span><span>Доставка по Рязани и всей России</span></div>
    <header className="header"><a className="brand" href="/"><span className="brand-mark">⌇</span><span>Фикусин</span></a>
      <nav className="desktop-nav"><a href="/#catalog">Каталог</a><a href="/#care">Уход</a><a href="/#delivery">Доставка</a></nav>
      <div className="header-actions">
        {onQueryChange && <label className="header-search"><span>⌕</span><input value={query || ""} onChange={(event) => onQueryChange(event.target.value)} placeholder="Поиск по каталогу" /></label>}
        <AccountMenu user={user} />
        <a className="favorites-button" href="/favorites" aria-label={`Избранное, товаров: ${favoritesCount}`}><span>♥</span><b>{favoritesCount}</b></a>
        <a className="cart-button" href="/?cart=1"><span>Корзина</span><b>{cartCount}</b></a>
      </div>
    </header></>;
}

