import { useState, type ReactNode } from "react";

export function CareFieldDisclosure({
  title,
  filled,
  children,
}: {
  title: string;
  filled: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="editor-care-topic"
      data-filled={filled}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>
        <span>{title}</span>
        {!filled && <span className="editor-care-empty">Не заполнено</span>}
        {filled && (
          <svg
            className="editor-care-complete"
            viewBox="0 0 24 24"
            width="18"
            height="18"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            role="img"
            aria-label="Заполнено"
          >
            <path d="m5 12 4 4 10-10" />
          </svg>
        )}
        <svg
          className="editor-care-chevron"
          viewBox="0 0 24 24"
          width="16"
          height="16"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </summary>
      {open && children}
    </details>
  );
}
