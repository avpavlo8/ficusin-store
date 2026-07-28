import postgres from "postgres";

declare global {
  var ficusinPostgres: ReturnType<typeof postgres> | undefined;
}

export function getPostgres() {
  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error("DATABASE_URL is not configured");
  }

  if (!globalThis.ficusinPostgres) {
    globalThis.ficusinPostgres = postgres(connectionString, {
      max: 10,
      idle_timeout: 20,
      connect_timeout: 20,
      prepare: true,
    });
  }

  return globalThis.ficusinPostgres;
}
