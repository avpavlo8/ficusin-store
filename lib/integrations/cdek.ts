import type { IntegrationEnv } from "./types";
import { requireSetting } from "./types";

type CdekToken = { access_token: string; expires_in: number };

export type CdekCity = {
  code: number;
  city: string;
  region?: string;
  country?: string;
  postal_codes?: string[];
};

export type CdekOffice = {
  code: string;
  name: string;
  location: {
    city: string;
    address: string;
    address_full?: string;
  };
  work_time?: string;
};

type CdekTariff = {
  tariff_code: number;
  tariff_name: string;
  tariff_description?: string;
  delivery_mode?: number;
  delivery_sum: number;
  period_min: number;
  period_max: number;
};

const getBaseUrl = (env: IntegrationEnv) =>
  (env.CDEK_API_URL?.trim() || "https://api.cdek.ru/v2").replace(/\/$/, "");

async function getToken(env: IntegrationEnv): Promise<string> {
  const body = new URLSearchParams({
    grant_type: "client_credentials",
    client_id: requireSetting(env, "CDEK_CLIENT_ID"),
    client_secret: requireSetting(env, "CDEK_CLIENT_SECRET"),
  });
  const response = await fetch(`${getBaseUrl(env)}/oauth/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!response.ok) {
    throw new Error(`СДЭК не выдал токен: ${response.status}`);
  }
  return ((await response.json()) as CdekToken).access_token;
}

async function cdekFetch<T>(
  env: IntegrationEnv,
  path: string,
  init?: RequestInit,
): Promise<T> {
  const token = await getToken(env);
  const response = await fetch(`${getBaseUrl(env)}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Ошибка СДЭК ${response.status}: ${body}`);
  }
  return (await response.json()) as T;
}

export function isCdekConfigured(env: IntegrationEnv) {
  return Boolean(env.CDEK_CLIENT_ID && env.CDEK_CLIENT_SECRET);
}

export async function findCities(
  env: IntegrationEnv,
  city: string,
): Promise<CdekCity[]> {
  const params = new URLSearchParams({ city, size: "20" });
  return cdekFetch<CdekCity[]>(env, `/location/cities?${params}`);
}

export async function getOffices(
  env: IntegrationEnv,
  cityCode: number,
): Promise<CdekOffice[]> {
  const params = new URLSearchParams({
    city_code: String(cityCode),
    type: "PVZ",
    is_handout: "true",
  });
  return cdekFetch<CdekOffice[]>(env, `/deliverypoints?${params}`);
}

export async function calculatePvzDelivery(
  env: IntegrationEnv,
  input: {
    cityCode: number;
    weightGrams: number;
    lengthCm: number;
    widthCm: number;
    heightCm: number;
  },
) {
  const result = await cdekFetch<{ tariff_codes: CdekTariff[] }>(
    env,
    "/calculator/tarifflist",
    {
      method: "POST",
      body: JSON.stringify({
        type: 1,
        currency: 1,
        from_location: {
          code: Number(requireSetting(env, "CDEK_FROM_CITY_CODE")),
        },
        to_location: { code: input.cityCode },
        packages: [
          {
            weight: Math.max(1, Math.round(input.weightGrams)),
            length: Math.max(1, Math.round(input.lengthCm)),
            width: Math.max(1, Math.round(input.widthCm)),
            height: Math.max(1, Math.round(input.heightCm)),
          },
        ],
      }),
    },
  );

  const tariffs = result.tariff_codes
    .filter((tariff) => tariff.delivery_sum > 0)
    .sort((a, b) => a.delivery_sum - b.delivery_sum);
  const preferred = tariffs.find((tariff) => tariff.delivery_mode === 3) ?? tariffs[0];
  if (!preferred) throw new Error("СДЭК не нашёл доступный тариф");

  return {
    tariffCode: preferred.tariff_code,
    tariffName: preferred.tariff_name,
    price: preferred.delivery_sum,
    daysMin: preferred.period_min,
    daysMax: preferred.period_max,
  };
}
