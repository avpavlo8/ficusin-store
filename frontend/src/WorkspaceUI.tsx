import { useId, type ReactNode } from "react";

export function WorkspaceState({ kind, title, detail, action }: { kind: "loading" | "empty" | "error" | "restricted" | "stale" | "partial" | "unknown"; title: string; detail?: string; action?: ReactNode }) {
  return <section className="workspace-state" data-kind={kind} role={kind === "error" ? "alert" : "status"} aria-live="polite" aria-busy={kind === "loading"}>
    <strong>{title}</strong>{detail && <p>{detail}</p>}{action}
  </section>;
}
export function WorkspaceTable({ label, children }: { label: string; children: ReactNode }) {
  const hint = useId();
  return <><p className="workspace-scroll-hint" id={hint}>Таблицу можно прокручивать по горизонтали.</p><div className="admin-table-wrap" role="region" aria-label={label} aria-describedby={hint} tabIndex={0}><table className="admin-table">{children}</table></div></>;
}
export function WorkspaceStatus({ children, status }: { children: ReactNode; status: string }) {
  return <span className="workspace-status" data-status={status}>{children}</span>;
}
export function WorkspaceTasks({ title, children }: { title: string; children: ReactNode }) {
  return <section className="workspace-panel"><h3>{title}</h3><div className="workspace-tasks">{children}</div></section>;
}
export function WorkspaceForm({ children, readOnly = false }: { children: ReactNode; readOnly?: boolean }) {
  return <fieldset className="admin-form-grid workspace-form" disabled={readOnly}>{children}</fieldset>;
}
