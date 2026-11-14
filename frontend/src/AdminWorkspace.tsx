import "./styles/account-access.css";
import { BrandLogo } from "./BrandLogo";
import { useChatUnread } from "./lib/chat";
import { mobilePrimarySections, sections } from "./adminNavigation";
import { useRef, useState } from "react";
import type { AdminData, Section } from "./adminTypes";
import { roleLabel } from "./adminShared";

function WorkspaceAvatar({ user }: { user: AdminData["user"] }) {
  const [failed, setFailed] = useState("");
  const src = user.avatarUpdatedAt
    ? `/api/v1/account/avatar?v=${encodeURIComponent(user.avatarUpdatedAt)}`
    : "";
  return src && failed !== src ? (
    <img className="ws-avatar-image" src={src} alt="" onError={() => setFailed(src)} />
  ) : (
    <>{user.fullName.trim().charAt(0) || "Ф"}</>
  );
}

type IconName = Section | "search" | "arrow" | "more";
export function AdminIcon({ name }: { name: IconName }) {
  const paths: Record<IconName, string> = {
    chat: "M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6",
    returns: "M4 8h10a6 6 0 0 1 0 12H9M4 8l5-5M4 8l5 5",
    marketplaces: "M8 16 16 8M9 5l2-2a5 5 0 0 1 7 7l-2 2M8 12l-2 2a5 5 0 0 0 7 7l2-2",
    flowwow: "M4 7h16v14H4ZM8 7V5a4 4 0 0 1 8 0v2M9 13l2 2 4-4",
    avito: "M5 12a7 7 0 1 0 14 0 7 7 0 1 0-14 0m3 0h8M12 8v8",
    dashboard: "M3 10 12 3l9 7v11h-6v-7H9v7H3Z",
    analytics: "M4 21V11m8 10V3m8 18V7",
    products: "M20 4C9 2 2 8 5 16s17 3 15-12ZM5 20 16 9",
    categories: "M3 3h7v7H3Zm11 0h7v7h-7ZM3 14h7v7H3Zm11 0h7v7h-7Z",
    collections: "M5 3h14v18l-7-4-7 4Z",
    orders: "M6 3h12v18l-3-2-3 2-3-2-3 2ZM9 8h6m-6 4h6",
    procurement: "m3 7 9-4 9 4-9 5Zm0 0v10l9 5 9-5V7M12 12v10",
    customers:
      "M16 21v-3a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v3m17-7a4 4 0 0 1 3 4v3M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8m8 0a4 4 0 0 1 0 8",
    settings: "M4 7h16M4 17h16M8 4v6m8 4v6",
    search: "M21 21l-5-5M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14",
    arrow: "M5 12h14m-5-5 5 5-5 5",
    more: "M4 6h16M4 12h16M4 18h16",
  };
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={paths[name]} />
    </svg>
  );
}

function visibleSections(data: AdminData) {
  return sections.filter(
    (item) =>
      !["categories", "collections"].includes(item.id) &&
      (!item.permission || data.permissions.includes(item.permission)) &&
      (!item.owner || data.role === "owner"),
  );
}
const isActive = (item: Section, section: Section) =>
  section === item || (item === "products" && ["categories", "collections"].includes(section));

function SectionLink({
  id,
  label,
  section,
  unread,
  onNavigate,
}: {
  id: Section;
  label: string;
  section: Section;
  unread: number;
  onNavigate: (section: Section) => void;
}) {
  const active = isActive(id, section);
  return (
    <a
      href={`/admin?section=${id}`}
      className="ws-nav-link"
      aria-current={active ? "page" : undefined}
      onClick={(event) => {
        event.preventDefault();
        onNavigate(id);
      }}
    >
      <AdminIcon name={id} />
      <span>{label}</span>
      {id === "chat" && unread > 0 && (
        <b className="ws-count" aria-label={`Непрочитанных: ${unread}`}>
          {unread > 99 ? "99+" : unread}
        </b>
      )}
    </a>
  );
}

/* Боковая панель на компьютере и нижняя панель на телефоне — одна и та же
   навигация. На телефоне сотрудник держит устройство одной рукой, поэтому
   главные разделы внизу под большим пальцем, остальные — в «Ещё». */
export function WorkspaceSidebar({
  data,
  section,
  onNavigate,
}: {
  data: AdminData;
  section: Section;
  onNavigate: (section: Section) => void;
}) {
  const unread = useChatUnread(true, data.permissions.includes("chat.manage"));
  const more = useRef<HTMLDialogElement>(null);
  const available = visibleSections(data);
  const go = (next: Section) => {
    more.current?.close();
    onNavigate(next);
  };
  // Одним списком, без подписей групп: разделов немного, а заголовки
  // «Сегодня», «Растения» только добавляли слов. Порядок задан в sections.
  const groupedLinks = (
    <div className="ws-nav-group">
      {available.map((item) => (
        <SectionLink
          key={item.id}
          id={item.id}
          label={item.label}
          section={section}
          unread={unread}
          onNavigate={go}
        />
      ))}
    </div>
  );
  const primary = mobilePrimarySections
    .map((id) => available.find((item) => item.id === id))
    .filter((item) => !!item);
  const moreActive = !primary.some((item) => isActive(item.id, section));
  return (
    <>
      <aside className="ws-sidebar">
        <a className="workspace-skip" href="#workspace-content">
          К содержимому
        </a>
        <div className="ws-brand">
          <WorkspaceMark />
        </div>
        <nav aria-label="Разделы управления">{groupedLinks}</nav>
        <div className="ws-sidebar-bottom">
          <a className="ws-profile" href="/account">
            <span className="ws-avatar" aria-hidden="true">
              <WorkspaceAvatar user={data.user} />
            </span>
            <span>
              <strong>{data.user.fullName}</strong>
              <small>{roleLabel(data.role)}</small>
            </span>
          </a>
        </div>
      </aside>
      <nav className="ws-tabbar" aria-label="Главные разделы">
        {primary.map((item) => (
          <SectionLink
            key={item.id}
            id={item.id}
            label={item.label}
            section={section}
            unread={unread}
            onNavigate={go}
          />
        ))}
        <button
          type="button"
          className="ws-nav-link"
          aria-current={moreActive ? "page" : undefined}
          onClick={() => more.current?.showModal()}
        >
          <AdminIcon name="more" />
          <span>Ещё</span>
        </button>
      </nav>
      <dialog
        ref={more}
        className="ws-more"
        aria-label="Все разделы"
        onClick={(event) => {
          if (event.target === event.currentTarget) event.currentTarget.close();
        }}
      >
        <div className="ws-more-sheet">
          <header>
            <strong>Все разделы</strong>
            <button type="button" aria-label="Закрыть" onClick={() => more.current?.close()}>
              ×
            </button>
          </header>
          <nav aria-label="Все разделы управления">{groupedLinks}</nav>
        </div>
      </dialog>
    </>
  );
}

export function WorkspaceToolbar({
  data,
  section,
  onNavigate,
}: {
  data: AdminData;
  section: Section;
  onNavigate: (section: Section) => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const available = sections.filter(
    (item) =>
      (!item.permission || data.permissions.includes(item.permission)) &&
      (!item.owner || data.role === "owner"),
  );
  const results = available.filter((item) =>
    item.label.toLocaleLowerCase("ru").includes(query.trim().toLocaleLowerCase("ru")),
  );
  const choose = (next: Section) => {
    onNavigate(next);
    setOpen(false);
    setQuery("");
    input.current?.focus();
  };
  return (
    <header className="ws-topbar">
      <div className="ws-topbar-brand">
        <WorkspaceMark />
      </div>
      <strong className="ws-topbar-title">
        {
          sections.find(
            (item) =>
              isActive(item.id, section) && item.id !== "categories" && item.id !== "collections",
          )?.label
        }
      </strong>
      <div
        className="ws-search"
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
        }}
      >
        <AdminIcon name="search" />
        <input
          ref={input}
          type="search"
          aria-label="Найти раздел"
          placeholder="Перейти в раздел…"
          value={query}
          onFocus={() => setOpen(true)}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") setOpen(false);
            if (event.key === "Enter" && results.length === 1) {
              event.preventDefault();
              choose(results[0].id);
            }
          }}
        />
        {open && (
          <div className="ws-search-results" aria-label="Результаты поиска разделов">
            {results.length ? (
              results.map((item) => (
                <button type="button" key={item.id} onClick={() => choose(item.id)}>
                  <AdminIcon name={item.id} />
                  {item.label}
                  <AdminIcon name="arrow" />
                </button>
              ))
            ) : (
              <p>Раздел не найден</p>
            )}
          </div>
        )}
      </div>
    </header>
  );
}

/* Знак админки: настоящий логотип Фикусина (те же контуры, что на сайте)
   и подпись «Управление», чтобы было видно, что это рабочий кабинет. */
export function WorkspaceMark() {
  return (
    <span className="ws-mark">
      <a href="/" aria-label="Фикусин — магазин">
        <BrandLogo />
      </a>
      <small>
        <a href="/admin">Управление</a>
      </small>
    </span>
  );
}
