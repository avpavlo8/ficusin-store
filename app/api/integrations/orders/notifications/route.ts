import { getRuntimeEnv } from "../../../../../lib/server/runtime-db";

type GithubClaims = {
  iss?: string;
  aud?: string | string[];
  exp?: number;
  nbf?: number;
  repository?: string;
  ref?: string;
  workflow_ref?: string;
};

type Jwks = {
  keys: Array<JsonWebKey & { kid?: string; alg?: string }>;
};

type PendingOrder = {
  order_number: string;
  customer_name: string;
  phone: string;
  email: string;
  address: string;
  comment: string;
  delivery_method: string;
  delivery_fee: number;
  subtotal: number;
  total: number;
  created_at: string;
};

type OrderItem = {
  order_number: string;
  product_name: string;
  unit_price: number;
  quantity: number;
};

const EXPECTED_AUDIENCE = "ficusin-store-order-notifications";
const EXPECTED_REPOSITORY = "avpavlo8/ficusin-store";
const EXPECTED_REF = "refs/heads/main";
const EXPECTED_WORKFLOW =
  "avpavlo8/ficusin-store/.github/workflows/order-notifications.yml@refs/heads/main";

let cachedJwks: { value: Jwks; expiresAt: number } | undefined;

class NotificationAuthError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

function decodeBase64Url(value: string) {
  const base64 = value.replaceAll("-", "+").replaceAll("_", "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  const decoded = atob(padded);
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

function decodeJson<T>(value: string): T {
  return JSON.parse(new TextDecoder().decode(decodeBase64Url(value))) as T;
}

async function getGithubJwks(): Promise<Jwks> {
  if (cachedJwks && cachedJwks.expiresAt > Date.now()) {
    return cachedJwks.value;
  }
  const response = await fetch(
    "https://token.actions.githubusercontent.com/.well-known/jwks",
  );
  if (!response.ok) {
    throw new NotificationAuthError(
      "jwks-fetch",
      "Не удалось проверить подпись GitHub",
    );
  }
  const value = (await response.json()) as Jwks;
  cachedJwks = { value, expiresAt: Date.now() + 60 * 60 * 1000 };
  return value;
}

async function verifyGithubToken(token: string) {
  const parts = token.split(".");
  if (parts.length !== 3) {
    throw new NotificationAuthError("jwt-format", "Некорректный токен GitHub");
  }
  const [encodedHeader, encodedPayload, encodedSignature] = parts;
  const header = decodeJson<{ alg?: string; kid?: string }>(encodedHeader);
  if (header.alg !== "RS256" || !header.kid) {
    throw new NotificationAuthError(
      "jwt-header",
      "Неподдерживаемая подпись GitHub",
    );
  }

  const jwks = await getGithubJwks();
  const jwk = jwks.keys.find((key) => key.kid === header.kid);
  if (!jwk) {
    throw new NotificationAuthError(
      "jwks-kid",
      "Ключ подписи GitHub не найден",
    );
  }
  const key = await crypto.subtle.importKey(
    "jwk",
    jwk,
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["verify"],
  );
  const signatureValid = await crypto.subtle.verify(
    "RSASSA-PKCS1-v1_5",
    key,
    decodeBase64Url(encodedSignature),
    new TextEncoder().encode(`${encodedHeader}.${encodedPayload}`),
  );
  if (!signatureValid) {
    throw new NotificationAuthError(
      "jwt-signature",
      "Подпись GitHub не прошла проверку",
    );
  }

  const claims = decodeJson<GithubClaims>(encodedPayload);
  const now = Math.floor(Date.now() / 1000);
  const audiences = Array.isArray(claims.aud) ? claims.aud : [claims.aud];
  if (
    claims.iss !== "https://token.actions.githubusercontent.com" ||
    !audiences.includes(EXPECTED_AUDIENCE) ||
    !claims.exp ||
    claims.exp <= now ||
    (claims.nbf ?? 0) > now ||
    claims.repository !== EXPECTED_REPOSITORY ||
    claims.ref !== EXPECTED_REF ||
    claims.workflow_ref !== EXPECTED_WORKFLOW
  ) {
    throw new NotificationAuthError(
      "jwt-claims",
      "GitHub Actions не имеет доступа к заказам",
    );
  }
}

export async function POST(request: Request) {
  const suppliedToken = request.headers
    .get("X-Ficusin-GitHub-OIDC")
    ?.trim();
  if (!suppliedToken) {
    return Response.json({ error: "Доступ запрещён" }, { status: 401 });
  }

  try {
    await verifyGithubToken(suppliedToken);
  } catch (error) {
    const code =
      error instanceof NotificationAuthError ? error.code : "auth-error";
    console.error("Отклонён доступ к уведомлениям о заказах", error);
    return Response.json(
      { error: "Доступ запрещён", code },
      {
        status: 403,
        headers: { "X-Order-Notification-Error": code },
      },
    );
  }

  const env = getRuntimeEnv();
  const body = (await request.json()) as {
    action?: "list" | "ack";
    orderNumbers?: string[];
  };

  if (body.action === "ack") {
    const orderNumbers = Array.from(
      new Set(
        (body.orderNumbers ?? [])
          .filter((value) => typeof value === "string")
          .map((value) => value.trim())
          .filter(Boolean),
      ),
    ).slice(0, 20);
    if (!orderNumbers.length) {
      return Response.json({ error: "Не указаны заказы" }, { status: 400 });
    }
    await env.DB.batch(
      orderNumbers.map((orderNumber) =>
        env.DB.prepare(`
          UPDATE orders
          SET telegram_notified_at = CURRENT_TIMESTAMP
          WHERE order_number = ? AND telegram_notified_at IS NULL
        `).bind(orderNumber),
      ),
    );
    return Response.json({ ok: true, acknowledged: orderNumbers.length });
  }

  if (body.action !== "list") {
    return Response.json({ error: "Неизвестное действие" }, { status: 400 });
  }

  const orders = await env.DB.prepare(`
    SELECT
      order_number, customer_name, phone, email, address, comment,
      delivery_method, delivery_fee, subtotal, total, created_at
    FROM orders
    WHERE telegram_notified_at IS NULL
    ORDER BY created_at ASC
    LIMIT 20
  `).all<PendingOrder>();

  if (!orders.results.length) {
    return Response.json({ orders: [] });
  }

  const items = await env.DB.prepare(`
    SELECT
      o.order_number, oi.product_name, oi.unit_price, oi.quantity
    FROM order_items oi
    JOIN orders o ON o.id = oi.order_id
    WHERE o.telegram_notified_at IS NULL
    ORDER BY oi.id ASC
    LIMIT 500
  `).all<OrderItem>();

  return Response.json({
    orders: orders.results.map((order) => ({
      orderNumber: order.order_number,
      customerName: order.customer_name,
      phone: order.phone,
      email: order.email,
      address: order.address,
      comment: order.comment,
      deliveryMethod: order.delivery_method,
      deliveryFee: Number(order.delivery_fee),
      subtotal: Number(order.subtotal),
      total: Number(order.total),
      createdAt: order.created_at,
      items: items.results
        .filter((item) => item.order_number === order.order_number)
        .map((item) => ({
          name: item.product_name,
          price: Number(item.unit_price),
          quantity: Number(item.quantity),
        })),
    })),
  });
}
