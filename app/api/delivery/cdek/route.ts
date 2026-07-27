import {
  calculatePvzDelivery,
  findCities,
  getOffices,
} from "../../../../lib/integrations/cdek";

const errorResponse = (error: unknown, status = 502) =>
  Response.json(
    { error: error instanceof Error ? error.message : "Ошибка сервиса СДЭК" },
    { status },
  );

export async function GET(request: Request) {
  try {
    const url = new URL(request.url);
    const action = url.searchParams.get("action");
    if (action === "cities") {
      const city = url.searchParams.get("city")?.trim() ?? "";
      if (city.length < 2) return errorResponse("Введите хотя бы 2 буквы", 400);
      return Response.json({ cities: await findCities(city) });
    }
    if (action === "offices") {
      const cityCode = Number(url.searchParams.get("cityCode"));
      if (!Number.isInteger(cityCode) || cityCode <= 0) {
        return errorResponse("Выберите город", 400);
      }
      const offices = await getOffices(cityCode);
      return Response.json({
        offices: offices.map((office) => ({
          code: office.code,
          name: office.name,
          location: office.location,
          work_time: office.work_time,
        })),
      });
    }
    return errorResponse("Неизвестная операция", 400);
  } catch (error) {
    return errorResponse(error);
  }
}

export async function POST(request: Request) {
  try {
    const body = (await request.json()) as {
      cityCode?: number;
      itemCount?: number;
    };
    const cityCode = Number(body.cityCode);
    if (!Number.isInteger(cityCode) || cityCode <= 0) {
      return errorResponse("Выберите город", 400);
    }
    return Response.json({
      quote: await calculatePvzDelivery(
        cityCode,
        Math.max(1, Math.min(10, Math.ceil(Number(body.itemCount) || 1))),
      ),
    });
  } catch (error) {
    return errorResponse(error);
  }
}
