import { useEffect, useId, useRef } from "react";

export function EditorMultiSelect({
  label,
  options,
  value,
  onChange,
  placeholder = "Не указано",
  disabled = false,
  compact = false,
}: {
  label: string;
  options: Array<{ value: string; label: string }>;
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
  disabled?: boolean;
  compact?: boolean;
}) {
  const picker = useRef<HTMLDetailsElement>(null);
  const listId = useId();
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (picker.current && !picker.current.contains(event.target as Node))
        picker.current.open = false;
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, []);
  const selected = options.filter((option) => value.includes(option.value));
  return (
    <details
      className="editor-multi-select"
      data-compact={compact || undefined}
      ref={picker}
      onKeyDown={(event) => {
        if (event.key === "Escape" && picker.current?.open) {
          picker.current.open = false;
          picker.current.querySelector("summary")?.focus();
        }
      }}
    >
      <summary
        aria-label={label}
        aria-controls={listId}
        aria-disabled={disabled || undefined}
        title={
          compact && selected.length ? selected.map((option) => option.label).join(", ") : undefined
        }
        onClick={(event) => {
          if (disabled) event.preventDefault();
        }}
      >
        <span className={compact ? "editor-multi-value" : undefined}>
          {compact &&
            options.map((option) => (
              <span key={option.value} className="editor-multi-sizer" aria-hidden="true">
                {option.label}
                <span className="editor-multi-count">+1</span>
              </span>
            ))}
          <span className={compact ? "editor-multi-selection" : undefined}>
            <span>
              {selected.length
                ? compact
                  ? selected[0].label
                  : selected.map((option) => option.label).join(", ")
                : placeholder}
            </span>
            {compact && selected.length > 1 && (
              <span className="editor-multi-count">+{selected.length - 1}</span>
            )}
          </span>
        </span>
        <svg
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
      <div
        className="editor-multi-options"
        role="listbox"
        id={listId}
        aria-label={label}
        aria-multiselectable="true"
      >
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            role="option"
            aria-selected={value.includes(option.value)}
            disabled={disabled}
            onClick={() =>
              onChange(
                value.includes(option.value)
                  ? value.filter((item) => item !== option.value)
                  : [...value, option.value],
              )
            }
          >
            <span>{option.label}</span>
            <svg
              viewBox="0 0 24 24"
              width="18"
              height="18"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="m5 12 4 4 10-10" />
            </svg>
          </button>
        ))}
      </div>
    </details>
  );
}
