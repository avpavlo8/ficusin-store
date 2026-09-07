export class APIError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = "APIError";
  }
}

const sharedReads = new Map<string, Promise<unknown>>();

export async function apiJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  if (!response.ok) {
    let message = "Сервис временно недоступен";
    try {
      const body = await response.json() as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // A proxy can return HTML; never expose it to the customer.
    }
    throw new APIError(message, response.status);
  }
  return response.json() as Promise<T>;
}

// Public immutable reads are shared by every mounted consumer. The header
// search and the current page therefore cannot download the same catalogue
// twice during one navigation.
export function sharedAPIJSON<T>(url: string, force = false): Promise<T> {
  if (force) sharedReads.delete(url);
  const current = sharedReads.get(url);
  if (current) return current as Promise<T>;
  const pending = apiJSON<T>(url).catch((error) => {
    sharedReads.delete(url);
    throw error;
  });
  sharedReads.set(url, pending);
  return pending;
}
