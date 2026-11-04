import { useId, type ReactNode } from "react";

export function WorkspaceState({
  kind,
  title,
  detail,
  action,
}: {
  kind: "loading" | "empty" | "error" | "restricted" | "stale" | "partial" | "unknown";
  title: string;
  detail?: string;
  action?: ReactNode;
}) {
  return (
    <section
      className="workspace-state"
      data-kind={kind}
      role={kind === "error" ? "alert" : "status"}
      aria-live="polite"
      aria-busy={kind === "loading"}
    >
      <strong>{title}</strong>
      {detail && <p>{detail}</p>}
      {action}
    </section>
  );
}
export function WorkspaceTable({ label, children }: { label: string; children: ReactNode }) {
  const hint = useId();
  return (
    <>
      <p className="workspace-scroll-hint" id={hint}>
        Таблицу можно прокручивать по горизонтали.
      </p>
      <div
        className="admin-table-wrap"
        role="region"
        aria-label={label}
        aria-describedby={hint}
        tabIndex={0}
      >
        <table className="admin-table">{children}</table>
      </div>
    </>
  );
}
export function WorkspaceStatus({ children, status }: { children: ReactNode; status: string }) {
  return (
    <span className="workspace-status" data-status={status}>
      {children}
    </span>
  );
}
export function WorkspaceTasks({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="workspace-panel">
      <h3>{title}</h3>
      <div className="workspace-tasks">{children}</div>
    </section>
  );
}
export function WorkspaceForm({
  children,
  readOnly = false,
}: {
  children: ReactNode;
  readOnly?: boolean;
}) {
  return (
    <fieldset className="admin-form-grid workspace-form" disabled={readOnly}>
      {children}
    </fieldset>
  );
}

/* «Ещё»: редкие действия и разделы прячутся сюда, чтобы на экране
   оставались одна главная кнопка и самые частые вкладки. Сделано на
   <details>, поэтому работает с клавиатуры и без JavaScript-состояния. */
export function WorkspaceMore({
  label = "Ещё",
  active = false,
  children,
}: {
  label?: string;
  active?: boolean;
  children: ReactNode;
}) {
  return (
    <details
      className="ws-more-menu"
      data-active={active || undefined}
      onClick={(event) => {
        const target = event.target as HTMLElement;
        if (target.closest(".ws-more-menu-list button, .ws-more-menu-list a"))
          event.currentTarget.open = false;
      }}
    >
      <summary>{label}</summary>
      <div className="ws-more-menu-list">{children}</div>
    </details>
  );
}
