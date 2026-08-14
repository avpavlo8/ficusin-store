import { Page } from "@playwright/test";

// The preview server only serves the built files, so every API call has to
// be answered here. The shapes below mirror what the Go handlers return.
const product = {
  id: "saby-1",
  name: "Аглаонема Мария",
  latin: "Aglaonema",
  category: "Растения",
  price: 1490,
  image: "/assets/product-pothos.png",
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
  },
} as const;

type Session = typeof guest | typeof owner;

export async function mockApi(page: Page, session: Session = guest) {
  // Playwright checks routes in reverse order of registration, so this
  // catch-all goes first: anything the page asks for that is not listed
  // below still gets an answer instead of hanging the test.
  await page.route("**/api/v1/**", (route) => route.fulfill({ json: {} }));

  await page.route("**/api/v1/catalog", (route) =>
    route.fulfill({ json: { products: [product, ficus, monstera] } }));

  await page.route("**/api/v1/products/*", (route) => route.fulfill({ json: { product: {
    ...product,
    shortDescription: "Неприхотливое растение",
    description: "Подходит для дома",
    careInstructions: "Поливать после просыхания грунта",
    images: [product.image],
    variants: [{ id: 1, sku: "X100", label: "D12", price: product.price, stock: product.stock, heightCm: 35, potDiameterCm: 12, wholesaleMinQty: 1 }],
    recommendations: [],
  } } }));

  // Дерево нарочно трёхуровневое и с пустым разделом — как в настоящей базе.
  // Пока стенд был плоским, витрина рисовала два уровня и никто этого не
  // замечал: виды растений просто не показывались.
  await page.route("**/api/v1/categories", (route) =>
    route.fulfill({ json: { categories: [
      { id: 1, parentId: null, name: "Растения", slug: "plants", sortOrder: 1 },
      { id: 2, parentId: null, name: "Кашпо и горшки", slug: "pots", sortOrder: 2 },
      { id: 3, parentId: 1, name: "Комнатные растения", slug: "indoor", sortOrder: 1 },
      { id: 4, parentId: 3, name: "Аглаонема", slug: "aglaonema", sortOrder: 1 },
      { id: 5, parentId: 3, name: "Фикус", slug: "ficus", sortOrder: 2 },
    ] } }));

  await page.route("**/api/v1/auth/me", (route) => session.signedIn
    ? route.fulfill({ json: { user: session.user } })
    : route.fulfill({ status: 401, json: { error: "Требуется авторизация" } }));

  await page.route("**/api/v1/account/**", (route) =>
    route.fulfill({ json: { orders: [] } }));
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
  return page.addInitScript(([f, c]) => {
    localStorage.setItem("ficusin-favorites", JSON.stringify(f));
    localStorage.setItem("ficusin-cart", JSON.stringify(c));
  }, [favorites, cart] as const);
}
