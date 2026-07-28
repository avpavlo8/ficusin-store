import fs from "node:fs/promises";
import path from "node:path";
import postgres from "postgres";

const connectionString = process.env.DATABASE_URL;
if (!connectionString) {
  throw new Error("DATABASE_URL is not set");
}

const sql = postgres(connectionString, {
  max: 1,
  connect_timeout: 20,
  idle_timeout: 5,
});

try {
  await sql`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      name TEXT PRIMARY KEY,
      applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
    )
  `;

  const directory = path.join(process.cwd(), "timeweb", "migrations");
  const files = (await fs.readdir(directory))
    .filter((name) => name.endsWith(".sql"))
    .sort();

  for (const name of files) {
    const [existing] = await sql`
      SELECT name FROM schema_migrations WHERE name = ${name}
    `;
    if (existing) continue;

    const migration = await fs.readFile(path.join(directory, name), "utf8");
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
