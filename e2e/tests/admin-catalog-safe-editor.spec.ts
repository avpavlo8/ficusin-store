import { expect, test } from "@playwright/test";
import { owner } from "./helpers";

const categories = [
  { id: 1, parentId: null, name: "Растения", slug: "plants", sortOrder: 10, icon: "leaf", productsCount: 2, childrenCount: 0 },
  { id: 2, parentId: null, name: "Кашпо", slug: "pots", sortOrder: 20, icon: "pot", productsCount: 1, childrenCount: 0 },
  { id: 3, parentId: null, name: "Грунты", slug: "soil", sortOrder: 30, icon: "soil", productsCount: 1, childrenCount: 0 },
  { id: 4, parentId: null, name: "Удобрения", slug: "fertilizer", sortOrder: 40, icon: "fertilizer", productsCount: 1, childrenCount: 0 },
  { id: 5, parentId: null, name: "Аксессуары", slug: "accessories", sortOrder: 50, icon: "tools", productsCount: 1, childrenCount: 0 },
];
const passport = { origin:"",lighting:"",watering:"",humidity:"",temperature:"",soil:"",fertilizer:"",repotting:"",careDifficulty:"",growthRate:"",matureSize:"",toxicity:"",problems:"",pests:"",faq:[] };
const base = { sabyId:"",slug:"item",latinName:"",shortDescription:"",description:"",careInstructions:"",status:"draft",featured:false,image:"",price:100,stock:1,sku:"FIC-1",variantLabel:"Основной",wholesaleMinQty:1,overrideFields:[],sabyFields:[],sabyCode:"",passport,importantWarnings:[],externalIds:[],attributes:{} };
const products = [
  { ...base,id:1,name:"Фикус ручной",catalogSection:"plants",categoryId:1,attributes:{light_level:"diffused"} },
  { ...base,id:2,name:"Монстера следующая",catalogSection:"plants",categoryId:1 },
  { ...base,id:3,name:"Кашпо Oslo",catalogSection:"pots",categoryId:2 },
  { ...base,id:4,name:"Грунт универсальный",catalogSection:"soil",categoryId:3 },
  { ...base,id:5,name:"Удобрение",catalogSection:"fertilizer",categoryId:4 },
  { ...base,id:6,name:"Лопатка",catalogSection:"accessories",categoryId:5 },
];
const schemas: Record<number, unknown[]> = {
  1:[{code:"light_level",name:"Освещение",dataType:"enum",unit:"",options:["diffused"],optionLabels:{diffused:"Рассеянный свет"},audience:"customer",scope:"product",required:false,filterable:true,showOnPdp:true,keyCharacteristic:true,badge:true,sortOrder:10}],
  2:[{code:"material",name:"Материал",dataType:"enum",unit:"",options:["ceramic"],optionLabels:{ceramic:"Керамика"},audience:"customer",scope:"product",required:false,filterable:true,showOnPdp:true,keyCharacteristic:true,badge:false,sortOrder:10}],
  3:[],4:[],5:[],
};

async function mockCatalog(page: import("@playwright/test").Page, options: { aiError?: boolean; saveError?: boolean } = {}) {
  await page.addInitScript(({ user, categories, products, schemas, options }) => {
    const json=(body:unknown,status=200)=>Promise.resolve(new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}}));
    const original=window.fetch.bind(window);
    window.fetch=async(input,init)=>{const raw=typeof input==="string"?input:input instanceof Request?input.url:input.toString();const path=new URL(raw,location.origin).pathname;
      if(path==="/api/v1/auth/me")return json({user});
      if(path==="/api/v1/admin/dashboard")return json({user:{fullName:"Владелец"},role:"owner",permissions:["dashboard.read","products.read","products.edit","products.sync"],dashboard:{products:products.length,variants:products.length,orders:0,customers:0,wholesalePending:0,lastSync:null,recentOrders:[]}});
      if(path==="/api/v1/admin/products"&&(!init?.method||init.method==="GET"))return json({products});
      if(path==="/api/v1/admin/categories")return json({categories});
      const schema=path.match(/^\/api\/v1\/admin\/categories\/(\d+)\/attributes$/);if(schema)return json({attributes:schemas[Number(schema[1])]||[]});
      if(path==="/api/v1/admin/reviews")return json({reviews:[]});
      if(path.match(/^\/api\/v1\/admin\/products\/\d+\/variants$/))return json({variants:[]});
      if(path.match(/^\/api\/v1\/admin\/products\/\d+\/media$/))return json({media:[]});
      if(path==="/api/v1/admin/products/1/ai-draft"){if(options.aiError)return json({error:"AI недоступен"},503);return json({proposal:{name:"Фикус AI",shortDescription:"Короткое описание AI"}});}
      const update=path.match(/^\/api\/v1\/admin\/products\/(\d+)$/);if(update&&init?.method==="PATCH"){if(options.saveError)return json({error:"Ошибка сохранения"},500);const id=Number(update[1]);const body=JSON.parse(String(init.body||"{}"));(window as typeof window&{__updates?:unknown[]}).__updates=[...((window as typeof window&{__updates?:unknown[]}).__updates||[]),{id,body}];const index=products.findIndex((item)=>item.id===id);products[index]={...products[index],...body};return json({product:products[index]});}
      if(path==="/api/v1/admin/products/publish"&&init?.method==="POST")return json({published:[1],blocked:[{productId:2,name:"Монстера следующая",reason:"Нет фото"}]});
      if(path.startsWith("/api/v1/"))return json({});return original(input,init);
    };
  },{user:{...owner.user,adminRole:"owner"},categories,products:structuredClone(products),schemas,options});
}

async function openProducts(page: import("@playwright/test").Page) { await page.goto("/admin");await page.getByRole("button",{name:"Товары",exact:true}).click(); }

test("@desktop уход и plant AI доступны только растениям",async({page})=>{await mockCatalog(page);await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();let dialog=page.getByRole("dialog");await expect(dialog.getByRole("button",{name:"Уход и FAQ"})).toBeVisible();await expect(dialog.getByRole("button",{name:"✦ Предложить AI-обложку"})).toBeVisible();await dialog.getByRole("button",{name:"Закрыть",exact:true}).click();for(const name of ["Кашпо Oslo","Грунт универсальный","Удобрение","Лопатка"]){await page.getByText(name,{exact:true}).click();dialog=page.getByRole("dialog");await expect(dialog.getByRole("button",{name:"Уход и FAQ"})).toHaveCount(0);await expect(dialog.getByRole("button",{name:"✦ Предложить AI-обложку"})).toHaveCount(0);await dialog.getByRole("button",{name:"Закрыть",exact:true}).click();}});

test("@desktop AI заполняет пустое, а замена и отмена проходят через diff",async({page})=>{await mockCatalog(page);await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();const editor=page.locator(".product-editor-dialog");await editor.getByRole("button",{name:"✦ Сгенерировать раздел"}).click();const diff=page.locator(".ai-diff-dialog");await expect(diff).toContainText("Сейчас: Фикус ручной");const checks=diff.getByRole("checkbox");await expect(checks.nth(0)).not.toBeChecked();await expect(checks.nth(1)).toBeChecked();await diff.getByRole("button",{name:"Отмена"}).click();await expect(editor.getByRole("textbox",{name:"Название",exact:true})).toHaveValue("Фикус ручной");await expect(editor.getByRole("textbox",{name:"Короткое описание",exact:true})).toHaveValue("");});

test("@desktop русский label показывается, technical value сохраняется",async({page})=>{await mockCatalog(page);await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();const dialog=page.getByRole("dialog");await dialog.getByRole("button",{name:/Характеристики/}).click();const select=dialog.getByLabel("Освещение",{exact:true});await expect(select).toHaveValue("diffused");await expect(select.locator("option:checked")).toHaveText("Рассеянный свет");});

test("@desktop сохранить и следующий не переключает карточку при ошибке",async({page})=>{await mockCatalog(page,{saveError:true});await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();const dialog=page.getByRole("dialog");await dialog.getByRole("textbox",{name:"Короткое описание",exact:true}).fill("Заполнено");await dialog.getByRole("button",{name:"Сохранить и открыть следующий незаполненный"}).click();await expect(dialog).toContainText("Фикус ручной");await expect(dialog).toContainText("Ошибка сохранения");});

test("@desktop сохранить и следующий открывает товар текущего фильтра",async({page})=>{await mockCatalog(page);await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();let dialog=page.getByRole("dialog");await dialog.getByRole("textbox",{name:"Короткое описание",exact:true}).fill("Заполнено");await dialog.getByRole("button",{name:"Сохранить и открыть следующий незаполненный"}).click();dialog=page.getByRole("dialog");await expect(dialog).toContainText("Монстера следующая");const updates=await page.evaluate(()=>(window as typeof window&{__updates?:Array<{id:number}>}).__updates||[]);expect(updates).toEqual([{id:1,body:{shortDescription:"Заполнено"}}]);});

test("@desktop несохранённые изменения защищены при закрытии",async({page})=>{await mockCatalog(page);await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();const dialog=page.getByRole("dialog");await dialog.getByRole("textbox",{name:"Описание",exact:true}).fill("Несохранённое");page.once("dialog",(confirmation)=>void confirmation.dismiss());await dialog.getByRole("button",{name:"Закрыть",exact:true}).click();await expect(dialog).toBeVisible();});

test("@desktop ошибка AI не очищает форму",async({page})=>{await mockCatalog(page,{aiError:true});await openProducts(page);await page.getByText("Фикус ручной",{exact:true}).click();const dialog=page.getByRole("dialog");await dialog.getByRole("textbox",{name:/Описание/,exact:true}).fill("Ручной текст");await dialog.getByRole("button",{name:"✦ Сгенерировать раздел"}).click();await expect(dialog.getByRole("textbox",{name:/Описание/,exact:true})).toHaveValue("Ручной текст");await expect(page.getByText("AI недоступен")).toBeVisible();});

test("@desktop массовая операция разделяет успех, пропуск и ошибку",async({page})=>{await mockCatalog(page);await openProducts(page);const rows=page.locator("table.products tbody tr");await rows.nth(0).getByRole("checkbox").check();await rows.nth(1).getByRole("checkbox").check();await page.getByRole("button",{name:"Опубликовать"}).click();await page.getByRole("button",{name:/Подтвердить публикацию/}).click();await expect(page.locator(".admin-bulk-result")).toContainText("Успешно: 1 · Пропущено: 1 · Ошибка: 0");});
