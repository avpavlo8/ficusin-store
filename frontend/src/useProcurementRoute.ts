import { useEffect, useState } from "react";

const views = ["orders", "drafts", "calculator", "recommendations", "products", "unlinkedSales", "requests", "availability", "integrations", "settings"] as const;
type View = typeof views[number];
export function useProcurementParam(key: string, fallback: string) {
  const readParam = () => new URLSearchParams(window.location.search).get(key) ?? fallback;
  const [value, setValue] = useState(readParam);
  useEffect(() => {
    const pop = () => setValue(new URLSearchParams(window.location.search).get(key) ?? fallback);
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, [key, fallback]);
  return [value, (next: string) => {
    const url = new URL(window.location.href);
    if (next === fallback) url.searchParams.delete(key); else url.searchParams.set(key, next);
    window.history.replaceState(null, "", url);
    setValue(next);
  }] as const;
}
const read = () => {
  const params = new URLSearchParams(window.location.search);
  const view = params.get("view") as View;
  const order = Number(params.get("order"));
  return { view: views.includes(view) ? view : "orders" as View, order: Number.isSafeInteger(order) && order > 0 ? order : null };
};
export function useProcurementRoute() {
  const [route, setRoute] = useState(read);
  useEffect(() => {
    const pop = () => setRoute(read());
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, []);
  const navigate = (patch: Partial<typeof route>) => {
    const next = { ...read(), ...patch };
    const url = new URL(window.location.href);
    url.searchParams.set("view", next.view);
    if (next.order) url.searchParams.set("order", String(next.order)); else url.searchParams.delete("order");
    window.history.pushState(null, "", url);
    setRoute(next);
  };
  return { view: route.view, selectedOrder: route.order, setView: (view: View) => navigate({ view, order: null }), setSelectedOrder: (order: number | null) => navigate({ order }) };
}
