import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

const adminOwner = {
  ...owner,
  user: { ...owner.user, adminRole: "owner" as const },
};

const adminProduct = {
  id: 1,
  sabyId: "saby-1",
  slug: "1",
  name: "Аглаонема Мария",
  latinName: "Aglaonema",
  shortDescription: "Неприхотливое растение",
  description: "Старое описание",
  careInstructions: "Поливать умеренно",
  status: "published",
  featured: false,
  image: "/assets/hero-monstera.webp",
  price: 1490,
  stock: 5,
  sku: "1",
  variantLabel: "D12",
  wholesaleMinQty: 1,
  overrideFields: [],
  sabyFields: ["stock"],
  sabyCode: "X100",
  catalogSection: "plants",
  categoryId: 1,
  passport: { faq: [] },
  importantWarnings: [],
  externalIds: [{ provider: "saby", type: "id", externalId: "saby-1" }],
  attributes: {},
};

test("@desktop покупатель не видит инструменты редактирования PDP", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  await expect(page.getByText("Режим администратора")).toHaveCount(0);
});

test("@desktop владелец открывает из PDP общий редактор и сохраняет описание", async ({ page }) => {
  await mockApi(page, adminOwner);
  let saved = {...adminProduct};
  let update: Record<string, unknown> | undefined;
  let plan = {variants: []};
  const variant = {id:10, productId:1, sku:"1",label:"D12",price:1490,stock:5,wholesaleMinQty:1,active:true,archived:false,attributes:{pot_diameter_cm:12,height_cm:35},externalIds:adminProduct.externalIds,images:[]};
  await page.route("**/api/v1/admin/**", async route => {
    const req=route.request(),path=new URL(req.url()).pathname;
    const send=(json:unknown)=>route.fulfill({json});
    if(path.endsWith('/dashboard'))return send({role:'owner',user:adminOwner.user,permissions:['products.read','products.edit','products.manage'],dashboard:{products:1,variants:1,orders:0,customers:1,wholesalePending:0,recentOrders:[]}});
    if(path==='/api/v1/admin/products')return send({products:[saved]});
    if(path.endsWith('/categories'))return send({categories:[{id:1,parentId:null,name:'Растения',slug:'plants',sortOrder:1,productsCount:1}]});
    if(path.endsWith('/attributes'))return send({attributes:[]});
    if(path.endsWith('/variants'))return send({variants:[variant]});
    if(path.endsWith('/media'))return send({media:path.includes('/products/')?[{id:7,productId:1,url:adminProduct.image,primary:true,sortOrder:0}]:[]});
    if(path.endsWith('/collection-definitions'))return send({collections:[]});
    if(path.endsWith('/catalog-editor/references'))return send({categories:[],folders:[]});
    if(path.endsWith('/editor-plan')){if(req.method()==='PUT')plan=req.postDataJSON().config;return send({config:plan,publications:[],links:{},barcodes:{},publicationMode:'disabled'});}
    if(path.endsWith('/settings'))return send({values:{'cdek.packaging_season':'summer'}});
    if(path==='/api/v1/admin/products/1'&&req.method()==='PATCH'){
      update=req.postDataJSON();saved={...saved,...update};return send({product:saved});
    }
    return route.fulfill({status:404,json:{error:`Unmocked admin endpoint: ${path}`}});
  });
  await page.goto('/product/1');
  await page.getByRole('button',{name:'Редактировать карточку',exact:true}).click();
  await expect(page).toHaveURL(/\/admin\?section=products&product=1/);
  const editor=page.locator('.editor-page');
  await expect(editor.getByRole('heading',{name:'Аглаонема Мария',exact:true})).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(editor.getByRole('tab')).toHaveCount(3);
  await editor.getByRole('textbox',{name:'О растении',exact:true}).fill('Новое описание из PDP');
  await editor.getByRole('button',{name:'Сохранить',exact:true}).click();
  await expect(editor.locator('.editor-notice')).toContainText('Изменения товара сохранены');
  expect(update?.description).toBe('Новое описание из PDP');
  await page.reload();
  await expect(editor.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Новое описание из PDP');
});

test("@desktop владелец управляет галереей товара из PDP", async ({ page }) => {
  await mockApi(page, adminOwner);
  await page.route("**/api/v1/admin/products", (route) => route.fulfill({ json: { products: [adminProduct] } }));
  let uploadCount = 0;
  let deleted = 0;
  await page.route("**/api/v1/admin/products/1/media", async (route) => {
    if (route.request().method() === "POST") {
      uploadCount++;
      await route.fulfill({ status: 201, json: { media: { id: 8, url: "/uploaded.jpg", primary: false, sortOrder: 1 } } });
      return;
    }
    await route.fulfill({ json: { media: [{ id: 7, url: "/assets/hero-monstera.webp", primary: true, sortOrder: 0 }] } });
  });
  await page.route("**/api/v1/admin/products/1/media/7", async (route) => {
    deleted++;
    await route.fulfill({ json: { deleted: true } });
  });
  page.on("dialog", (dialog) => void dialog.accept());

  await page.goto("/product/1");
  await page.getByRole("button", { name: "Фотографии" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Главная · Основное фото", {exact:true})).toBeVisible();
  await dialog.locator('input[type="file"]').setInputFiles({
    name: "plant.png", mimeType: "image/png", buffer: Buffer.from("fake-image"),
  });
  await expect.poll(() => uploadCount).toBe(1);
  await dialog.getByRole("button", { name: "Удалить" }).click();
  await expect.poll(() => deleted).toBe(1);
});
