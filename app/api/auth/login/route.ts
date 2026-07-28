import { headers } from "next/headers";
import { normalizeRussianPhone } from "../../../../lib/phone";
import { createSession, verifyPassword } from "../../../../lib/server/auth";
import { getPostgres } from "../../../../lib/server/postgres";

export const runtime = "nodejs";

export async function POST(request: Request) {
  let body: { identifier?: string; password?: string };
  try {
    body = (await request.json()) as { identifier?: string; password?: string };
  } catch {
    return Response.json({ error: "Некорректные данные формы" }, { status: 400 });
  }

  const identifier = body.identifier?.trim() ?? "";
  const password = body.password ?? "";
  const phone = normalizeRussianPhone(identifier);
  const email = identifier.toLowerCase();

  if (!identifier || !password) {
    return Response.json({ error: "Введите телефон или email и пароль" }, { status: 400 });
  }

  try {
    const rows = await getPostgres()`
      SELECT id, password_hash, is_active
      FROM customers
      WHERE ${phone ? getPostgres()`phone = ${phone}` : getPostgres()`LOWER(email) = ${email}`}
      LIMIT 1
    `;
    const customer = rows[0];
    const valid =
      customer &&
      customer.is_active &&
      (await verifyPassword(password, String(customer.password_hash)));

    if (!valid) {
      return Response.json(
        { error: "Неверный телефон, email или пароль" },
        { status: 401 },
      );
    }

    const userAgent = (await headers()).get("user-agent");
    await createSession(Number(customer.id), userAgent);
    return Response.json({ ok: true });
  } catch (error) {
    console.error("Login failed", error);
    return Response.json(
      { error: "Не удалось войти. Попробуйте позднее" },
      { status: 500 },
    );
  }
}
