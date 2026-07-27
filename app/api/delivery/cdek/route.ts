import {
  calculatePvzDelivery,
  findCities,
  getOffices,
  isCdekConfigured,
} from "../../../../lib/integrations/cdek";
import type { IntegrationEnv } from "../../../../lib/integrations/types";

const jsonError = (message: string, status = 400) =>
  Response.json({ error: message }, { status });

async function getIntegrationEnv() {
  const { env } = await import("cloudflare:workers");
  return env as unknown as IntegrationEnv;
}

export async function GET(request: Request) {
  try {
    const env = await getIntegrationEnv();
    if (!isCdekConfigured(env)) {
      return jsonError("СДЭК ещё не настроен", 503);
    }
    const url = new URL(request.url);
    const action = url.searchParams.get("action");
    if (action === "cities") {
      const city = url.searchParams.get("city")?.trim();
      if (!city || city.length < 2) return jsonError("Укажите город");
      return Response.json({ cities: await findCities(env, city) });
    }
    if (action === "offices") {
      const cityCode = Number(url.searchParams.get("cityCode"));
      if (!Number.isInteger(cityCode) || cityCode <= 0) {
        return jsonError("Некорректный код города");
      }
      return Response.json({ offices: await getOffices(env, cityCode) });
    }
    return jsonError("Неизвестная операция");
  } catch (error) {
    return jsonError(
      error instanceof Error ? error.message : "Ошибка сервиса СДЭК",
      502,
    );
  }
}

export async function POST(request: Request) {
  try {
    const env = await getIntegrationEnv();
    if (!isCdekConfigured(env)) {
      return jsonError("СДЭК ещё не настроен", 503);
    }
    const body = (await request.json()) as {
      cityCode?: number;
      package?: {
        weightGrams?: number;
        lengthCm?: number;
        widthCm?: number;
        heightCm?: number;
      };
    };
    if (!Number.isInteger(body.cityCode) || Number(body.cityCode) <= 0) {
      return jsonError("Выберите город");
    }
    const parcel = body.package ?? {};
    const quote = await calculatePvzDelivery(env, {
      cityCode: Number(body.cityCode),
      weightGrams: Math.min(30000, Math.max(100, parcel.weightGrams ?? 2000)),
      lengthCm: Math.min(150, Math.max(5, parcel.lengthCm ?? 30)),
      widthCm: Math.min(150, Math.max(5, parcel.widthCm ?? 30)),
      heightCm: Math.min(200, Math.max(5, parcel.heightCm ?? 60)),
    });
    return Response.json({ quote });
  } catch (error) {
    return jsonError(
      error instanceof Error ? error.message : "Не удалось рассчитать доставку",
      502,
    );
  }
}
