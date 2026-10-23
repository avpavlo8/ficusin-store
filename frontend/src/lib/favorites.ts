import { useCallback, useEffect, useState } from "react";
import { sharedAPIJSON } from "./api";

export const FAVORITES_EVENT = "ficusin-storage";
export function readFavorites(): Set<string> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem("ficusin-favorites") || "[]");
    // Older clients stored numeric IDs; the catalogue exposes strings.
    return new Set(
      Array.isArray(stored)
        ? stored
            .filter((id) => typeof id === "string" || typeof id === "number")
            .map(String)
            .filter(Boolean)
        : [],
    );
  } catch {
    return new Set();
  }
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
  const setFavorites = useCallback(
    (change: Set<string> | ((current: Set<string>) => Set<string>)) => {
      const next = typeof change === "function" ? change(readFavorites()) : change;
      localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      window.dispatchEvent(new Event(FAVORITES_EVENT));
    },
    [],
  );
  return [favorites, setFavorites] as const;
}

// Count what the favorites page can actually display; retain unavailable IDs
// in storage so an unpublished card can return without losing a saved choice.
export function useAvailableFavoriteCount(favorites: Set<string>) {
  const [available, setAvailable] = useState<Set<string> | null>(null);
  const hasFavorites = favorites.size > 0;
  useEffect(() => {
    if (!hasFavorites) return;
    let active = true;
    sharedAPIJSON<{ products?: Array<{ id: string }> }>("/api/v1/catalog")
      .then((data) => {
        if (active && Array.isArray(data.products))
          setAvailable(new Set(data.products.map((product) => String(product.id))));
      })
      .catch(() => {
        /* A failed read is not evidence that saved products vanished. */
      });
    return () => {
      active = false;
    };
  }, [hasFavorites]);
  if (!hasFavorites) return 0;
  return available ? [...favorites].filter((id) => available.has(id)).length : undefined;
}
