import { useEffect, useRef, useState } from "react";
import ChatThread, { ChatIcon } from "./ChatThread";
import type { ChatContext } from "./lib/chat";

export default function QuickChat({ open, context, onClose }: { open: boolean; context?: ChatContext; onClose: () => void }) {
  const panel = useRef<HTMLElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const [mobile, setMobile] = useState(() => window.matchMedia("(max-width: 760px)").matches);
  const closeCallback = useRef(onClose);
  useEffect(() => { closeCallback.current = onClose; }, [onClose]);
  useEffect(() => { const query = window.matchMedia("(max-width: 760px)"); const update = () => setMobile(query.matches); query.addEventListener("change", update); return () => query.removeEventListener("change", update); }, []);
  useEffect(() => {
    const dialog = panel.current;
    if (!open || !dialog) return;
    const background: Array<[HTMLElement, boolean]> = [];
    if (mobile) {
      let branch: HTMLElement = dialog;
      while (branch.parentElement) {
        const parent = branch.parentElement;
        for (const sibling of parent.children) if (sibling instanceof HTMLElement && sibling !== branch) { background.push([sibling, sibling.inert]); sibling.inert = true; }
        if (parent === document.body) break;
        branch = parent;
      }
    }
    dialog.focus({ preventScroll: true });
    const controls = () => [...dialog.querySelectorAll<HTMLElement>('a[href],button,input,textarea,select,summary,[tabindex]')].filter(node => node.tabIndex >= 0 && !node.hasAttribute("disabled") && node.getClientRects().length > 0 && !node.closest("[hidden]"));
    const keydown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      if (event.key === "Escape" && (mobile || dialog.contains(document.activeElement))) { event.preventDefault(); closeCallback.current(); return; }
      if (!mobile || event.key !== "Tab") return;
      const items = controls();
      event.preventDefault();
      if (!items.length) return;
      const current = items.indexOf(document.activeElement as HTMLElement);
      const next = event.shiftKey ? (current <= 0 ? items.length - 1 : current - 1) : (current < 0 || current === items.length - 1 ? 0 : current + 1);
      items[next].focus();
    };
    document.addEventListener("keydown", keydown);
    return () => { document.removeEventListener("keydown", keydown); background.forEach(([element, inert]) => { element.inert = inert; }); };
  }, [open, mobile]);
  return <section ref={panel} tabIndex={-1} id="quick-chat" className="quick-chat telegram-chat" hidden={!open} role="dialog" aria-modal={mobile || undefined} aria-label="Быстрый чат с Фикусином"><header className="chat-panel-heading"><img className="chat-avatar" src="/icon-192.png" alt="" /><div><h2>Фикусин</h2><p>Сотрудник магазина</p></div><button ref={close} className="chat-header-action" type="button" aria-label="Свернуть чат" onClick={onClose}><ChatIcon name="close" /></button></header><ChatThread context={context} visible={open} /></section>;
}
