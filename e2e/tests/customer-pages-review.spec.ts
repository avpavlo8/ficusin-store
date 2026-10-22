import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { mockApi, owner } from "./helpers";

const review = { id: 7, product: "Аглаонема Мария", slug: "1", rating: 5, text: "Растение приехало здоровым и хорошо упакованным.", status: "published", createdAt: "2026-10-20T10:00:00Z" };
const routes = ["/account", "/account/profile", "/account/favorites", "/account/reviews", "/login", "/register", "/delivery-and-returns", "/offer", "/privacy", "/contacts", "/requisites", "/favorites", "/discounts", "/care", "/care/mealybug", "/care/spider-mite", "/care/yellow-leaves", "/care/root-problems", "/unknown-page"];

for (const route of routes) {
  test(`@desktop @phone customer page ${route}: layout, title and accessibility`, async ({ page }, info) => {
    await mockApi(page, owner);
    await page.context().route("**/api/v1/account/reviews", route => route.fulfill({ json: { reviews: [review, { ...review, id: 8, status: "pending" }] } }));
    await page.context().route("**/api/v1/discount-plants", route => route.fulfill({ json: { items: [] } }));
    await page.context().route("**/api/v1/account/orders", route => route.fulfill({ json: { orders: [{ orderNumber: "DEMO-1024", total: 3980, status: "ready", createdAt: "2026-10-20T09:00:00Z", itemsCount: 2 }] } }));
    await page.goto(route);
    await expect(page.locator("h1")).toHaveCount(1);
    await expect(page.locator("h1")).toBeVisible();
    if (route.startsWith("/account")) await expect(page.locator(".account-content")).toBeVisible();
    await expect(page).not.toHaveTitle("Фикусин — комнатные растения в Рязани");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    const result = await new AxeBuilder({ page }).include("main").withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(result.violations.map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
    if (process.env.INFORMATION_REVIEW_OUTPUT && ["/delivery-and-returns", "/offer", "/privacy", "/contacts", "/requisites"].includes(route)) {
      await mkdir(process.env.INFORMATION_REVIEW_OUTPUT, { recursive: true });
      await page.screenshot({ path: `${process.env.INFORMATION_REVIEW_OUTPUT}/${info.project.name}-${route.slice(1)}.png`, fullPage: true });
      if (info.project.name === "desktop") await writeFile(`${process.env.INFORMATION_REVIEW_OUTPUT}/${route.slice(1)}.txt`, await page.locator(".legal-document").innerText());
      await expect(page.getByRole("navigation", { name: "Информация для покупателей" }).locator('a[aria-current="page"]')).toHaveCount(1);
    }
  });
}

test("@desktop @phone registration preserves every field after returning from call confirmation", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/auth/request-code", route => route.fulfill({ json: { checkId: "local-check", callPhonePretty: "+7 800 000-00-00" } }));
  await page.context().route("**/api/v1/auth/verify-code", route => route.fulfill({ status: 202, json: {} }));
  await page.goto("/register?returnTo=%2Faccount%2Fprofile");
  await page.getByLabel("Фамилия", { exact: true }).fill("Тестовая");
  await page.getByLabel("Имя", { exact: true }).fill("Анна");
  await page.getByLabel("Отчество", { exact: true }).fill("Ивановна");
  await page.getByLabel("Телефон", { exact: true }).fill("+7 900 000-00-00");
  await page.getByLabel("Email", { exact: true }).fill("test@example.com");
  await page.getByLabel("Адрес доставки", { exact: true }).fill("Рязань, Новосёлов, 40А");
  await page.getByRole("checkbox").check();
  await page.getByRole("button", { name: "Зарегистрироваться", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Подтвердите номер звонком" })).toBeVisible();
  await expect(page.locator('.auth-call-number a')).toHaveAttribute("href", "tel:+78000000000");
  await page.getByRole("button", { name: "Изменить данные" }).click();
  await expect(page.getByLabel("Фамилия", { exact: true })).toHaveValue("Тестовая");
  await expect(page.getByLabel("Имя", { exact: true })).toHaveValue("Анна");
  await expect(page.getByLabel("Отчество", { exact: true })).toHaveValue("Ивановна");
  await expect(page.getByLabel("Email", { exact: true })).toHaveValue("test@example.com");
  await expect(page.getByLabel("Адрес доставки", { exact: true })).toHaveValue("Рязань, Новосёлов, 40А");
  await expect(page.getByRole("checkbox")).toBeChecked();
  await expect(page.locator('.auth-switch a')).toHaveAttribute("href", /returnTo/);
});

test("@desktop @phone review failures stay distinct from empty list and can be retried", async ({ page }) => {
  await mockApi(page, owner);
  let failed = true;
  await page.context().route("**/api/v1/account/reviews", route => failed ? route.fulfill({ status: 500, json: { error: "Не удалось загрузить отзывы" } }) : route.fulfill({ json: { reviews: [{ ...review, status: "pending" }] } }));
  await page.goto("/account/reviews");
  await expect(page.getByRole("alert")).toContainText("Не удалось загрузить отзывы");
  await expect(page.getByText("Отзывов пока нет", { exact: true })).toHaveCount(0);
  failed = false;
  await page.getByRole("button", { name: "Повторить загрузку" }).click();
  await expect(page.locator(".account-reviews .admin-pill")).toHaveText("На проверке");
  await expect(page.getByRole("button", { name: "Редактировать", exact: true })).toHaveCount(0);
});

test("@desktop @phone review editor supports errors, keyboard focus and saving", async ({ page }) => {
  await mockApi(page, owner);
  await page.context().route("**/api/v1/account/reviews", route => route.fulfill({ json: { reviews: [review] } }));
  let failSave = true;
  await page.context().route("**/api/v1/account/reviews/7", route => failSave ? route.fulfill({ status: 500, json: { error: "Повторите сохранение" } }) : route.fulfill({ json: { ok: true } }));
  await page.goto("/account/reviews");
  await page.getByRole("button", { name: "Редактировать", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Изменить отзыв" });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("Текст отзыва", { exact: false }).fill("Новый текст отзыва, который должен сохраниться после повторной попытки.");
  await dialog.getByRole("button", { name: "Сохранить отзыв", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("Повторите сохранение");
  await expect(dialog.getByRole("textbox")).toHaveValue(/Новый текст/);
  const last = dialog.getByRole("button", { name: "Отмена", exact: true });
  await last.focus();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Закрыть редактор отзыва" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Редактировать", exact: true })).toBeFocused();
  await page.getByRole("button", { name: "Редактировать", exact: true }).click();
  await dialog.getByRole("textbox").fill("Сохранённый текст отзыва после повторной проверки.");
  failSave = false;
  await dialog.getByRole("button", { name: "Сохранить отзыв", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText("Отзыв сохранён");
  await expect(page.locator(".account-reviews")).toContainText("Сохранённый текст отзыва");
});

test("@desktop @phone profile keeps input after a failed save and sends the whole profile", async ({ page }) => {
  await mockApi(page, owner);
  let failed = true;
  let submitted: Record<string, unknown> = {};
  await page.context().route("**/api/v1/account/profile", route => {
    submitted = route.request().postDataJSON();
    return failed ? route.fulfill({ status: 500, json: { error: "Профиль не сохранён" } }) : route.fulfill({ json: { user: { ...owner.user, ...submitted } } });
  });
  await page.goto("/account/profile");
  await page.getByRole("button", { name: "Изменить", exact: true }).click();
  await page.getByLabel("Имя", { exact: true }).fill("Анна");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Профиль не сохранён");
  await expect(page.getByLabel("Имя", { exact: true })).toHaveValue("Анна");
  failed = false;
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Данные сохранены");
  expect(submitted).toEqual(expect.objectContaining({ fullName: "Анна", lastName: "", patronymic: "", email: owner.user.email, deliveryAddress: "" }));
});

test("@desktop @phone favorites are shared between catalog page and account", async ({ page }) => {
  await mockApi(page, owner);
  await page.addInitScript(() => { if (!localStorage.getItem("ficusin-favorites")) localStorage.setItem("ficusin-favorites", '["1","2"]'); });
  await page.goto("/account/favorites");
  await expect(page.locator(".customer-favorites-grid .storefront-card")).toHaveCount(2);
  await page.locator(".customer-favorites-grid .storefront-card").first().getByRole("button", { name: /из избранного/ }).click();
  await page.locator('a[href="/favorites"]:visible').first().click();
  await expect(page.locator(".favorites-page .storefront-card")).toHaveCount(1);
});

test("@desktop @phone contacts show an embedded map and a working route link", async ({ page }) => {
  await mockApi(page);
  await page.goto("/contacts");
  await expect(page.locator(".contacts-map")).toBeVisible();
  await expect(page.locator(".contacts-route")).toHaveAttribute("href", /yandex.ru\/maps/);
  await expect(page.getByTitle("Магазин Фикусин на карте")).toHaveAttribute("src", /map-widget\/v1\/\?oid=121536326951&ol=biz/);
});

test("@desktop @phone discount page can recover from a network error", async ({ page }) => {
  await mockApi(page);
  let fail = true;
  await page.context().route("**/api/v1/discount-plants", route => fail ? route.fulfill({ status: 503, json: {} }) : route.fulfill({ json: { items: [] } }));
  await page.goto("/discounts");
  await expect(page.getByRole("alert")).toContainText("Не удалось загрузить растения");
  fail = false;
  await page.getByRole("button", { name: "Повторить загрузку" }).click();
  await expect(page.getByRole("heading", { name: "Сейчас таких растений нет" })).toBeVisible();
});

test("@desktop @phone legal contents navigate to visible sections", async ({ page }) => {
  await mockApi(page);
  await page.goto("/offer");
  const toc = page.getByRole("navigation", { name: "На этой странице" });
  await expect(toc).toBeVisible();
  const toggle = toc.getByRole("button", { name: "Содержание" });
  if (await toggle.isVisible()) await toggle.click();
  await expect(toc.getByRole("link")).toHaveCount(7);
  await toc.getByRole("link").last().click();
  await expect(page).toHaveURL(/#document-section-7$/);
  await expect(page.locator("#document-section-7 h2")).toBeInViewport();
});

test("@desktop customer pages remain usable at 320px", async ({ page }) => {
  await mockApi(page, owner);
  await page.context().route("**/api/v1/account/reviews", route => route.fulfill({ json: { reviews: [review] } }));
  await page.context().route("**/api/v1/discount-plants", route => route.fulfill({ json: { items: [] } }));
  await page.setViewportSize({ width: 320, height: 740 });
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator("h1")).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), route).toBeTruthy();
  }
});


test("@desktop @phone wholesale registration retains company details when buyer type changes", async ({ page }) => {
  await mockApi(page);
  await page.goto("/register");
  await page.getByRole("radio", { name: /Оптовый/ }).check();
  await page.route("**/api/v1/organizations/suggest?*", route => route.fulfill({ json: { available: true, suggestions: [{ name: "Тестовая организация", inn: "1234567890", kpp: "", legalAddress: "" }] } }));
  await page.getByRole("combobox", { name: "Название организации или ИНН", exact: true }).fill("Тестовая");
  await page.getByRole("option", { name: /Тестовая организация/ }).click();
  await page.getByRole("radio", { name: /Розничный/ }).check();
  await page.getByRole("radio", { name: /Оптовый/ }).check();
  await expect(page.getByRole("combobox", { name: "Название организации или ИНН", exact: true })).toHaveValue("Тестовая организация");
  await expect(page.getByLabel("ИНН", { exact: true })).toHaveValue("1234567890");
});

test("@desktop @phone avatar upload rejects unsupported files before sending a request", async ({ page }) => {
  await mockApi(page, owner);
  let requested = false;
  await page.context().route("**/api/v1/account/avatar", route => { requested = true; return route.fulfill({ json: { user: owner.user } }); });
  await page.goto("/account/profile");
  await page.locator('#avatar-input').setInputFiles({ name: "invalid.txt", mimeType: "text/plain", buffer: Buffer.from("not an image") });
  await expect(page.getByRole("alert")).toContainText("Выберите фото JPEG, PNG или WebP");
  expect(requested).toBeFalsy();
});

test("@desktop @phone account navigation keeps every label inside its link across breakpoints", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/account");
  const nav = page.getByRole("navigation", { name: "Разделы личного кабинета" });
  await expect(nav.getByRole("link")).toHaveCount(5);
  for (const width of [320, 393, 560, 561, 648, 768, 900, 901, 1100, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const layout = await nav.getByRole("link").evaluateAll(links => links.map(link => {
      const box = link.getBoundingClientRect();
      const inside = (r: DOMRect) => r.left >= box.left - 1 && r.right <= box.right + 1 && r.top >= box.top - 1 && r.bottom <= box.bottom + 1;
      const content = [...link.querySelectorAll("strong,small")].flatMap(label => {
        const range = document.createRange();
        range.selectNodeContents(label);
        return [...range.getClientRects()];
      });
      const icon = link.querySelector(".cozy-nav-icon")!.getBoundingClientRect();
      return { label: link.textContent, contained: content.every(inside) && inside(icon), box: { left: box.left, top: box.top, right: box.right, bottom: box.bottom } };
    }));
    expect(layout.filter(item => !item.contained), `Content outside a navigation link at ${width}px`).toEqual([]);
    for (let i = 0; i < layout.length; i++) for (let j = i + 1; j < layout.length; j++) {
      const a = layout[i].box, b = layout[j].box;
      expect(a.right <= b.left + 1 || b.right <= a.left + 1 || a.bottom <= b.top + 1 || b.bottom <= a.top + 1, `Overlapping navigation links at ${width}px`).toBeTruthy();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Page overflow at ${width}px`).toBeTruthy();
  }
});

test("@desktop @phone profile has a compact photo control, loyalty meter and a clean menu", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/account/profile");
  const button = page.getByRole("button", { name: "Загрузить фото", exact: true });
  await expect(button).toBeVisible();
  const box = await button.boundingBox();
  expect(box!.width).toBeLessThan(180);
  expect(box!.height).toBeGreaterThanOrEqual(44);
  expect(box!.height).toBeLessThanOrEqual(48);
  await expect(page.getByText("Розничный клиент", { exact: true })).toHaveCount(0);
  await expect(page.getByText("JPEG, PNG или WebP. Обрежем по центру до квадрата.")).toHaveCount(0);
  const nav = page.getByRole("complementary", { name: "Ваш личный кабинет" });
  await expect(nav.getByRole("link", { name: /Сообщения/ })).toHaveAttribute("href", "/account/chat");
  await expect(nav.getByRole("link", { name: "Подсказки по уходу" })).toHaveAttribute("href", "/care");
  await expect(nav.getByText(/Ваш профиль|Пусть растёт|Мы рядом/)).toHaveCount(0);
  const meter = page.getByRole("meter", { name: "Персональная скидка" });
  await expect(meter).toHaveAttribute("aria-valuenow", "64");
  await expect(meter).toHaveAttribute("aria-valuemax", "100");
  const picker = page.waitForEvent("filechooser");
  await button.click();
  await (await picker).setFiles({ name: "avatar.txt", mimeType: "text/plain", buffer: Buffer.from("invalid photo") });
  await expect(page.getByRole("alert")).toContainText("Выберите фото JPEG, PNG или WebP");
  await expect(button).toBeEnabled();
});

test("@desktop @phone loyalty meter preserves manual discounts, the maximum and wholesale states", async ({ page }) => {
  await mockApi(page, owner);
  let user = { ...owner.user, retailDiscountBps: 700, lifetimeSpendMinor: 200000 };
  await page.context().route("**/api/v1/auth/me", route => route.fulfill({ json: { user } }));
  await page.goto("/account/profile");
  await expect(page.locator(".loyalty-value")).toHaveText("7%");
  await expect(page.getByRole("meter")).toHaveCount(0);
  await expect(page.getByText(/Скидка назначена вручную/)).toBeVisible();
  user = { ...user, retailDiscountBps: 1000, lifetimeSpendMinor: 10000000 };
  await page.reload();
  await expect(page.locator(".loyalty-value")).toHaveText("10%");
  await expect(page.getByRole("meter")).toHaveCount(0);
  await expect(page.getByText(/верхней ступени/)).toBeVisible();
  for (const [status, label] of [["approved", "Оптовые условия одобрены"], ["pending", "Оптовая заявка на проверке"], ["rejected", "Оптовая заявка отклонена"]]) {
    await page.context().route("**/api/v1/auth/me", route => route.fulfill({ json: { user: { ...user, accountType: "wholesale", wholesaleStatus: status } } }));
    await page.reload();
    await expect(page.getByText("Оптовый клиент", { exact: true })).toBeVisible();
    await expect(page.getByText(label, { exact: true })).toBeVisible();
    await expect(page.getByRole("meter")).toHaveCount(0);
  }
});


test("@desktop @phone loyalty shows progress between the current and next discount", async ({ page }) => {
  await mockApi(page, owner);
  let spend = 1240000;
  await page.context().route("**/api/v1/auth/me", route => route.fulfill({ json: { user: { ...owner.user, retailDiscountBps: 100, lifetimeSpendMinor: spend } } }));
  await page.goto("/account/profile");
  const progress = page.locator(".loyalty-progress");
  await expect(progress.locator(":scope > span")).toHaveText(["1%", "2%"]);
  await expect(page.getByRole("meter")).toHaveAttribute("aria-valuenow", "24");
  await expect(page.locator(".loyalty-next")).toContainText("7 600");
  const info = page.getByRole("button", { name: "Условия персональной скидки" });
  await expect(page.locator("#loyalty-rules")).toBeHidden();
  await info.click();
  await expect(info).toHaveAttribute("aria-expanded", "true");
  await expect(page.locator("#loyalty-rules tbody tr")).toHaveCount(10);
  await expect(page.locator("#loyalty-rules tbody tr").first()).toContainText("10 000");
  await expect(page.locator("#loyalty-rules tbody tr").last()).toContainText("100 000");
  expect((await new AxeBuilder({ page }).include(".loyalty-card").withTags(["wcag2a", "wcag2aa"]).analyze()).violations).toEqual([]);
  await info.press("Enter");
  await expect(page.locator("#loyalty-rules")).toBeHidden();
  spend = 1800000;
  await page.reload();
  await expect(page.getByRole("meter")).toHaveAttribute("aria-valuenow", "80");
  await expect(page.locator(".loyalty-next")).toContainText("2 000");
});


test("@desktop @phone expired sign-in returns to the phone without an unnecessary retry action", async ({ page }) => {
  await mockApi(page);
  let expired = false;
  let requests = 0;
  await page.route("**/api/v1/auth/request-code", route => { requests++; return route.fulfill({ json: { checkId: `local-${requests}`, callPhonePretty: "+7 800 000-00-00" } }); });
  await page.route("**/api/v1/auth/verify-code", route => expired
    ? route.fulfill({ status: 400, json: { error: "Срок проверки истёк. Начните вход заново." } })
    : route.fulfill({ status: 202, json: {} }));
  await page.goto("/login");
  await page.getByLabel("Телефон", { exact: true }).fill("9000000000");
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Подтвердите номер звонком" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Повторить|новый номер/ })).toHaveCount(0);
  expired = true;
  await expect(page.getByRole("alert")).toContainText("Срок проверки истёк");
  await expect(page.getByLabel("Телефон", { exact: true })).toHaveValue("+7 900 000-00-00");
  expired = false;
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Подтвердите номер звонком" })).toBeVisible();
  expect(requests).toBe(2);
});
