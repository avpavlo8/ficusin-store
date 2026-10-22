import { apiJSON } from "./api";
export const chatRequest = <T,>(url: string, body: unknown, method = "POST") => apiJSON<T>(url, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
