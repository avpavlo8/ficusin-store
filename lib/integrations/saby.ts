import type { IntegrationEnv } from "./types";
import { requireSetting } from "./types";

export type SabyCatalogItem = {
  id: number;
  externalId?: string;
  article?: string;
  name: string;
  description?: string;
  cost?: number;
  balance?: string | number;
  images?: string[];
  published?: boolean;
  isParent?: boolean;
};

type SabyCatalogResponse =
  | SabyCatalogItem[]
  | {
      nomenclatures?: SabyCatalogItem[];
      items?: SabyCatalogItem[];
      result?: SabyCatalogItem[];
    };

export function isSabyConfigured(env: IntegrationEnv) {
  return Boolean(
    env.SABY_ACCESS_TOKEN && env.SABY_POINT_ID && env.SABY_PRICE_LIST_ID,
  );
}

export async function fetchSabyCatalog(
  env: IntegrationEnv,
  page = 0,
): Promise<SabyCatalogItem[]> {
  const params = new URLSearchParams({
    pointId: requireSetting(env, "SABY_POINT_ID"),
    priceListId: requireSetting(env, "SABY_PRICE_LIST_ID"),
    withBalance: "true",
    withBarcode: "true",
    noStopList: "true",
    pageSize: "1000",
    page: String(page),
  });
  const response = await fetch(
    `https://api.sbis.ru/retail/v2/nomenclature/list?${params}`,
    {
      headers: {
        "X-SBISAccessToken": requireSetting(env, "SABY_ACCESS_TOKEN"),
      },
    },
  );
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Saby не вернул каталог: ${response.status} ${body}`);
  }
  const data = (await response.json()) as SabyCatalogResponse;
  if (Array.isArray(data)) return data;
  return data.nomenclatures ?? data.items ?? data.result ?? [];
}
