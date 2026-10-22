import { useEffect, useId, useRef, useState } from "react";

export type OrganizationDetails = { name: string; inn: string; kpp: string; legalAddress: string };

export function OrganizationLookup({ initialName, onClear, onSelect }: { initialName: string; onClear: () => void; onSelect: (organization: OrganizationDetails) => void }) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState(initialName);
  const [focused, setFocused] = useState(false);
  const [suggestions, setSuggestions] = useState<OrganizationDetails[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [status, setStatus] = useState("");

  useEffect(() => {
    if (!focused) return;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      if (query.trim().length < 3) return;
      setStatus("Ищем организации…");
      try {
        const response = await fetch(`/api/v1/organizations/suggest?q=${encodeURIComponent(query.trim())}`, { credentials: "same-origin", signal: controller.signal });
        const data = await response.json() as { available?: boolean; suggestions?: OrganizationDetails[]; error?: string; demo?: boolean };
        if (controller.signal.aborted) return;
        if (!response.ok) throw new Error(data.error || "Справочник временно недоступен. Попробуйте позже.");
        const items = data.suggestions || [];
        setSuggestions(items);
        setOpen(items.length > 0);
        setStatus(data.available === false ? "Справочник пока не подключён. Попробуйте позже." : items.length ? (data.demo ? "Демонстрационные подсказки для согласования." : "Выберите организацию из списка.") : "Организация не найдена. Проверьте название или ИНН.");
      } catch (caught) {
        if (!controller.signal.aborted) { setSuggestions([]); setOpen(false); setStatus((caught as Error).message); }
      }
    }, 300);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [query, focused]);

  useEffect(() => {
    const outside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false); };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, []);

  useEffect(() => {
    if (open && active >= 0) root.current?.querySelectorAll<HTMLElement>('[role="option"]')[active]?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  function select(item: OrganizationDetails) {
    setQuery(item.name);
    setFocused(false);
    setOpen(false);
    setActive(-1);
    setSuggestions([]);
    setStatus("");
    onSelect(item);
  }

  return <div className="organization-lookup" ref={root} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) { setOpen(false); setFocused(false); } }}>
    <label htmlFor={`${id}-query`}>Название организации или ИНН</label>
    <input id={`${id}-query`} type="text" role="combobox" autoComplete="off" spellCheck={false} maxLength={150} placeholder="Начните вводить название или ИНН" value={query}
      aria-autocomplete="list" aria-expanded={open} aria-controls={`${id}-options`} aria-describedby={`${id}-hint`}
      aria-activedescendant={open && active >= 0 ? `${id}-option-${active}` : undefined}
      onFocus={() => { setFocused(true); if (suggestions.length) setOpen(true); }}
      onChange={event => { setQuery(event.target.value); onClear(); setFocused(true); setSuggestions([]); setOpen(false); setActive(-1); setStatus(""); }}
      onKeyDown={event => {
        if (event.key === "Escape") { event.preventDefault(); setOpen(false); setActive(-1); }
        if ((event.key === "ArrowDown" || event.key === "ArrowUp") && suggestions.length) {
          event.preventDefault(); setOpen(true);
          setActive(index => event.key === "ArrowDown" ? (index + 1) % suggestions.length : (index < 0 ? suggestions.length - 1 : (index - 1 + suggestions.length) % suggestions.length));
        }
        if (event.key === "Enter") { event.preventDefault(); if (open && active >= 0 && suggestions[active]) select(suggestions[active]); }
      }} />
    <div className="organization-options" id={`${id}-options`} role="listbox" aria-label="Организации" hidden={!open}>
      {suggestions.map((item, index) => <button key={`${item.inn}-${item.kpp}`} id={`${id}-option-${index}`} type="button" role="option" aria-selected={active === index}
        onMouseDown={event => event.preventDefault()} onClick={() => select(item)}>
        <strong>{item.name}</strong><span>ИНН {item.inn}{item.kpp ? ` · КПП ${item.kpp}` : ""}</span>
        {item.legalAddress && <small>{item.legalAddress}</small>}
      </button>)}
    </div>
    <p className="organization-status" id={`${id}-hint`} role="status" hidden={!status}>{status}</p>
  </div>;
}
