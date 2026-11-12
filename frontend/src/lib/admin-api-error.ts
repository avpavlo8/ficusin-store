export function adminApiError(
  status: number,
  path: string,
  method = "GET",
  serverMessage?: string,
  requestId?: string | null,
): Error {
  const generic = !serverMessage || serverMessage === "Не удалось выполнить операцию";
  const message = generic
    ? status === 404
      ? "Сервер не нашёл запрошенные данные или действие"
      : status === 405
        ? "Эта версия сервера не поддерживает действие"
        : status >= 500
          ? "Сервер не смог выполнить запрос"
          : status >= 400
            ? "Сервер отклонил запрос"
            : "Сервер вернул некорректный ответ"
    : serverMessage;
  const endpoint = path.split(/[?#]/)[0];
  return new Error(
    `${message}. HTTP ${status}, ${method.toUpperCase()} ${endpoint}${requestId ? `. Код запроса: ${requestId}` : ""}`,
  );
}
