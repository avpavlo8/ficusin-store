import { Page } from "@playwright/test";

// The preview server only serves the built files, so every API call has to
// be answered here. The shapes below mirror what the Go handlers return.
const product = {
  id: "saby-1",
  name: "Аглаонема Мария",
  latin: "Aglaonema",
  category: "Растения",
  price: 1490,
  image: "/assets/hero-monstera.png",
  light: "Рассеянный свет",
  size: "D12",
  stock: 5,
  catalogSection: "plants",
  lightLevel: "low_light",
  watering: "moderate",
  heightClass: "low",
  careLevel: "easy",
  placement: "bathroom",
  petSafety: "safe",
  categoryId: 4,
  rating: 4.8,
  reviewsCount: 12,
  // Значения приходят кодами — витрина обязана перевести их для покупателя.
  filterAttributes: [
    { code: "light_level", name: "Освещение", value: "low_light", badge: true, filterable: true },
    { code: "care_level", name: "Сложность ухода", value: "easy", badge: true, filterable: true },
  ],
};

// Второй вид, другая ветка дерева и другие атрибуты: иначе подборки и
// каталог нечем отличить друг от друга в проверках.
const ficus = {
  ...product,
  id: "saby-2",
  name: "Фикус Бенджамина",
  latin: "Ficus benjamina",
  price: 2490,
  stock: 3,
  lightLevel: "diffused",
  heightClass: "high",
  careLevel: "medium",
  placement: "office",
  petSafety: "caution",
  categoryId: 5,
  filterAttributes: [
    { code: "light_level", name: "Освещение", value: "diffused", badge: true, filterable: true },
    { code: "care_level", name: "Сложность ухода", value: "demanding", badge: true, filterable: true },
  ],
};

// Ноль на складе — это предзаказ, а не исчезнувшая карточка.
const monstera = {
  ...ficus,
  id: "saby-3",
  name: "Монстера Делициоза",
  latin: "Monstera deliciosa",
  price: 3200,
  stock: 0,
};

export const guest = { signedIn: false } as const;
export const owner = {
  signedIn: true,
  user: {
    id: 1,
    email: "owner@example.com",
    phone: "+79150000000",
    fullName: "Александр",
    lastName: "",
    patronymic: "",
    deliveryAddress: "",
    accountType: "retail",
    wholesaleStatus: "not_requested",
    retailDiscountBps: 0,
    // 6 400 ₽ выполненных заказов — первая ступень скидки ещё не взята,
    // до неё остаётся 3 600 ₽. Кабинет обязан назвать это число.
    lifetimeSpendMinor: 640_000,
  },
} as const;

type Session = typeof guest | typeof owner;
const carts = new WeakMap<Page, Record<string, number>>();

// Состав корзины из тела запроса.
//
// При неудачном разборе возвращаем null, а не пустой объект: стенд, молча
// стирающий корзину, врёт убедительнее любой поломки — на такой лжи здесь
// один раз уже потеряли день, разыскивая несуществующую ошибку в магазине.
function cartFromRequest(raw: string | null): Record<string, number> | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as { items?: Record<string, number> };
    return parsed.items ?? null;
  } catch {
    return null;
  }
}

export async function mockApi(page: Page, session: Session = guest) {
  // Моки держим на контексте, а не на странице: ссылки шапки и нижней
  // панели ведут на настоящий адрес, и браузер загружает новый документ.
  // Контекст у теста свой, так что охват не меняется.
  const context = page.context();

  // Playwright checks routes in reverse order of registration, so this
  // catch-all goes first: anything the page asks for that is not listed
  // below still gets an answer instead of hanging the test.
  await context.route("**/api/v1/**", (route) => route.fulfill({ json: {} }));

  await context.route("**/api/v1/catalog", (route) =>
    route.fulfill({ json: { products: [product, ficus, monstera] } }));

  if (!carts.has(page)) carts.set(page, {});
  await context.route("**/api/v1/cart", async (route) => {
    if (route.request().method() === "PUT") {
      const items = cartFromRequest(route.request().postData());
      // Тело не прочиталось — оставляем прежний состав.
      if (items) carts.set(page, items);
    }
    await route.fulfill({ json: { items: carts.get(page) || {} } });
  });

  await context.route("**/api/v1/products/*", (route) => route.fulfill({ json: { product: {
    ...product,
    shortDescription: "Неприхотливое растение",
    description: "Подходит для дома",
    careInstructions: "Поливать после просыхания грунта",
    images: [product.image],
    variants: [{ id: 1, sku: "X100", label: "D12", price: product.price, stock: product.stock, heightCm: 35, potDiameterCm: 12, wholesaleMinQty: 1 }],
    recommendations: [],
    importantWarnings: ["Безопасно для животных"],
    passport: { origin: "Тропические леса Азии", lighting: "Яркий рассеянный свет", watering: "После просыхания верхнего слоя", faq: [{ question: "Когда пересаживать?", answer: "Весной, когда корни заполнят горшок." }] },
    rating: 5,
    reviewsCount: 1,
    reviews: [{ id: 1, rating: 5, text: "Растение приехало здоровым и хорошо упакованным.", author: "Мария", date: "2026-08-01", verifiedPurchase: true, photos: [] }],
  } } }));

  // Дерево нарочно трёхуровневое и с пустым разделом — как в настоящей базе.
  // Пока стенд был плоским, витрина рисовала два уровня и никто этого не
  // замечал: виды растений просто не показывались.
  await context.route("**/api/v1/categories", (route) =>
    route.fulfill({ json: { categories: [
      { id: 1, parentId: null, name: "Растения", slug: "plants", sortOrder: 1 },
      { id: 2, parentId: null, name: "Кашпо и горшки", slug: "pots", sortOrder: 2 },
      { id: 3, parentId: 1, name: "Комнатные растения", slug: "indoor", sortOrder: 1 },
      { id: 4, parentId: 3, name: "Аглаонема", slug: "aglaonema", sortOrder: 1 },
      { id: 5, parentId: 3, name: "Фикус", slug: "ficus", sortOrder: 2 },
    ] } }));

  await context.route("**/api/v1/auth/me", (route) => session.signedIn
    ? route.fulfill({ json: { user: session.user } })
    : route.fulfill({ status: 401, json: { error: "Требуется авторизация" } }));

  await context.route("**/api/v1/account/**", (route) =>
    route.fulfill({ json: { orders: [] } }));

  await context.route("**/api/v1/payments/methods?delivery=*", (route) => {
    const delivery = new URL(route.request().url()).searchParams.get("delivery");
    const methods = delivery === "pickup"
      ? [{ id: "on_delivery", title: "При получении", note: "Оплатите, когда заберёте заказ" }]
      : [{ id: "manager_confirmation", title: "После подтверждения менеджером", note: "Оплата после подтверждения заказа менеджером" }];
    return route.fulfill({ json: { methods } });
  });

  await context.route("**/api/v1/delivery/cdek?action=status", (route) =>
    route.fulfill({ json: { available: false } }));
}

// Nothing may stick out sideways: a horizontal scrollbar on a phone is the
// classic sign of a fixed width that survived into the mobile layout.
export async function horizontalOverflow(page: Page) {
  return page.evaluate(() => {
    const doc = document.documentElement;
    return doc.scrollWidth - doc.clientWidth;
  });
}

export function setStoredCounts(page: Page, favorites: string[], cart: Record<string, number>) {
  carts.set(page, { ...cart });
  return page.addInitScript((f) => {
    localStorage.setItem("ficusin-favorites", JSON.stringify(f));
  }, favorites);
}
