import { getCdekCredentials } from "./credentials";

type CdekToken = { access_token: string; expires_in: number };

export type CdekCity = {
  code: number;
  city: string;
  region?: string;
  country?: string;
  country_code?: string;
};

export type CdekOffice = {
  code: string;
  name: string;
  type?: string;
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

const BASE_URL = "https://api.cdek.ru/v2";
const FROM_CITY_CODE = 159;
let tokenCache: { token: string; expiresAt: number } | null = null;

async function getToken(): Promise<string> {
  if (tokenCache && tokenCache.expiresAt > Date.now()) return tokenCache.token;
  const credentials = await getCdekCredentials();
  const body = new URLSearchParams({
    grant_type: "client_credentials",
    client_id: credentials.clientId,
    client_secret: credentials.clientSecret,
  });
  const response = await fetch(`${BASE_URL}/oauth/token`, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "User-Agent": "Ficusin-Store/1.0",
    },
    body,
  });
  if (!response.ok) throw new Error(`СДЭК не выдал токен: ${response.status}`);
  const data = (await response.json()) as CdekToken;
  tokenCache = {
    token: data.access_token,
    expiresAt: Date.now() + Math.max(60, data.expires_in - 120) * 1000,
  };
  return data.access_token;
}

async function cdekFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${await getToken()}`,
      Accept: "application/json",
      "Content-Type": "application/json",
      "User-Agent": "Ficusin-Store/1.0",
      ...init?.headers,
    },
  });
  if (!response.ok) {
    throw new Error(`СДЭК временно недоступен (${response.status})`);
  }
  return (await response.json()) as T;
}

export async function findCities(city: string): Promise<CdekCity[]> {
  const params = new URLSearchParams({
    city,
    country_codes: "RU",
    size: "20",
  });
  const cities = await cdekFetch<CdekCity[]>(`/location/cities?${params}`);
  return cities.filter((item) => item.country_code === "RU" || !item.country_code);
}

export async function getOffices(cityCode: number): Promise<CdekOffice[]> {
  const params = new URLSearchParams({
    city_code: String(cityCode),
    type: "PVZ",
    is_handout: "true",
  });
  const offices = await cdekFetch<CdekOffice[]>(`/deliverypoints?${params}`);
  return offices
    .filter((office) => office.location?.address)
    .sort((a, b) => a.location.address.localeCompare(b.location.address, "ru"));
}

export async function calculatePvzDelivery(cityCode: number, itemCount: number) {
  const packages = Array.from(
    { length: Math.max(1, Math.min(10, Math.ceil(itemCount))) },
    () => ({ weight: 2500, length: 35, width: 35, height: 60 }),
  );
  const result = await cdekFetch<{ tariff_codes: CdekTariff[] }>(
    "/calculator/tarifflist",
    {
      method: "POST",
      body: JSON.stringify({
        type: 1,
        currency: 1,
        from_location: { code: FROM_CITY_CODE },
        to_location: { code: cityCode },
        packages,
      }),
    },
  );
  const tariffs = result.tariff_codes
    .filter(
      (tariff) =>
        tariff.delivery_sum > 0 &&
        (tariff.delivery_mode === 2 || tariff.delivery_mode === 4),
    )
    .sort((a, b) => a.delivery_sum - b.delivery_sum);
  const preferred = tariffs[0];
  if (!preferred) throw new Error("СДЭК не нашёл доставку до пункта выдачи");
  return {
    tariffCode: preferred.tariff_code,
    tariffName: preferred.tariff_name,
    price: Math.ceil(preferred.delivery_sum),
    daysMin: preferred.period_min,
    daysMax: preferred.period_max,
  };
}
