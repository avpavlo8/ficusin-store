import { resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";
import AxeBuilder from "@axe-core/playwright";
import type { ReturnIntake, ReturnIntakeData } from "../../frontend/src/adminTypes";

const products = [{ variantId: 101, name: "Фикус Бенджамина", sku: "FIC-D12", label: "D12", price: 1000 }, { variantId: 102, name: "Монстера Делициоза", sku: "MON-D17", label: "D17", price: 2500 }];

async function setup(page: Page, initial: ReturnIntake | null = null) {
  await mockApi(page, owner);
  await page.route("**/api/v1/admin/dashboard", route => route.fulfill({ json: { user: { fullName: "Александр" }, role: "owner", permissions: ["dashboard.read", "returns.read", "returns.edit", "returns.receipt.create"], dashboard: { products: 2, variants: 2, orders: 0, customers: 1, wholesalePending: 0, lastSync: null, recentOrders: [] } } }));

  await page.route("**/api/v1/admin/returns/products", route => route.fulfill({ json: { items: products } }));
  let intake: ReturnIntake | null = initial;
  let failSave = false;
  let delaySave = 0;
  let loseSaveResponse = false;
  let receiptCalls = 0;
  const writes: ReturnIntakeData[] = [];
  await page.route("**/api/v1/admin/returns", route => route.fulfill({ json: { items: intake?.items || [] } }));
  await page.route("**/api/v1/admin/returns/documents", route => route.fulfill({ json: { items: intake?.status === "saved" ? [intake] : [] } }));
  await page.route("**/api/v1/admin/returns/intake-draft", route => route.fulfill({ json: { intake: intake?.status === "draft" ? intake : null } }));
  await page.route("**/api/v1/admin/returns/intakes/**", async route => {
    const request = route.request();
    if (request.method() === "PUT") {
      if (failSave) return route.fulfill({ status: 503, json: { error: "Не удалось сохранить черновик. Попробуйте снова." } });
      const input = request.postDataJSON();
      writes.push(input.data);
      if (delaySave) await new Promise(resolve => setTimeout(resolve, delaySave));
      intake = { id: new URL(request.url()).pathname.split("/").at(-1)!, revision: input.revision + 1, data: {...input.data,rows:input.data.rows.map((row: ReturnIntakeData["rows"][number])=>({discountPercent:0,...row}))}, status: "draft", receiptStatus: "none", items: [] };
      if (loseSaveResponse) { loseSaveResponse = false; return route.fulfill({ status: 502, json: { error: "Ответ сохранения потерян" } }); }
    } else if (request.method() === "POST" && intake) {
      if (request.url().endsWith("/receipt")) receiptCalls++;
      intake.status = "saved"; intake.documentNumber = 1; intake.createdAt = "2026-10-14T12:00:00Z";
      intake.receiptStatus = request.url().endsWith("/receipt") ? "queued" : "none";
      intake.items = intake.data.rows.flatMap((row, rowIndex) => Array.from({ length: row.quantity }, (_, index) => ({ id: 10001 + rowIndex * 100 + index, channel: intake!.data.channel, sourceReturnId: "test", sourceShipmentId: "", sourceUnitIndex: index + 1, variantId: row.variantId, productName: row.name, sku: row.sku, returnedAt: "2026-10-14", condition: row.condition, comment: row.comment, costOutcome: row.condition === "ready" ? "restored" as const : row.condition === "dead" ? "lost" as const : "unknown" as const, financialStatus: "incomplete" as const, receiptStatus: row.condition === "ready" ? intake!.receiptStatus : "none" as const, photoIds: [], history: [] })));
    }
    await route.fulfill({ json: { intake } });
  });
  await page.goto("/admin?section=returns");
  await page.getByRole("button", { name: "+ Принять возврат", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Название растения 1", exact: true })).toBeVisible();
  return { writes, getIntake: () => intake, setFail: (value: boolean) => { failSave = value; }, setDelay: (value: number) => { delaySave = value; }, loseResponse: () => { loseSaveResponse = true; }, receiptCalls: () => receiptCalls };
}

test("@desktop @phone returns table opens whole catalogue, matches word fragments and restores autosaved rows", async ({ page }) => {
  const state = await setup(page);
  const input = page.getByRole("combobox", { name: "Название растения 1" });
  await input.click();
  await expect(page.getByRole("listbox", { name: "Каталог растений" }).getByRole("option")).toHaveCount(2);
  await input.fill("ндж");
  await expect(page.getByRole("listbox", { name: "Каталог растений" }).getByRole("option")).toHaveCount(1);
  await page.getByRole("option", { name: /Фикус Бенджамина/ }).click();
  await page.getByRole("textbox", { name: "Количество 1" }).fill("3");
  await page.getByRole("combobox", { name: "Состояние 1" }).selectOption("restoring");
  await page.getByRole("textbox", { name: "Комментарий 1", exact: true }).fill("Повреждён лист");
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  expect(state.getIntake()?.data.rows[0]).toMatchObject({ variantId: 101, quantity: 3, condition: "restoring", comment: "Повреждён лист" });
  await page.getByRole("dialog").getByRole("button", { name: "Закрыть", exact: true }).click();
  await page.getByRole("button", { name: "+ Принять возврат", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Количество 1" })).toHaveValue("3");
  await expect(page.getByRole("textbox", { name: "Комментарий 1", exact: true })).toHaveValue("Повреждён лист");
  await expect(page.getByRole("combobox", { name: "Состояние 1" })).toHaveValue("restoring");
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone other channel shows comment and receipt saves multiple plants with their conditions", async ({ page }) => {
  const state = await setup(page);
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox", { name: "Канал", exact: true }).selectOption("other");
  await dialog.getByRole("textbox", { name: "Комментарий к каналу" }).fill("Возврат из розницы");
  await dialog.getByRole("combobox", { name: "Название растения 1" }).fill("фи");
  await page.getByRole("option", { name: /Фикус/ }).click();
  await dialog.getByRole("textbox", { name: "Количество 1" }).fill("2");
  await dialog.getByRole("button", { name: "Добавить растение" }).click();
  await dialog.getByRole("combobox", { name: "Название растения 2" }).fill("нсте");
  await page.getByRole("option", { name: /Монстера/ }).click();
  await dialog.getByRole("combobox", { name: "Состояние 2" }).selectOption("dead");
  await dialog.getByRole("button", { name: "Создать поступление" }).click();
  await expect(dialog.getByText("Создаём поступление в СБИС…", { exact: true })).toBeVisible();
  expect(state.receiptCalls()).toBe(1);
  expect(state.getIntake()?.data.rows.map(row => [row.variantId, row.quantity, row.condition])).toEqual([[101, 2, "ready"], [102, 1, "dead"]]);
  await expect(dialog.getByRole("button", { name: "Создать поступление" })).toHaveCount(0);
  await dialog.getByRole("button", { name: "Закрыть документ", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(state.receiptCalls()).toBe(1);
  await expect(page.getByRole("button", { name: /Документ приёмки №1/ })).toBeVisible();
});

test("@desktop @phone autosave failure keeps edited values, shows retry and prevents losing them on close", async ({ page }) => {
  const state = await setup(page);
  state.setFail(true);
  await page.getByRole("textbox", { name: "Комментарий 1", exact: true }).fill("Не потерять комментарий");
  await expect(page.getByText("Не сохранено", { exact: true })).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Комментарий 1", exact: true })).toHaveValue("Не потерять комментарий");
  state.setFail(false);
  await page.getByRole("button", { name: "Повторить сохранение" }).click();
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  expect(state.getIntake()?.data.rows[0].comment).toBe("Не потерять комментарий");
});

test("@desktop slow autosave response cannot overwrite a newer edit", async ({ page }) => {
  const state = await setup(page);
  state.setDelay(1100);
  const comment = page.getByRole("textbox", { name: "Комментарий 1", exact: true });
  await comment.fill("Первый текст");
  await expect.poll(() => state.writes.length).toBe(1);
  await comment.fill("Новый текст во время сохранения");
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  await expect(comment).toHaveValue("Новый текст во время сохранения");
  expect(state.getIntake()?.data.rows[0].comment).toBe("Новый текст во время сохранения");
});

test("@desktop return dialog supports keyboard selection and accessible controls", async ({ page }) => {
  await setup(page);
  const input = page.getByRole("combobox", { name: "Название растения 1" });
  await input.fill("Фикус");
  await input.press("ArrowDown");
  await input.press("Enter");
  await expect(input).toHaveValue("Фикус Бенджамина · D12");
  await page.getByRole("textbox", { name: "Количество 1" }).focus();
  const result = await new AxeBuilder({ page }).include('[role="dialog"]').withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze();
  expect(result.violations.filter(item => ["serious", "critical"].includes(item.impact || ""))).toEqual([]);
});

test("@desktop a lost autosave response is verified before saving further edits", async ({ page }) => {
  const state = await setup(page);
  state.loseResponse();
  const comment = page.getByRole("textbox", { name: "Комментарий 1", exact: true });
  await comment.fill("Ответ может потеряться");
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  expect(state.getIntake()?.revision).toBe(1);
  await comment.fill("Правка после потерянного ответа");
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  expect(state.getIntake()?.revision).toBe(2);
  expect(state.getIntake()?.data.rows[0].comment).toBe("Правка после потерянного ответа");
});


test("@desktop other channel can be saved with an optional comment", async ({ page }) => {
  await setup(page);
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox", { name: "Канал", exact: true }).selectOption("other");
  await expect(dialog.getByRole("textbox", { name: "Комментарий к каналу" })).toBeVisible();
  await dialog.getByRole("combobox", { name: "Название растения 1" }).fill("Фикус");
  await page.getByRole("option", { name: /Фикус/ }).click();
  await dialog.getByRole("button", { name: "Сохранить возврат", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Документ приёмки №1/ })).toBeVisible();
});

test("@desktop saved document opens again and replaces the misleading Saby row counter with the receipt link", async ({page})=>{
 const state=await setup(page);await page.getByRole("combobox",{name:"Название растения 1"}).fill("фи");await page.getByRole("option",{name:/Фикус/}).click();await page.getByRole("dialog").getByRole("button",{name:"Создать поступление",exact:true}).click();
 const intake=state.getIntake()!;intake.receiptStatus="failed";intake.receiptError="Saby сохранил 0 товарных строк из 1";intake.receiptExternalUrl="https://ret.saby.ru/opendoc.html?guid=test";
 const dialog=page.getByRole("dialog");await expect(dialog.getByRole("link",{name:"Открыть поступление ↗"})).toBeVisible();await expect(dialog.getByText(/0 товарных строк/)).toHaveCount(0);await dialog.getByRole("button",{name:"Закрыть документ",exact:true}).click();await page.getByRole("button",{name:/Документ приёмки №1/}).click();await expect(page.getByRole("dialog").getByRole("heading",{name:"Состав документа",exact:true})).toBeVisible();await expect(page.getByRole("dialog").getByRole("spinbutton")).toHaveCount(0);expect(state.receiptCalls()).toBe(1);
});

test("@desktop label download failure keeps the saved document open with a retryable error", async ({page})=>{
 const state=await setup(page);await page.route("**/api/v1/admin/returns/labels",route=>route.fulfill({status:503,json:{error:"Не удалось скачать этикетки. Попробуйте ещё раз."}}));await page.getByRole("combobox",{name:"Название растения 1"}).fill("фи");await page.getByRole("option",{name:/Фикус/}).click();await page.getByRole("combobox",{name:"Состояние 1"}).selectOption("restoring");await page.getByRole("dialog").getByRole("button",{name:"Сохранить и скачать этикетки",exact:true}).click();await expect(page.getByRole("dialog")).toBeVisible();await expect(page.getByRole("dialog").getByRole("alert")).toContainText("Не удалось скачать этикетки");expect(state.getIntake()?.status).toBe("saved");await expect(page.getByRole("dialog").getByRole("button",{name:"Скачать этикетки",exact:true})).toBeEnabled();
});


test("@desktop @phone invalid intake focuses the first missing plant or invalid quantity", async ({page}) => {
  await setup(page);
  const input=page.getByRole("combobox",{name:"Название растения 1",exact:true});
  await page.getByRole("button",{name:"Сохранить возврат",exact:true}).click();
  await expect(input).toBeFocused();
  await expect(page.getByText("Выберите растение из каталога",{exact:true})).toBeVisible();
  await input.fill("фикус");
  await page.getByRole("option",{name:/Фикус Бенджамина/}).click();
  const quantity=page.getByRole("textbox",{name:"Количество 1",exact:true});
  await quantity.fill("0");
  await page.getByRole("button",{name:"Сохранить возврат",exact:true}).click();
  await expect(quantity).toBeFocused();
  await expect(quantity).toHaveAttribute("aria-invalid","true");
});

test("@desktop @phone text numbers can be cleared without spinners or silent clamping", async ({ page }) => {
  const state = await setup(page);
  await page.getByRole("combobox", { name: "Название растения 1" }).fill("фикус");
  await page.getByRole("option", { name: /Фикус Бенджамина/ }).click();
  const quantity = page.getByRole("textbox", { name: "Количество 1", exact: true });
  await expect(quantity).toHaveAttribute("type", "text");
  await expect(quantity).toHaveAttribute("inputmode", "numeric");
  await quantity.fill(""); await expect(quantity).toHaveValue("");
  await quantity.pressSequentially("12"); await quantity.press("ArrowUp");
  await expect(quantity).toHaveValue("12");
  await page.getByRole("combobox", { name: "Состояние 1" }).selectOption("restoring");
  const discount = page.getByRole("textbox", { name: "Скидка 1", exact: true });
  await discount.fill(""); await expect(discount).toHaveValue("");
  await discount.pressSequentially("101"); await expect(discount).toHaveValue("101");
  await page.getByRole("button", { name: "Сохранить возврат", exact: true }).click();
  await expect(discount).toBeFocused(); await expect(discount).toHaveAttribute("aria-invalid", "true");
  expect(state.getIntake()?.status).not.toBe("saved");
  await discount.fill("");
  await page.getByRole("textbox", { name: "Комментарий 1", exact: true }).fill("Без скидки");
  await expect(discount).toHaveValue("0");
  await expect(page.getByText("Черновик сохранён", { exact: true })).toBeVisible();
  expect(state.getIntake()?.data.rows[0]).toMatchObject({ quantity: 12, discountPercent: 0 });
});

test("@desktop @phone mixed intake prints only restoration including zero discount", async ({ page }) => {
  await setup(page);
  const requests: number[][] = [];
  await page.route("**/api/v1/admin/returns/labels", async route => {
    requests.push(route.request().postDataJSON().ids);
    await route.fulfill({ contentType: "application/pdf", body: "%PDF-1.4\n" });
  });
  for (const [index, condition] of ["ready", "restoring", "dead", "restoring"].entries()) {
    if (index) await page.getByRole("button", { name: "Добавить растение" }).click();
    await page.getByRole("combobox", { name: `Название растения ${index + 1}`, exact: true }).fill("фикус");
    await page.getByRole("option", { name: /Фикус Бенджамина/ }).click();
    await page.getByRole("combobox", { name: `Состояние ${index + 1}`, exact: true }).selectOption(condition);
  }
  await page.getByRole("textbox", { name: "Скидка 4", exact: true }).fill("30");
  await page.getByRole("button", { name: "Сохранить и скачать этикетки", exact: true }).click();
  await expect.poll(() => requests.length).toBe(1);
  expect(requests[0]).toEqual([10101, 10301]);
  await page.getByRole("button", { name: "Закрыть документ", exact: true }).click();
  await page.getByRole("navigation", { name: "Журналы возвратов" }).getByRole("button", { name: /^Растения/ }).click();
  await page.getByRole("checkbox", { name: "Выбрать все растения", exact: true }).check();
  await page.getByRole("button", { name: "Скачать этикетки · 2", exact: true }).click();
  await expect.poll(() => requests.length).toBe(2);
  expect(requests[1].sort()).toEqual([10101, 10301]);
});

test("@desktop @phone catalogue styles and 50-row intake keep search aligned and footer reachable", async ({ page }, testInfo) => {
  const rows = Array.from({ length: 50 }, (_, index) => ({ key: `long-${index}`, variantId: 101 + index, name: `Растение с длинным названием ${index + 1}`, sku: `SKU-${index}`, search: `Растение с длинным названием ${index + 1} · D12`, quantity: 1, condition: "ready" as const, comment: "" }));
  const state = await setup(page, { id: "long-intake", revision: 1, status: "draft", receiptStatus: "none", data: { channel: "wb", channelComment: "", rows }, items: [] });
  await page.addStyleTag({ path: resolve(__dirname, "../../frontend/src/styles/admin-catalog.css") });
  const sizes = testInfo.project.name === "desktop" ? [{ width: 1366, height: 768 }, { width: 1093, height: 614 }, { width: 911, height: 512 }] : [{ width: 390, height: 844 }, { width: 390, height: 480 }];
  for (const size of sizes) {
    await page.setViewportSize(size);
    const search = page.getByRole("combobox", { name: "Название растения 50", exact: true });
    await search.fill(""); await search.click();
    const list = page.getByRole("listbox", { name: "Каталог растений" });
    await list.scrollIntoViewIfNeeded();
    const field = await search.locator("..").boundingBox(), options = await list.boundingBox();
    expect(Math.abs(options!.x - field!.x)).toBeLessThanOrEqual(1);
    expect(Math.abs(options!.width - field!.width)).toBeLessThanOrEqual(1);
    expect(options!.y).toBeGreaterThanOrEqual(field!.y + field!.height);
    const footer = await page.locator(".return-create-dialog footer").boundingBox();
    expect(footer!.y + footer!.height).toBeLessThanOrEqual(size.height);
    await page.getByRole("option", { name: /Фикус Бенджамина/ }).click();
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  }
  await page.getByRole("button", { name: "Сохранить возврат", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: /Документ приёмки №1/ }).click();
  await expect(page.getByRole("button", { name: "Редактировать документ", exact: true })).toBeInViewport();
  const footer = await page.locator(".return-create-dialog footer").boundingBox();
  expect(footer!.y + footer!.height).toBeLessThanOrEqual(sizes.at(-1)!.height);
  expect(state.getIntake()?.items).toHaveLength(50);
});
