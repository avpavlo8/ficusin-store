import { useCallback, useEffect, useRef, useState } from "react";
import { isStandalone } from "./lib/pwa";
import { claimInstallOffer } from "./lib/install-offer";
import { InstallHint } from "./InstallHint";
import "./styles/app-discovery.css";
export function InstallFirstVisit() {
  const [open, setOpen] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    const show = () => {
      if (
        document.visibilityState !== "visible" ||
        document.querySelector('dialog[open], [role="dialog"][aria-modal="true"]') ||
        document.body.classList.contains("menu-open") ||
        document.body.classList.contains("quick-chat-open")
      )
        return;
      let storage: Storage | null = null;
      try {
        storage = localStorage;
      } catch {
        /* Privacy settings can disable persistence. */
      }
      if (claimInstallOffer(storage, isStandalone())) setOpen(true);
    };
    const timer = setTimeout(show, 700);
    document.addEventListener("visibilitychange", show);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", show);
    };
  }, []);
  useEffect(() => {
    const node = dialog.current;
    if (!open || !node) return;
    node.showModal();
    return () => {
      if (node.open) node.close();
    };
  }, [open]);
  if (!open) return null;
  return (
    <dialog
      ref={dialog}
      className="install-first-visit"
      aria-label="Фикусин на экране Домой"
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) close();
      }}
    >
      <button
        type="button"
        className="install-offer-close"
        aria-label="Закрыть предложение установки"
        onClick={close}
      >
        <svg
          width="22"
          height="22"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d="m6 6 12 12M18 6 6 18" />
        </svg>
      </button>
      <InstallHint inline onInstalled={close} />
    </dialog>
  );
}
