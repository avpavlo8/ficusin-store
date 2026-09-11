import { sections } from "./adminNavigation";
import { useRef, useState } from "react";
import type { AdminData, Section } from "./adminTypes";
import { roleLabel } from "./adminShared";



export function AdminIcon({ name }: { name: Section | "search" | "arrow" }) {
  const paths: Record<Section | "search" | "arrow", string> = {
    returns: "M4 8h10a6 6 0 0 1 0 12H9M4 8l5-5M4 8l5 5",
    finance: "M3 6h18v14H3ZM3 10h18M15 15h3",
    marketplaces: "M8 16 16 8M9 5l2-2a5 5 0 0 1 7 7l-2 2M8 12l-2 2a5 5 0 0 0 7 7l2-2",
    dashboard: "M3 10 12 3l9 7v11h-6v-7H9v7H3Z",
    analytics: "M4 21V11m8 10V3m8 18V7",
    products: "M20 4C9 2 2 8 5 16s17 3 15-12ZM5 20 16 9",
    categories: "M3 3h7v7H3Zm11 0h7v7h-7ZM3 14h7v7H3Zm11 0h7v7h-7Z",
    collections: "M5 3h14v18l-7-4-7 4Z",
    orders: "M6 3h12v18l-3-2-3 2-3-2-3 2ZM9 8h6m-6 4h6",
    procurement: "m3 7 9-4 9 4-9 5Zm0 0v10l9 5 9-5V7M12 12v10",
    customers: "M16 21v-3a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v3m17-7a4 4 0 0 1 3 4v3M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8m8 0a4 4 0 0 1 0 8",
    settings: "M4 7h16M4 17h16M8 4v6m8 4v6",
    search: "M21 21l-5-5M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14",
    arrow: "M5 12h14m-5-5 5 5-5 5",
  };
  return <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d={paths[name]} /></svg>;
}

export function WorkspaceSidebar({ data, section, onNavigate }: { data: AdminData; section: Section; onNavigate: (section: Section) => void }) {
  return <aside className="account-sidebar">
    <a className="workspace-skip" href="#workspace-content">К содержимому</a>
    <a className="workspace-brand" href="/" aria-label="Фикусин — в магазин">Фикусин<span aria-hidden="true">✳</span></a>
    <p className="workspace-caption">Рабочее пространство</p>
    <nav aria-label="Разделы управления">{sections.filter(item => !["categories", "collections"].includes(item.id)).filter(item => (!item.permission || data.permissions.includes(item.permission)) && (!item.owner || data.role === "owner")).map(item =>
      <button key={item.id} type="button" className={(section === item.id || item.id === "products" && ["categories", "collections"].includes(section)) ? "active" : ""} aria-current={(section === item.id || item.id === "products" && ["categories", "collections"].includes(section)) ? "page" : undefined} onClick={() => onNavigate(item.id)}>
        <AdminIcon name={item.id} />{item.label}
      </button>
    )}</nav>
    <div className="workspace-sidebar-bottom">
      <a className="workspace-store-link" href="/">Вернуться в магазин <AdminIcon name="arrow" /></a>
      <a className="workspace-profile" href="/account"><span className="workspace-avatar" aria-hidden="true">{data.user.fullName.trim().charAt(0) || "Ф"}</span><div><strong>{data.user.fullName}</strong><small>{roleLabel(data.role)} · Личный кабинет</small></div></a>
    </div>
  </aside>;
}

export function WorkspaceToolbar({ data, section, onNavigate }: { data: AdminData; section: Section; onNavigate: (section: Section) => void }) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const available = sections.filter(item => (!item.permission || data.permissions.includes(item.permission)) && (!item.owner || data.role === "owner"));
  const results = available.filter(item => item.label.toLocaleLowerCase("ru").includes(query.trim().toLocaleLowerCase("ru")));
  const choose = (next: Section) => { onNavigate(next); setOpen(false); setQuery(""); input.current?.focus(); };
  return <header className="workspace-toolbar">
    <div className="workspace-breadcrumb">Магазин <span>/</span> <strong>{sections.find(item => item.id === section)?.label}</strong></div>
    <div className="workspace-search" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false); }}>
      <AdminIcon name="search" />
      <input ref={input} type="search" aria-label="Найти раздел" placeholder="Найти раздел…" value={query} onFocus={() => setOpen(true)} onChange={event => { setQuery(event.target.value); setOpen(true); }} onKeyDown={event => {
        if (event.key === "Escape") setOpen(false);
        if (event.key === "Enter" && results.length === 1) { event.preventDefault(); choose(results[0].id); }
      }} />
      {open && <div className="workspace-search-results" aria-label="Результаты поиска разделов">
        {results.length ? results.map(item => <button type="button" key={item.id} onClick={() => choose(item.id)}><AdminIcon name={item.id} />{item.label}<AdminIcon name="arrow" /></button>) : <p>Раздел не найден</p>}
      </div>}
    </div>
    <a className="workspace-toolbar-profile" href="/account" aria-label="Личный кабинет">{data.user.fullName.trim().charAt(0) || "Ф"}</a>
    <a className="workspace-toolbar-store" href="/" title="Открыть магазин">Магазин ↗</a>
  </header>;
}
