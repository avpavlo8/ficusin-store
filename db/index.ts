import { getPostgres } from "../lib/server/postgres";

export function getDb() {
  return getPostgres();
}
