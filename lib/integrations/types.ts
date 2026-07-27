export type IntegrationEnv = {
  SITE_URL?: string;
  TELEGRAM_BOT_TOKEN?: string;
  TELEGRAM_ORDER_CHAT_ID?: string;
  YOOKASSA_SHOP_ID?: string;
  YOOKASSA_SECRET_KEY?: string;
  YOOKASSA_VAT_CODE?: string;
  CDEK_CLIENT_ID?: string;
  CDEK_CLIENT_SECRET?: string;
  CDEK_API_URL?: string;
  CDEK_FROM_CITY_CODE?: string;
  SABY_ACCESS_TOKEN?: string;
  SABY_POINT_ID?: string;
  SABY_PRICE_LIST_ID?: string;
  SABY_WAREHOUSE_ID?: string;
  SABY_SYNC_SECRET?: string;
};

export function requireSetting(
  env: IntegrationEnv,
  key: keyof IntegrationEnv,
): string {
  const value = env[key]?.trim();
  if (!value) {
    throw new Error(`Не задана настройка ${key}`);
  }
  return value;
}
