// Shared by both browser configurations and the session bootstrap: credentials
// may only reach a supported isolated staging origin, never production.
export function isStagingOrigin(value: string): boolean {
  try {
    const url = new URL(value);
    return (
      url.protocol === "https:" &&
      url.port === "" &&
      url.origin === value &&
      (url.hostname === "staging.ficusin.ru" ||
        (url.hostname.endsWith(".twc1.net") &&
          url.hostname.length > ".twc1.net".length))
    );
  } catch {
    return false;
  }
}
