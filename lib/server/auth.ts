import {
  createHash,
  randomBytes,
  scrypt as scryptCallback,
  timingSafeEqual,
} from "node:crypto";
import { promisify } from "node:util";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { getPostgres } from "./postgres";

const scrypt = promisify(scryptCallback);
const COOKIE_NAME = "ficusin_session";
const KEY_LENGTH = 64;

export type StoreUser = {
  id: number;
  email: string;
  phone: string;
  fullName: string;
  accountType: "retail" | "wholesale";
  wholesaleStatus: string;
  retailDiscountBps: number;
};

export async function hashPassword(password: string) {
  const salt = randomBytes(16);
  const derived = (await scrypt(password, salt, KEY_LENGTH)) as Buffer;
  return `scrypt$16384$8$1$${salt.toString("base64url")}$${derived.toString("base64url")}`;
}

export async function verifyPassword(password: string, encoded: string) {
  const parts = encoded.split("$");
  if (parts.length !== 6 || parts[0] !== "scrypt") return false;

  try {
    const salt = Buffer.from(parts[4], "base64url");
    const expected = Buffer.from(parts[5], "base64url");
    const actual = (await scrypt(password, salt, expected.length)) as Buffer;
    return expected.length === actual.length && timingSafeEqual(expected, actual);
  } catch {
    return false;
  }
}

export function passwordIsAcceptable(password: string) {
  return (
    password.length >= 10 &&
    password.length <= 128 &&
    /\p{L}/u.test(password) &&
    /\d/.test(password)
  );
}

export async function createSession(customerId: number, userAgent?: string | null) {
  const sql = getPostgres();
  const rawToken = randomBytes(32).toString("base64url");
  const tokenHash = createHash("sha256").update(rawToken).digest("hex");
  const days = Math.min(90, Math.max(1, Number(process.env.AUTH_SESSION_DAYS) || 30));
  const expiresAt = new Date(Date.now() + days * 86_400_000);

  await sql`
    INSERT INTO auth_sessions (token_hash, customer_id, expires_at, user_agent)
    VALUES (${tokenHash}, ${customerId}, ${expiresAt}, ${userAgent?.slice(0, 500) ?? null})
  `;

  const cookieStore = await cookies();
  cookieStore.set(COOKIE_NAME, rawToken, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.AUTH_COOKIE_SECURE !== "false",
    path: "/",
    expires: expiresAt,
  });
}

export async function destroySession() {
  const cookieStore = await cookies();
  const rawToken = cookieStore.get(COOKIE_NAME)?.value;
  if (rawToken) {
    const tokenHash = createHash("sha256").update(rawToken).digest("hex");
    try {
      await getPostgres()`DELETE FROM auth_sessions WHERE token_hash = ${tokenHash}`;
    } catch {
      // The cookie is still cleared even if the database is temporarily unavailable.
    }
  }
  cookieStore.delete(COOKIE_NAME);
}

export async function getStoreUser(): Promise<StoreUser | null> {
  const rawToken = (await cookies()).get(COOKIE_NAME)?.value;
  if (!rawToken) return null;

  const tokenHash = createHash("sha256").update(rawToken).digest("hex");
  const rows = await getPostgres()`
    SELECT
      c.id,
      c.email,
      c.phone,
      c.full_name,
      c.account_type,
      c.wholesale_status,
      c.retail_discount_bps
    FROM auth_sessions s
    JOIN customers c ON c.id = s.customer_id
    WHERE s.token_hash = ${tokenHash}
      AND s.expires_at > CURRENT_TIMESTAMP
      AND c.is_active = TRUE
    LIMIT 1
  `;
  const row = rows[0];
  if (!row) return null;

  return {
    id: Number(row.id),
    email: String(row.email),
    phone: String(row.phone),
    fullName: String(row.full_name),
    accountType: row.account_type === "wholesale" ? "wholesale" : "retail",
    wholesaleStatus: String(row.wholesale_status),
    retailDiscountBps: Number(row.retail_discount_bps),
  };
}

export async function requireStoreUser(returnTo = "/account") {
  const user = await getStoreUser();
  if (user) return user;
  redirect(`/login?returnTo=${encodeURIComponent(safeReturnTo(returnTo))}`);
}

export function safeReturnTo(value: string | null | undefined) {
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/account";
  return value;
}
