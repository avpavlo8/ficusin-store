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

const textDecoder = new TextDecoder();
let cached: { credentials: CdekCredentials; expiresAt: number } | null = null;

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

async function decryptEnvelope(envelope: EncryptedEnvelope) {
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
  return JSON.parse(textDecoder.decode(clear)) as CdekCredentials;
}

export async function getCdekCredentials() {
  if (cached && cached.expiresAt > Date.now()) return cached.credentials;

  const env = await getRuntimeEnv();
  const row = await env.DB.prepare(
    "SELECT encrypted_payload FROM integration_credentials WHERE provider = ? LIMIT 1",
  )
    .bind("cdek")
    .first<{ encrypted_payload: string }>();
  if (!row?.encrypted_payload) {
    throw new Error("Доступ к СДЭК ещё не передан сайту");
  }

  const credentials = await decryptEnvelope(
    JSON.parse(row.encrypted_payload) as EncryptedEnvelope,
  );
  if (!credentials.clientId?.trim() || !credentials.clientSecret?.trim()) {
    throw new Error("Данные доступа к СДЭК неполные");
  }
  cached = { credentials, expiresAt: Date.now() + 10 * 60 * 1000 };
  return credentials;
}

export async function storeEncryptedCredentials(
  provider: "cdek",
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
  cached = null;
}
