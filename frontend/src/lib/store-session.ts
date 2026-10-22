let request: Promise<Response> | undefined;

// The header, product tools and checkout share a single profile read per page.
export function storeSession() {
  request ??= fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store" })
    .catch((error) => { request = undefined; throw error; });
  return request.then((response) => response.clone());
}
