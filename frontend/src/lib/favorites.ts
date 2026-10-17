import { useCallback, useEffect, useState } from "react";

export const FAVORITES_EVENT = "ficusin-storage";
export function readFavorites(): Set<string> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem("ficusin-favorites") || "[]");
    // Older clients stored numeric IDs; the catalogue exposes strings.
    return new Set(Array.isArray(stored) ? stored.filter((id) => typeof id === "string" || typeof id === "number").map(String).filter(Boolean) : []);
  } catch { return new Set(); }
}

export function useFavorites() {
  const [favorites, update] = useState(readFavorites);
  useEffect(() => {
    const sync = () => update(readFavorites());
    window.addEventListener("storage", sync);
    window.addEventListener(FAVORITES_EVENT, sync);
    return () => {
      window.removeEventListener("storage", sync);
      window.removeEventListener(FAVORITES_EVENT, sync);
    };
  }, []);
  const setFavorites = useCallback((change: Set<string> | ((current: Set<string>) => Set<string>)) => {
    const next = typeof change === "function" ? change(readFavorites()) : change;
    localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
    window.dispatchEvent(new Event(FAVORITES_EVENT));
  }, []);
  return [favorites, setFavorites] as const;
}
