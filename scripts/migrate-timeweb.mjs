import fs from "node:fs";
import fsPromises from "node:fs/promises";
import path from "node:path";
import postgres from "postgres";

const connectionString = process.env.DATABASE_URL;
if (!connectionString) {
  throw new Error("DATABASE_URL is not set");
}

function getDatabaseCertificate() {
  const bundledCertificate = path.join(process.cwd(), "timeweb", "ca.crt");
  if (fs.existsSync(bundledCertificate)) {
    return fs.readFileSync(bundledCertificate, "utf8").trim();
  }

  return process.env.DATABASE_SSL_CA
    ?.replaceAll("\\n", "\n")
    .replace(/^["']|["']$/g, "")
    .trim();
}

function shouldVerifyDatabaseCertificate() {
  const value = process.env.DATABASE_SSL_VERIFY?.trim().toLowerCase();
  return value !== "false" && value !== "0";
}

const certificate = getDatabaseCertificate();
const verifyDatabaseCertificate = shouldVerifyDatabaseCertificate();
const ssl = verifyDatabaseCertificate
  ? certificate
    ? { ca: certificate, rejectUnauthorized: true }
    : undefined
  : { rejectUnauthorized: false };

if (!verifyDatabaseCertificate) {
  process.stderr.write(
    "WARNING: PostgreSQL TLS certificate verification is disabled.\n",
  );
}

const sql = postgres(connectionString, {
  max: 1,
  connect_timeout: 20,
  idle_timeout: 5,
  ...(ssl ? { ssl } : {}),
});

try {
  await sql`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      name TEXT PRIMARY KEY,
      applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
    )
  `;

  const directory = path.join(process.cwd(), "timeweb", "migrations");
  const files = (await fsPromises.readdir(directory))
    .filter((name) => name.endsWith(".sql"))
    .sort();

  for (const name of files) {
    const [existing] = await sql`
      SELECT name FROM schema_migrations WHERE name = ${name}
    `;
    if (existing) continue;

    const migration = await fsPromises.readFile(
      path.join(directory, name),
      "utf8",
    );
    await sql.begin(async (transaction) => {
      await transaction.unsafe(migration);
      await transaction`
        INSERT INTO schema_migrations (name) VALUES (${name})
      `;
    });
    process.stdout.write(`Applied ${name}\n`);
  }
} finally {
  await sql.end();
}
