import { getPostgres } from "./postgres";

type SqlClient = Pick<ReturnType<typeof getPostgres>, "unsafe">;

export type RuntimeResult<T = Record<string, unknown>> = {
  results: T[];
  meta: { changes: number; last_row_id?: number };
};

class RuntimeStatement {
  constructor(
    private readonly source: string,
    private readonly values: unknown[] = [],
  ) {}

  bind(...values: unknown[]) {
    return new RuntimeStatement(this.source, values);
  }

  async all<T = Record<string, unknown>>(
    client: SqlClient = getPostgres(),
  ): Promise<RuntimeResult<T>> {
    const rows = await client.unsafe<T[]>(
      toPostgresSql(this.source),
      this.values as never[],
    );
    return { results: rows as T[], meta: { changes: rows.count ?? 0 } };
  }

  async first<T = Record<string, unknown>>(): Promise<T | null> {
    const result = await this.all<T>();
    return result.results[0] ?? null;
  }

  async run(): Promise<RuntimeResult> {
    let query = toPostgresSql(this.source);
    const wantsId =
      /^\s*INSERT\s+INTO\b/i.test(query) &&
      !/\bRETURNING\b/i.test(query) &&
      !/\bON\s+CONFLICT\b/i.test(query);
    if (wantsId) query = `${query.replace(/;\s*$/, "")} RETURNING id`;
    const rows = await getPostgres().unsafe<Record<string, unknown>[]>(
      query,
      this.values as never[],
    );
    const id = rows[0]?.id;
    return {
      results: rows,
      meta: {
        changes: rows.count ?? 0,
        ...(id === undefined ? {} : { last_row_id: Number(id) }),
      },
    };
  }
}

function toPostgresSql(source: string) {
  let index = 0;
  return source.replace(/\?/g, () => `$${++index}`);
}

export const runtimeDb = {
  prepare(query: string) {
    return new RuntimeStatement(query);
  },
  async batch<T extends RuntimeStatement[]>(statements: T) {
    return getPostgres().begin(async (transaction) =>
      Promise.all(statements.map((statement) => statement.all(transaction))),
    );
  },
};

export function getRuntimeEnv() {
  return {
    DB: runtimeDb,
    INTEGRATION_SECRETS_PRIVATE_KEY:
      process.env.INTEGRATION_SECRETS_PRIVATE_KEY,
  };
}
