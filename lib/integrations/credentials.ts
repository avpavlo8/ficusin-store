type EncryptedEnvelope = {
  encryptedKey: string;
  iv: string;
  ciphertext: string;
  tag: string;
};

export type CdekCredentials = {
  clientId: string;
  clientSecret: string;
};

export type TelegramCredentials = {
  botToken: string;
};

const textDecoder = new TextDecoder();
const cached = new Map<
  string,
  { credentials: unknown; expiresAt: number }
>();

async function getRuntimeEnv() {
  const { env } = await import("cloudflare:workers");
  return env;
}

function fromBase64(value: string) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function pemToBytes(pem: string) {
  return fromBase64(
    pem
      .replace("-----BEGIN PRIVATE KEY-----", "")
      .replace("-----END PRIVATE KEY-----", "")
      .replace(/\s/g, ""),
  );
}

async function decryptEnvelope<T>(envelope: EncryptedEnvelope) {
  const env = await getRuntimeEnv();
  const privateKeyPem = env.INTEGRATION_SECRETS_PRIVATE_KEY?.trim();
  if (!privateKeyPem) throw new Error("Ключ защищённых интеграций не настроен");

  const privateKey = await crypto.subtle.importKey(
    "pkcs8",
    pemToBytes(privateKeyPem),
    { name: "RSA-OAEP", hash: "SHA-256" },
    false,
    ["decrypt"],
  );
  const aesKeyBytes = await crypto.subtle.decrypt(
    { name: "RSA-OAEP" },
    privateKey,
    fromBase64(envelope.encryptedKey),
  );
  const aesKey = await crypto.subtle.importKey(
    "raw",
    aesKeyBytes,
    { name: "AES-GCM" },
    false,
    ["decrypt"],
  );
  const encrypted = new Uint8Array([
    ...fromBase64(envelope.ciphertext),
    ...fromBase64(envelope.tag),
  ]);
  const clear = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: fromBase64(envelope.iv), tagLength: 128 },
    aesKey,
    encrypted,
  );
  return JSON.parse(textDecoder.decode(clear)) as T;
}

async function getEncryptedCredentials<T>(provider: "cdek" | "telegram") {
  const current = cached.get(provider);
  if (current && current.expiresAt > Date.now()) {
    return current.credentials as T;
  }
  const env = await getRuntimeEnv();
  const row = await env.DB.prepare(
    "SELECT encrypted_payload FROM integration_credentials WHERE provider = ? LIMIT 1",
  )
    .bind(provider)
    .first<{ encrypted_payload: string }>();
  if (!row?.encrypted_payload) {
    throw new Error(`Доступ к интеграции ${provider} ещё не передан сайту`);
  }

  const credentials = await decryptEnvelope<T>(
    JSON.parse(row.encrypted_payload) as EncryptedEnvelope,
  );
  cached.set(provider, {
    credentials,
    expiresAt: Date.now() + 10 * 60 * 1000,
  });
  return credentials;
}

export async function getCdekCredentials() {
  const credentials = await getEncryptedCredentials<CdekCredentials>("cdek");
  if (!credentials.clientId?.trim() || !credentials.clientSecret?.trim()) {
    throw new Error("Данные доступа к СДЭК неполные");
  }
  return credentials;
}

export async function getTelegramCredentials() {
  const credentials =
    await getEncryptedCredentials<TelegramCredentials>("telegram");
  if (!credentials.botToken?.trim()) {
    throw new Error("Токен Telegram-бота не настроен");
  }
  return credentials;
}

export async function storeEncryptedCredentials(
  provider: "cdek" | "telegram",
  envelope: EncryptedEnvelope,
) {
  const env = await getRuntimeEnv();
  const fields = ["encryptedKey", "iv", "ciphertext", "tag"] as const;
  if (fields.some((field) => !envelope[field] || envelope[field].length > 4096)) {
    throw new Error("Некорректный защищённый пакет");
  }
  await env.DB.prepare(
    `INSERT INTO integration_credentials (provider, encrypted_payload, updated_at)
     VALUES (?, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(provider) DO UPDATE SET
       encrypted_payload = excluded.encrypted_payload,
       updated_at = CURRENT_TIMESTAMP`,
  )
    .bind(provider, JSON.stringify(envelope))
    .run();
  cached.delete(provider);
}
