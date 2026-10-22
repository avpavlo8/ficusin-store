import { useEffect, useRef } from "react";

export default function ChatPhotoViewer({ src, onClose }: { src: string; onClose: () => void }) {
  const modal = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = modal.current;
    if (!dialog) return;
    dialog.showModal();
    dialog.querySelector('button')?.focus({ preventScroll: true });
    return () => dialog.close();
  }, []);
  const close = () => { modal.current?.close(); onClose(); };
  return <dialog ref={modal} className="chat-photo-viewer" aria-label="Просмотр фотографии" onKeyDown={event => {
    if (event.key !== 'Tab') return;
    const controls = [...event.currentTarget.querySelectorAll<HTMLElement>('a[href],button')];
    const current = controls.indexOf(document.activeElement as HTMLElement);
    event.preventDefault();
    controls[(current + (event.shiftKey ? controls.length - 1 : 1)) % controls.length]?.focus();
  }} onCancel={event => { event.preventDefault(); close(); }} onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <header><span>Фотография</span><a href={src} download="photo.jpg">Скачать</a><button type="button" aria-label="Закрыть фотографию" onClick={close} autoFocus><svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button></header>
    <img src={src} alt="Фотография из переписки" />
  </dialog>;
}
