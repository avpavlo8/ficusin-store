import fs from "node:fs";
import path from "node:path";
import postgres from "postgres";

declare global {
  var ficusinPostgres: ReturnType<typeof postgres> | undefined;
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

export function getPostgres() {
  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error("DATABASE_URL is not configured");
  }

  if (!globalThis.ficusinPostgres) {
    const certificate = getDatabaseCertificate();
    const verifyDatabaseCertificate = shouldVerifyDatabaseCertificate();
    const ssl = verifyDatabaseCertificate
      ? certificate
        ? { ca: certificate, rejectUnauthorized: true }
        : undefined
      : { rejectUnauthorized: false };

    globalThis.ficusinPostgres = postgres(connectionString, {
      max: 10,
      idle_timeout: 20,
      connect_timeout: 20,
      prepare: true,
      ...(ssl ? { ssl } : {}),
    });
  }

  return globalThis.ficusinPostgres;
}
