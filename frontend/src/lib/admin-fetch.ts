const temporaryStatuses = new Set([502, 503, 504]);

function waitForRetry(delay: number, signal?: AbortSignal | null): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer);
      reject(signal?.reason || new DOMException("Aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, delay);
    if (signal?.aborted) abort();
    else signal?.addEventListener("abort", abort, { once: true });
  });
}

// Only reads may be repeated. A lost response to a mutation does not mean it failed.
export async function adminFetch(
  path: string,
  options: RequestInit = {},
  fetcher: typeof fetch = fetch,
): Promise<Response> {
  const read = ["GET", "HEAD"].includes((options.method || "GET").toUpperCase());
  for (let attempt = 0; ; attempt++) {
    try {
      const response = await fetcher(path, options);
      if (!read || !temporaryStatuses.has(response.status) || attempt >= 2) return response;
      await response.body?.cancel().catch(() => undefined);
    } catch (cause) {
      if (!read || attempt >= 2 || options.signal?.aborted || !(cause instanceof TypeError))
        throw cause;
    }
    await waitForRetry(300 * (attempt + 1), options.signal);
  }
}
