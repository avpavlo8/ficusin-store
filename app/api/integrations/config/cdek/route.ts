import { storeEncryptedCredentials } from "../../../../../lib/integrations/credentials";

const AUDIENCE = "ficusin-store-cdek-config";
const REPOSITORY = "avpavlo8/ficusin-store";
const WORKFLOW_REF =
  "avpavlo8/ficusin-store/.github/workflows/cdek-config-sync.yml@refs/heads/main";

function decodeBase64Url(value: string) {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function decodeJson(value: string) {
  return JSON.parse(new TextDecoder().decode(decodeBase64Url(value))) as Record<
    string,
    unknown
  >;
}

async function verifyGithubToken(token: string) {
  const parts = token.split(".");
  if (parts.length !== 3) throw new Error("Некорректный токен автоматизации");
  const header = decodeJson(parts[0]);
  const claims = decodeJson(parts[1]);
  if (header.alg !== "RS256" || typeof header.kid !== "string") {
    throw new Error("Неподдерживаемый токен автоматизации");
  }

  const configuration = (await fetch(
    "https://token.actions.githubusercontent.com/.well-known/openid-configuration",
  ).then((response) => response.json())) as { jwks_uri: string };
  const keys = (await fetch(configuration.jwks_uri).then((response) =>
    response.json(),
  )) as { keys: JsonWebKey[] };
  const jwk = keys.keys.find((key) => key.kid === header.kid);
  if (!jwk) throw new Error("Ключ автоматизации не найден");
  const publicKey = await crypto.subtle.importKey(
    "jwk",
    jwk,
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["verify"],
  );
  const validSignature = await crypto.subtle.verify(
    "RSASSA-PKCS1-v1_5",
    publicKey,
    decodeBase64Url(parts[2]),
    new TextEncoder().encode(`${parts[0]}.${parts[1]}`),
  );
  const now = Math.floor(Date.now() / 1000);
  if (
    !validSignature ||
    claims.iss !== "https://token.actions.githubusercontent.com" ||
    claims.aud !== AUDIENCE ||
    claims.repository !== REPOSITORY ||
    claims.ref !== "refs/heads/main" ||
    claims.workflow_ref !== WORKFLOW_REF ||
    typeof claims.exp !== "number" ||
    claims.exp < now ||
    (typeof claims.nbf === "number" && claims.nbf > now + 30)
  ) {
    throw new Error("Автоматизация не прошла проверку");
  }
}

export async function POST(request: Request) {
  try {
    const authorization = request.headers.get("Authorization") ?? "";
    if (!authorization.startsWith("Bearer ")) {
      return Response.json({ error: "Требуется авторизация" }, { status: 401 });
    }
    await verifyGithubToken(authorization.slice(7));
    const envelope = (await request.json()) as {
      encryptedKey: string;
      iv: string;
      ciphertext: string;
      tag: string;
    };
    await storeEncryptedCredentials("cdek", envelope);
    return Response.json({ ok: true });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : "Настройка не сохранена" },
      { status: 403 },
    );
  }
}
