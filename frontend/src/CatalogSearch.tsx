import { FormEvent, KeyboardEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { searchProducts, suggestions, type Searchable } from "./lib/search";

type SearchProduct = Searchable & { image?: string; price?: number };

export function CatalogSearch({ value, onChange, inlineResults = false, className = "header-search", placeholder = "Поиск по каталогу", autoFocus = false }: {
  value?: string;
  onChange?: (value: string) => void;
  /** Keep «Все результаты» on this page. Only the catalogue owns a results section. */
  inlineResults?: boolean;
  className?: string;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const [ownValue, setOwnValue] = useState("");
  const [products, setProducts] = useState<SearchProduct[]>([]);
  const [loaded, setLoaded] = useState(false);
  const pendingEnter = useRef(false);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const activeRef = useRef(-1);
  const root = useRef<HTMLDivElement>(null);
  const listID = useId();
  const query = value ?? ownValue;

  useEffect(() => {
    fetch("/api/v1/catalog")
      .then((response) => response.ok ? response.json() : { products: [] })
      .then((data: { products?: SearchProduct[] }) => setProducts(data.products || []))
      .catch(() => setProducts([]))
      .finally(() => setLoaded(true));
  }, []);
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);

  const matches = useMemo(() => suggestions(products, query).slice(0, 6), [products, query]);
  const resultCount = useMemo(() => searchProducts(products, query).length, [products, query]);
  const optionCount = matches.length + (query.trim() ? 1 : 0);
  const setQuery = (next: string) => onChange ? onChange(next) : setOwnValue(next);
  const allResults = () => {
    const trimmed = query.trim();
    if (!trimmed) return;
    if (inlineResults) { setOpen(false); document.getElementById("catalog")?.scrollIntoView(); }
    else window.location.assign(`/?q=${encodeURIComponent(trimmed)}#catalog`);
  };
  const choose = (index: number) => {
    if (index < matches.length) window.location.assign(`/product/${encodeURIComponent(matches[index].id)}`);
    else allResults();
  };
  useEffect(() => {
    if (!loaded || !pendingEnter.current) return;
    pendingEnter.current = false;
    choose(activeRef.current >= 0 ? activeRef.current : 0);
    // This effect deliberately resumes the keyboard action that happened
    // while the catalogue request was still in flight.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded]);
  const keyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault(); setOpen(true);
      const current = activeRef.current;
      const next = event.key === "ArrowDown" ? (current + 1) % optionCount : (current <= 0 ? optionCount - 1 : current - 1);
      activeRef.current = next;
      setActive(next);
    } else if (event.key === "Enter" && open && activeRef.current >= 0) {
      event.preventDefault();
      if (!loaded) pendingEnter.current = true;
      else choose(activeRef.current);
    } else if (event.key === "Escape") { setOpen(false); activeRef.current = -1; setActive(-1); }
  };

  return <div className={`${className} catalog-search`} ref={root}>
    <form onSubmit={(event: FormEvent) => {
      event.preventDefault();
      // Mobile Safari may submit the form without delivering the preceding
      // Enter keydown. Preserve the highlighted keyboard option in that path.
      if (!loaded) {
        pendingEnter.current = true;
      } else if (activeRef.current >= 0) {
        choose(activeRef.current);
      } else if (matches.length > 0) {
        // iOS virtual keyboards sometimes submit without emitting ArrowDown.
        // In an open autocomplete Enter therefore accepts the first visible
        // product; the explicit «Все результаты» row remains available.
        choose(0);
      } else allResults();
    }}>
      <span aria-hidden="true">⌕</span>
      <input autoFocus={autoFocus} value={query} onChange={(event) => { setQuery(event.target.value); setOpen(true); activeRef.current = -1; setActive(-1); }}
        onFocus={() => setOpen(true)} onKeyDown={keyDown} placeholder={placeholder} autoComplete="off"
        role="combobox" aria-expanded={open && Boolean(query.trim())} aria-controls={listID}
        aria-activedescendant={active >= 0 ? `${listID}-${active}` : undefined} />
      {query && <button type="button" className="catalog-search-clear" onClick={() => { setQuery(""); setOpen(false); }} aria-label="Очистить поиск">×</button>}
    </form>
    {open && query.trim() && <div className="catalog-search-options" id={listID} role="listbox">
      {matches.map((item, index) => <button id={`${listID}-${index}`} role="option" aria-selected={active === index}
        className={active === index ? "active" : ""} key={item.id} type="button" onMouseDown={(event) => event.preventDefault()} onClick={() => choose(index)}>
        {item.image && <img src={item.image} alt="" />}<span><b>{item.name}</b><small>{item.latin}</small></span>
      </button>)}
      <button id={`${listID}-${matches.length}`} role="option" aria-selected={active === matches.length}
        className={`catalog-search-all ${active === matches.length ? "active" : ""}`} type="button" onClick={allResults}>
        Все результаты <span>{resultCount}</span>
      </button>
    </div>}
  </div>;
}
