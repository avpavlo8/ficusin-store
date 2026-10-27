import { expect, test } from "@playwright/test";
import { horizontalOverflow, owner } from "./helpers";

test("@desktop @phone assortment waits for arrival and distributes into existing cards",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,catalogMatch:true,currency:"EUR"});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 const row=order.getByRole('row').filter({hasText:'Бонсай Зантоксилум D15'});
 await order.getByRole('textbox',{name:'Курс оплаты',exact:true}).fill('123');
 await row.getByRole('button',{name:'Способ приёмки',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Способ приёмки',exact:true});
 await dialog.getByRole('combobox',{name:'Способ приёмки растения'}).selectOption('assortment');
 await dialog.getByRole('button',{name:'Сохранить способ приёмки',exact:true}).click();
 await expect(dialog).toBeHidden();
 await expect(order.getByRole('textbox',{name:'Курс оплаты',exact:true})).toHaveValue('123');
 await expect(row).toContainText('Разобрать после приезда');
 await expect(row).toContainText('Распределено 0 / 60 шт.');
 await order.getByRole('tab',{name:'Разобрать миксы 1',exact:true}).click();
 await expect(order.locator('.procurement-order-lines > tbody > tr')).toHaveCount(1);
 await row.getByRole('button',{name:'Разобрать микс',exact:true}).click();
 await dialog.getByRole('textbox',{name:'Поиск карточки для разбора'}).fill('Bonsai');
 await dialog.getByRole('button',{name:'Добавить',exact:true}).click();
 const qty=dialog.getByRole('textbox',{name:'Количество: Bonsai Zantaxilum D15',exact:true});
 await qty.fill('61');
 await expect(dialog.getByRole('button',{name:'Сохранить распределение',exact:true})).toBeDisabled();
 await qty.fill('59');
 await expect(dialog.getByRole('button',{name:'Сохранить распределение',exact:true})).toBeDisabled();
 await qty.fill('60');
 await dialog.getByRole('button',{name:'Сохранить распределение',exact:true}).click();
 await expect(dialog).toBeHidden();
 await expect(order.getByRole('tab',{name:'Разобрать миксы 0',exact:true})).toHaveAttribute('aria-selected','true');
 await order.getByRole('tab',{name:/^Все /}).click();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 await expect(row).toContainText('Распределено 60 / 60 шт.');
 await page.reload();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 await expect(page.getByRole('dialog',{name:'TEST-100',exact:true}).getByRole('row').filter({hasText:'Бонсай Зантоксилум D15'})).toContainText('Bonsai Zantaxilum D15: 60 шт.');
});

test("@desktop @phone accidental card can be detached while waiting for a new Saby card",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,catalogMatch:true});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 const row=order.getByRole('row').filter({hasText:'Bonsai Zantaxilum'});
 await row.getByRole('button',{name:'Выбрать товар СБИС',exact:true}).click();
 await page.getByRole('dialog',{name:'Выбрать товар СБИС',exact:true}).getByRole('button',{name:'Выбрать',exact:true}).click();
 await expect(row).toContainText('Bonsai Zantaxilum D15');
 await row.getByRole('button',{name:'Способ приёмки',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Способ приёмки',exact:true});
 await dialog.getByRole('combobox',{name:'Способ приёмки растения'}).selectOption('awaiting_catalog');
 await dialog.getByRole('button',{name:'Сохранить способ приёмки',exact:true}).click();
 await expect(dialog).toBeHidden();
 await expect(row).toContainText('Ожидает карточку СБИС');
 await expect(row).not.toContainText('Bonsai Zantaxilum D15');
 await expect(row).toContainText('60 / 60');
 await page.reload();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 await expect(page.getByRole('dialog',{name:'TEST-100',exact:true}).getByRole('row').filter({hasText:'Bonsai Zantaxilum'})).toContainText('Ожидает карточку СБИС');
});

test("@desktop @phone invoice match exposes missing Saby card and resolves it inside purchase",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,catalogMatch:true});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await order.getByRole('button',{name:'Показать строки без товара СБИС (1)',exact:true}).click();
 await expect(order.getByRole('tab',{name:'Нет товара СБИС 1',exact:true})).toHaveAttribute('aria-selected','true');
 const row=order.getByRole('row').filter({hasText:'Бонсай Зантоксилум D15'});
 await expect(row).toContainText('Совпадает');
 await expect(row).toContainText('Выберите товар СБИС');
 await row.getByRole('button',{name:'Выбрать товар СБИС',exact:true}).click();
 const picker=page.getByRole('dialog',{name:'Выбрать товар СБИС',exact:true});
 await expect(picker).toBeVisible();
 const box=await picker.boundingBox();const viewport=page.viewportSize()!;
 expect(box!.y).toBeGreaterThanOrEqual(0);expect(box!.y+box!.height).toBeLessThanOrEqual(viewport.height);
 await picker.getByRole('button',{name:'Выбрать',exact:true}).click();
 await expect(picker).toBeHidden();
 await expect(order.getByText('Не выбраны товары СБИС: 1',{exact:true})).toHaveCount(0);
 await order.getByRole('tab',{name:/^Все /}).click();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 const matched=order.getByRole('row').filter({hasText:'Bonsai Zantaxilum'});
 await expect(matched).toContainText('Bonsai Zantaxilum D15');
 await expect(matched).toContainText('60 / 60');
 await page.reload();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 await expect(page.getByRole('dialog',{name:'TEST-100',exact:true}).getByRole('row').filter({hasText:'Bonsai Zantaxilum'})).toContainText('Bonsai Zantaxilum D15');
});

test("@desktop @phone manager can inspect incoming plants without editing controls",async({page})=>{
 await mockProcurement(page,{manager:true,invoicePicker:true});
 await page.goto('/admin?section=procurement&order=4');
 await expect(page.getByRole('heading',{name:'TEST-100',exact:true})).toBeVisible();
 await page.getByRole('textbox',{name:'Поиск растения в закупке'}).fill('Нижняя строка');
 await expect(page.getByRole('row').filter({hasText:'Нижняя строка без инвойса'})).toContainText('60');
 await expect(page.getByRole('button',{name:'Рассчитать',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Отменить закупку',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Закрыть закупку',exact:true}).click();
 await expect(page.getByRole('button',{name:'Собрать заказ',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Загрузить инвойс',exact:true})).toHaveCount(0);
});

test("@desktop @phone several invoice rows fulfil one planned quantity",async({page})=>{
 await mockProcurement(page,{invoicePicker:true});
 await page.goto('/admin?section=procurement&order=4');
 await page.getByRole('dialog',{name:'TEST-100',exact:true}).getByRole('tab',{name:'Только в закупке 1',exact:true}).click();
 await page.getByRole('button',{name:'Выбрать из инвойса',exact:true}).click();
 const picker=page.getByRole('dialog',{name:'Выбрать строки инвойса',exact:true});
 for(const id of [2000,2001,2002])await picker.getByRole('checkbox',{name:new RegExp(`Добавить строку ${id}:`)}).check();
 await expect(picker.getByText('Выбрано: 3 · +60 шт. · итог 60 / 60 шт.',{exact:true})).toBeVisible();
 await picker.getByRole('button',{name:'Сопоставить выбранные',exact:true}).click();
 await expect(picker).toBeHidden();
 await page.getByRole('dialog',{name:'TEST-100',exact:true}).getByRole('tab',{name:/^Все /}).click();
 await page.getByLabel('Поиск строки закупки').fill('Нижняя строка');
 await expect(page.getByRole('row').filter({hasText:'Нижняя строка без инвойса'})).toContainText('60 / 60');
});

async function mockProcurement(page: import("@playwright/test").Page, options: { sizeDefaults?: boolean; firstSabyPrices?: boolean; blockers?: string[] | null; currency?: "RUB" | "EUR"; anotherRecommendation?: boolean; requestError?: string; invoicePicker?: boolean; catalogMatch?: boolean; manager?: boolean; receivingTabs?: boolean; dimensionReview?: boolean; unified?:boolean; unifiedReceived?:boolean } = {}) {
  const procurement = {
    summary: { openOrders: 1, unresolvedAliases: 12, availabilityChecks: 3, openRequests: 2 },
    integrations: { wb: true, ozon: false, saby: false },
    settings: { economics:{monthlyFixedCostsRub:300000,channels:{}},version: 1, defaultExchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 63700, trolleyVolumeCm3: 1, trolleyFillRatio: 1, returnLossRate: 0, marketplaceCostRate: 0, taxRate: 0, reserveRate: 0, packageRub: 0, priceChangeThreshold: .1, domesticRetailMultiplier: 1, internationalCostMultiplier: 1, internationalRetailMultiplier: 1, marketplaceStrikeMarkup: 0, retailRoundStep: 1, avoidRoundHundreds: false, recommendationDays: 30, targetCoverDays: 30, retailMarkupMultiplier: 2.1, roundPrices: true },
    suppliers: [{ id: 1, name: "Тестовый поставщик", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }, { id: 2, name: "Другой поставщик", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }],
    orders: [{ id: 4, supplierId: 1, supplierName: "Тестовый поставщик", orderNumber: "TEST-100", documentNumber: "", sourceKind: "payment_invoice", currency: options.currency || "RUB", status: "draft", lines: 5, units: 20, total: 10000, unmatched: 2, createdAt: "2026-08-10T12:00:00Z" }],
    documents: [{ id: 7, supplierId: 1, supplierName: "Тестовый поставщик", orderId: 4, fileName: "test.pdf", parserKind: "domestic_payment_invoice", parseStatus: "review", arithmeticStatus: "ok", documentNumber: "TEST-100", documentDate: "2026-08-07", currency: "RUB", lines: 5, units: 20, productSubtotal: 10000, packageTotal: 0, documentTotal: 10000, calculatedTotal: 10000, parseError: "", createdAt: "2026-08-10T12:00:00Z" }],
    review: [{ id: 9, supplierId: 1, supplierName: "Тестовый поставщик", rawName: "Тестовая строка D10", supplierArticle: "", potDiameterCm: 10, suggestedSabyId: "TEST-SABY-1", suggestedSabyName: "Тестовый товар D10", matchStatus: "suggested", confidence: 0.52, availabilityStatus: "unknown" }],
    requests: [] as Array<Record<string, unknown>>, availability: [] as Array<Record<string, unknown>>, recommendations: [
      { aliasId: 9, supplierId: 1, sabyId: "TEST-SABY-1", name: "Тестовый товар D10", supplierArticle: "SUP-1", dutchName: "Цитрус", potDiameterCm: 12, heightCm: 35, lastUnitPrice: 5.3, availability: "available", balance: 2, incoming: 0, siteSales: 2, sabySales: 5, wbSales: 1, ozonSales: 0, totalSales: 8, customerRequests: 1, staffRequests: 0, openRequests: 1, minimumOrderQty: 6, orderMultiple: 6, suggestedQty: 12, dailySales: 0.27, daysOfCover: 7.5, status: "recommended", reason: "Есть товар под заказ клиента" },
      { aliasId: 10, supplierId: 1, sabyId: "TEST-SABY-2", name: "Товар уже едет", supplierArticle: "SUP-2", availability: "available", balance: 0, incoming: 6, siteSales: 0, sabySales: 4, wbSales: 0, ozonSales: 0, totalSales: 4, customerRequests: 0, staffRequests: 0, openRequests: 0, minimumOrderQty: 1, orderMultiple: 1, suggestedQty: 0, dailySales: 0.13, daysOfCover: 0, status: "already_ordered", reason: "Уже заказано 6 шт.; повторная закупка исключена" },
    ],
    salesSync: [
      { channel: "saby", status: "ok", lastSuccessAt: "2026-08-10T12:00:00Z", lastError: "", rowsSynced: 120, periodFrom: "2025-08-11", periodTo: "2026-08-10", latestSale: "2026-08-10" },
      { channel: "site", status: "ok", lastSuccessAt: "2026-08-10T12:00:00Z", lastError: "", rowsSynced: 12, periodFrom: "2025-08-11", periodTo: "2026-08-10", latestSale: "2026-08-09" },
      { channel: "wb", status: "disabled", lastError: "", rowsSynced: 0, periodFrom: "", periodTo: "", latestSale: "" },
      { channel: "ozon", status: "disabled", lastError: "", rowsSynced: 0, periodFrom: "", periodTo: "", latestSale: "" },
    ],
    integrationHealth: [
      { channel: "wb", configured: true, lastError: "" },
      { channel: "ozon", configured: true, lastError: "" },
      { channel: "saby", configured: false, lastError: "" },
    ],
  };
  if (options.anotherRecommendation) procurement.recommendations.push({ aliasId: 11, supplierId: 1, sabyId: "TEST-SABY-3", name: "Олива D18", supplierArticle: "SUP-3", dutchName: "Олива", potDiameterCm: 18, heightCm: 55, lastUnitPrice: 10, availability: "available", balance: 4, incoming: 0, siteSales: 2, sabySales: 0, wbSales: 0, ozonSales: 0, totalSales: 2, customerRequests: 0, staffRequests: 0, openRequests: 0, minimumOrderQty: 1, orderMultiple: 1, suggestedQty: 2, dailySales: 0.1, daysOfCover: 4, status: "recommended", reason: "Есть спрос" });
  const orderDetail = {
    order: procurement.orders[0], costs: { exchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 0, deliveryToMoscowRub: 0, deliveryToRyazanRub: 0 },
    validation: { canCalculate: false, canPrepareActions: false, blockers: options.blockers === undefined ? ["Не загружен инвойс или счёт", "Не выбраны товары СБИС: 2"] : options.blockers, arithmeticMismatch: 0, comparisonMismatch: 0, missingDimensions: 0, missingLoadUnits: 0, invalidLines: 0, unmatched: 2, trolleyCount: 0, expectedTrolleyRub: 0, allocatedTrolleyRub: 0, expectedRyazanRub: 0, allocatedRyazanRub: 0 },
    lines: [] as Array<Record<string, unknown>>, batches: [],
  };
  if(options.unified)Object.assign(procurement.settings.economics,{unified:{enabled:true,markupRate:.6,materialsRub:100,smallBoxRub:30,mediumBoxRub:45,largeBoxRub:60,wbFirstLiterRub:78.2,wbAdditionalLiterRub:23.8,wbHandlingRub:10,verifiedAt:"2026-10-19",norms:[{maxLiters:9,ozonRub:256.35,samples:45}]}});
  if(options.unified)Object.assign(orderDetail,{unifiedPreview:{version:7,markupRate:.6,fixedRub:300000,units:3,pricedUnits:3,contributionRub:360,lines:[{variantId:777,name:"Unified plant D12",quantity:3,costRub:200,quotes:{wb:{priceRub:1705,contributionRub:120.375,missing:[],costs:{packagingRub:145,logisticsRub:278.6},package:{lengthCm:15,widthCm:15,heightCm:40,source:"WB"}},ozon:{priceRub:2191,contributionRub:120.275,missing:[],costs:{packagingRub:145,logisticsRub:256.35}}}}]}});
  if(options.unifiedReceived)procurement.orders[0].status="received";
  if (options.invoicePicker) {
    procurement.orders[0].status = "review";
    const baseLine = { supplierCategory: "Растения", orderedQuantity: 1, invoicedQuantity: 1, expectedUnitPrice: 4.85, unitPrice: 4.85, potDiameterCm: 14, heightCm: 40, packageCount: 1, unitsPerPackage: 1, loadUnit: "shelf", comparisonAccepted: false, currentRetailRub: 0, matchStatus: "confirmed" };
    orderDetail.lines = [
      ...Array.from({ length: 60 }, (_, index) => ({ ...baseLine, id: index + 1, rawName: `Товар ${index + 1}`, sabyName: `Товар ${index + 1}`, sabyId: `SABY-${index + 1}`, invoiceRawName: `Invoice ${index + 1}`, reconciliationStatus: "matched" })),
      { ...baseLine, id: 1001, rawName: "Нижняя строка без инвойса", sabyName: "Нижняя строка без инвойса", sabyId: "SABY-MISSING", orderedQuantity:60, invoicedQuantity: null, reconciliationStatus: "missing" },
      ...Array.from({ length: 30 }, (_, index) => ({ ...baseLine, id: index + 2000, rawName: `Добавлено ${index + 1}`, invoiceRawName: `Added invoice ${index + 1}`, sabyId: "", orderedQuantity: 0, invoicedQuantity: index<3?[10,20,30][index]:1, reconciliationStatus: "added" })),
    ];
  }
  if (options.receivingTabs) {
    Object.assign(orderDetail.lines[0], {receivingMode:"assortment",receivingAllocations:[]});
    Object.assign(orderDetail.lines[1], {receivingMode:"customer_handoff",customerHandedOver:false});
    Object.assign(orderDetail.lines[2], {receivingMode:"customer_handoff",customerHandedOver:true});
    Object.assign(orderDetail.lines[3], {receivingMode:"assortment",receivingAllocations:[{sabyId:"SABY-4",quantity:1,name:"Товар 4"}]});
    Object.assign(orderDetail.lines[4], {receivingMode:"assortment",invoiceExcluded:true});
  }
  if (options.catalogMatch) {
    const line=orderDetail.lines.find(line=>line.id===1001)!;
    Object.assign(line,{rawName:"Бонсай Зантоксилум D15",sabyName:"",sabyId:"",supplierArticle:"BONSAI",invoiceRawName:"Bonsai Zantaxilum",invoicedQuantity:60,matchStatus:"unmatched",reconciliationStatus:"matched",matchAlias:{...procurement.review[0],id:9,rawName:"Bonsai Zantaxilum",matchStatus:"unmatched"}});
    orderDetail.validation.unmatched=1;
    orderDetail.validation.blockers=["Не выбраны товары СБИС: 1"];
  }
  if(options.dimensionReview) {
    const first=orderDetail.lines[0];
    orderDetail.lines=[
      {...first,id:1001,rawName:"Бонсай D10",sabyName:"Бонсай D10",catalogueLabel:"D10",potDiameterCm:12,heightCm:20,matchAlias:{potDiameterCm:12,heightCm:20},dimensionWarning:"Карточка D10, инвойс D12: проверьте вариант товара."},
      {...first,id:1002,rawName:"Непентес микс",sabyName:"",receivingMode:"customer_handoff",matchStatus:"deferred",potDiameterCm:12,heightCm:40,matchAlias:{potDiameterCm:9,heightCm:20},dimensionWarning:"Размеры отличаются от инвойса: проверьте фактические размеры"},
      {...first,id:1003,rawName:"Исключённая строка",invoiceExcluded:true,dimensionWarning:"Карточка D10, инвойс D12"},
      {...first,id:1004,rawName:"Проверенная строка",dimensionsReviewed:true,dimensionWarning:""},
    ];
    orderDetail.validation.unmatched=0;
    orderDetail.validation.blockers=[...orderDetail.lines.filter(line=>!line.invoiceExcluded && line.dimensionWarning).map(line=>`${line.rawName}: ${line.dimensionWarning}`),...(options.blockers || [])];
    await page.exposeFunction("procurementDimensionFixture",(path:string,rawBody:string)=>{
      if(path.includes("/order-lines/")) {
        const id=Number(path.split("/").at(-1)); const line=orderDetail.lines.find(line=>line.id===id)!;
        const body=JSON.parse(rawBody);
        if(!(body.potDiameterCm>0 && body.heightCm>0))throw new Error("Invalid dimensions");
        Object.assign(line,body,{dimensionsReviewed:true,dimensionWarning:""});
        orderDetail.validation.blockers=[...orderDetail.lines.filter(line=>!line.invoiceExcluded && line.dimensionWarning).map(line=>`${line.rawName}: ${line.dimensionWarning}`),...(options.blockers || [])];
        orderDetail.validation.canCalculate=orderDetail.validation.blockers.length===0;
      }
      return orderDetail;
    });
  }
  if(options.sizeDefaults) Object.assign(procurement.recommendations[0],{potDiameterCm:null,heightCm:null});
  const dashboard = {
      user: { fullName: "Тестовый владелец" }, role: "owner",
      permissions: ["dashboard.read", "procurement.read", "procurement.edit", "integrations.edit", "catalog.delete"],
      dashboard: { products: 331, variants: 331, orders: 0, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
  };
  if(options.manager){dashboard.role="manager";dashboard.permissions=["dashboard.read","procurement.read"];procurement.orders[0].status="review";}
  // Install the API stub inside the page before React starts. WebKit handles
  // route interception differently after state transitions, while fetch is
  // identical across the browsers this layout suite covers.
  // Keep the mock backend outside the document so reloading the UI does not
  // reset saved data. PostgreSQL tests separately verify durable storage.
  if(options.catalogMatch) await page.exposeFunction("procurementFixture",(path:string,rawBody:string)=>{
    if(path==="/api/v1/admin/procurement")return procurement;
    if(path==="/api/v1/admin/procurement/orders/4")return orderDetail;
    const body=JSON.parse(rawBody);
    const line=orderDetail.lines.find(line=>line.id===1001)!;
    if(path.endsWith("/aliases/9")){
      if(body.matchStatus!=="confirmed" || body.sabyId!=="TEST-SABY-BONSAI")throw new Error("Wrong catalog candidate");
      Object.assign(line,{sabyId:body.sabyId,sabyCode:"X616872557",sabyName:"Bonsai Zantaxilum D15",matchStatus:"confirmed",receivingMode:"catalog"});
      Object.assign(orderDetail.validation,{pendingCatalog:0});
      procurement.review=[];procurement.summary.unresolvedAliases=0;
    }else if("channelCosts" in body || "retailOverrideRub" in body){
      Object.assign(line,body);
    }else{
      Object.assign(line,{receivingMode:body.receivingMode,matchStatus:body.receivingMode==="catalog"?"unmatched":"deferred",receivingAllocations:(body.receivingAllocations || []).map((part:{sabyId:string;quantity:number})=>({...part,name:"Bonsai Zantaxilum D15",code:"X616872557"}))});
    }
    if(body.receivingMode==="awaiting_catalog"){
      Object.assign(line,{sabyId:"",sabyName:"",sabyCode:""});
      Object.assign(orderDetail.validation,{pendingCatalog:1});
    }
    orderDetail.validation.pendingAssortments=line.receivingMode==="assortment" && (line.receivingAllocations || []).reduce((sum,part)=>sum+part.quantity,0)!==line.invoicedQuantity?1:0;
    orderDetail.validation.unmatched=0;orderDetail.validation.blockers=[];
    return orderDetail;
  });
  if (options.firstSabyPrices) {
    orderDetail.order.status = "received";
    orderDetail.lines = Array.from({length: 8}, (_,index) => ({id:index+1,
      sabyId:`NEW-${index+1}`,sabyCode:`X${index+1}`,sabyName:`Новый товар ${index+1}`,
      rawName:`Новый товар ${index+1}`,quantity:2,orderedQuantity:0,invoicedQuantity:2,
      reconciliationStatus:"matched",matchStatus:"confirmed",receivingMode:"catalog",
      unitPrice:350,unitCostRub:350,currentRetailRub:0,proposedRetailRub:636+index}));
    orderDetail.lines.push({...orderDetail.lines[0],id:9,sabyId:"EXISTING",canonicalVariantId:99});
  }
  await page.addInitScript(({ user, dashboard, procurement, orderDetail, requestError, catalogMatch, dimensionReview, sizeDefaults }) => {
    const originalFetch = window.fetch.bind(window);
    const sabyExports: unknown[] = [];
    Object.assign(window, { sabyExports });

    const drafts: Array<{ id: number; title: string; payload: unknown; createdAt: string; updatedAt: string }> = [];
    const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), {
      status: 200, headers: { "Content-Type": "application/json" },
    }));
    window.fetch = (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const path = new URL(raw, window.location.origin).pathname;
      if(dimensionReview && (path === "/api/v1/admin/procurement/orders/4" || path.startsWith("/api/v1/admin/procurement/order-lines/"))) {
        return (window as unknown as {procurementDimensionFixture:(path:string,body:string)=>Promise<unknown>}).procurementDimensionFixture(path,String(init?.body || "{}")).then(json);
      }
      if(catalogMatch && ["/api/v1/admin/procurement","/api/v1/admin/procurement/orders/4","/api/v1/admin/procurement/aliases/9","/api/v1/admin/procurement/order-lines/1001"].includes(path)){
        return (window as unknown as {procurementFixture:(path:string,body:string)=>Promise<unknown>}).procurementFixture(path,String(init?.body || "")).then(json);
      }
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json(dashboard);
      if (path === "/api/v1/admin/procurement") return json(procurement);
      if(path === "/api/v1/admin/procurement/requests" && init?.method === "POST"){
        if(requestError) return Promise.resolve(new Response(JSON.stringify({error:requestError}),{status:503,headers:{"Content-Type":"application/json"}}));
        const body=JSON.parse(String(init.body));
        if(Object.keys(body).some(key=>!["kind","requestedName","sabyId","quantity","notes","dueDate","assignee","source","customerOrderId"].includes(key)))return Promise.resolve(new Response(JSON.stringify({error:"Unknown create request field"}),{status:400}));
        const item={...body,id:procurement.requests.length+1,status:"open",createdAt:new Date().toISOString(),allocatedQuantity:0,remainingQuantity:body.quantity,allocations:[],history:[]};procurement.requests.push(item);return json({request:item});
      }
      if(path.startsWith("/api/v1/admin/procurement/requests/") && init?.method === "PATCH"){
        const body=JSON.parse(String(init.body));
        if(Object.keys(body).some(key=>!["requestedName","sabyId","quantity","notes","dueDate","assignee","status"].includes(key)))return Promise.resolve(new Response(JSON.stringify({error:"Unknown update request field"}),{status:400}));
        const id=Number(path.split("/").at(-1));const item=procurement.requests.find(item=>item.id===id);if(item)Object.assign(item,body);return json({request:item});
      }
      if(path === "/api/v1/admin/procurement/pricing/preview")return json({retailRub:1490,quotes:{site:{priceRub:1490,breakEvenRub:800,contributionRub:600,coverUnits:500,missing:[]},wb:{missing:["комиссия","логистика"]},ozon:{missing:["комиссия","логистика"]}}});
      if(path === "/api/v1/admin/procurement/settings/example")return json({before:{cost:682,retail:1490},after:{cost:682,retail:1590},history:[]});
      if (path === "/api/v1/admin/procurement/recommendations/supplier" && init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as { sabyId: string; supplierId: number };
        const item = procurement.recommendations.find((candidate) => candidate.sabyId === body.sabyId);
        if (item) { item.supplierId = body.supplierId; item.supplierArticle = ""; }
        return json({ ok: true });
      }
      if (path === "/api/v1/admin/procurement/plan-drafts") {
        if (init?.method === "POST") {
          const body = JSON.parse(String(init.body)) as { title: string; payload: unknown };
          const draft = { id: drafts.length + 1, title: body.title, payload: body.payload, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
          drafts.push(draft);
          return json({ draft });
        }
        return json({ drafts: drafts.map(({ id, title, createdAt, updatedAt }) => ({ id, title, createdAt, updatedAt })) });
      }
      if (path.startsWith("/api/v1/admin/procurement/plan-drafts/")) {
        const id = Number(path.split("/").at(-1));
        const draft = drafts.find((item) => item.id === id);
        if (init?.method === "DELETE") { const index = drafts.findIndex((item) => item.id === id); if (index >= 0) drafts.splice(index, 1); return Promise.resolve(new Response(null, { status: 204 })); }
        if (init?.method === "PUT" && draft) { const body = JSON.parse(String(init.body)) as { title: string; payload: unknown }; draft.title = body.title; draft.payload = body.payload; draft.updatedAt = new Date().toISOString(); }
        return json({ draft });
      }
      if (path === "/api/v1/admin/procurement/suppliers/1" && init?.method === "PATCH") {
        const item=procurement.suppliers.find(item=>item.id===1)!;Object.assign(item,JSON.parse(String(init.body)));
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      if (path === "/api/v1/admin/procurement/integrations/wb/check" && init?.method === "POST") {
        return json({ integration: { channel: "wb", configured: true, lastCheckedAt: "2026-08-13T12:00:00Z", lastSuccessAt: "2026-08-13T12:00:00Z", lastError: "" } });
      }
      if (path === "/api/v1/admin/procurement/aliases/9" && init?.method === "PATCH") {
        const body=JSON.parse(String(init.body));
        if(body.matchStatus!=="confirmed" || body.sabyId!=="TEST-SABY-BONSAI")return Promise.resolve(new Response(JSON.stringify({error:"Wrong catalog candidate"}),{status:400}));
        const line=orderDetail.lines.find(line=>line.id===1001)!;
        Object.assign(line,{sabyId:body.sabyId,sabyCode:"X616872557",sabyName:"Bonsai Zantaxilum D15",matchStatus:"confirmed"});
        orderDetail.validation.unmatched=0;orderDetail.validation.blockers=[];
        procurement.review=[];procurement.summary.unresolvedAliases=0;
        return json({alias:{id:9,...body}});
      }
      if(path==="/api/v1/admin/procurement/orders/4/batches" && init?.method==="POST" && orderDetail.order.status==="received") {
        const body=JSON.parse(String(init.body));
        if(body.kind!=="prices"||body.channels?.length!==1||body.channels[0]!=="ozon")return Promise.resolve(new Response(JSON.stringify({error:"Wrong channel"}),{status:400}));
        const batch={id:99,kind:"prices",unified:true,status:"draft",createdAt:"2026-10-19T18:00:00Z",items:[{id:991,lineId:1,productName:"Unified plant D12",productCode:"X1",channel:"ozon",externalArticle:"plant-d12",oldValue:3000,newValue:2191,status:"draft",errorMessage:""}]};
        Object.assign(orderDetail,{batches:[batch]});return json({batch});
      }
      if (path === "/api/v1/admin/procurement/orders/4/saby-prices.xlsx") {
        sabyExports.push({method:init?.method || "GET",body:JSON.parse(String(init?.body || "{}"))});
        return Promise.resolve(new Response("mock-xlsx", {headers:{"Content-Type":"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","Content-Disposition":'attachment; filename="first-prices.xlsx"'}}));
      }
      if (path === "/api/v1/admin/procurement/orders/4") return json(orderDetail);
      if (path === "/api/v1/admin/procurement/order-lines/1001" && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body));
        const planned = orderDetail.lines.find(line => line.id === 1001)!;
        if(body.receivingMode){
          Object.assign(planned,{receivingMode:body.receivingMode,matchStatus:body.receivingMode==="catalog"?"unmatched":"deferred",receivingAllocations:(body.receivingAllocations || []).map((part:{sabyId:string;quantity:number})=>({...part,name:"Bonsai Zantaxilum D15",code:"X616872557"}))});
          orderDetail.validation.unmatched=0;orderDetail.validation.blockers=[];
          return json(orderDetail);
        }
        if("heightCm" in body || "acceptComparison" in body || "invoiceExcluded" in body || "channelCosts" in body || "retailOverrideRub" in body){Object.assign(planned,body);return json(orderDetail);}
        const invoices = orderDetail.lines.filter(line => (body.invoiceLineIds || [body.invoiceLineId]).includes(line.id));
        const parts=invoices.map(line=>({rawName:line.invoiceRawName,quantity:line.invoicedQuantity,unitPrice:line.unitPrice,lineTotal:Number(line.invoicedQuantity)*Number(line.unitPrice),sourcePage:1,sourceLine:line.id}));
        Object.assign(planned, { invoiceRawName: invoices[0].invoiceRawName, invoicedQuantity: invoices.reduce((sum,line)=>sum+Number(line.invoicedQuantity),0), invoiceComponents:parts, reconciliationStatus: "matched" });
        for(const invoice of invoices)invoice.reconciliationStatus = "superseded";
        orderDetail.lines=orderDetail.lines.filter(line=>line.reconciliationStatus!=="superseded");
        return json(orderDetail);
      }
      if (path === "/api/v1/admin/procurement/plans/preview" && init?.method === "POST") {
        return json({ lines: [{ purchase: 636, cost: 700, retail: 1490 }] });
      }
      if (path === "/api/v1/admin/procurement/nomenclature/balances") return json({ items: [{ sabyId: "TEST-SABY-1", balance: 2, defaults: sizeDefaults ? [{category:"Растения",article:"SIZE-SMALL",potDiameterCm:12,heightCm:35,unitPrice:5.3,unitsPerPackage:6},{category:"Растения",article:"SIZE-LARGE",potDiameterCm:17,heightCm:70,unitPrice:12.5,unitsPerPackage:8}] : [], seenAt: "2026-08-10T12:00:00Z" }, { sabyId: "TEST-SABY-3", balance: 4, seenAt: "2026-08-10T12:00:00Z" }] });
      if (path === "/api/v1/admin/procurement/nomenclature") {
        return json({ items: [{ sabyId: "TEST-SABY-BONSAI", code: "X616872557", article: "BONSAI", name: "Bonsai Zantaxilum D15", balance: 3, price: 2190, totalSales: 7, sabySales: 1, wbSales: 3, ozonSales: 2, siteSales: 1, supplierLinked: true }] });
      }
      if (path === "/api/v1/admin/procurement/products") {
        return json({ items: [{ variantId: 22, sabyId: "", sabyCode: "PLANT-22", sabyArticle: "", name: "Новый товар без кода СБИС", balance: 0, currentPriceRub: 0, supplierId: 1, supplierName: "Тестовый поставщик", supplierArticle: "NL-22", availabilityStatus: "unknown", checkAfter: "", hollandArticle: "", wbVendorCode: "WB-22", ozonOfferId: "OZON-22", wbArticles: ["WB-22"], wbLegacyArticles: [], ozonArticles: ["OZON-22"], ozonLegacyArticles: [], sabySales: 0, siteSales: 0, wbSales: 1, ozonSales: 2, minimumOrderQty: 1, orderMultiple: 6, aliases: [], aliasIds: [] }] });
      }
      if (path === "/api/v1/admin/procurement/documents" && init?.method === "POST") {
        orderDetail.validation.blockers = ["Не выбраны товары СБИС: 2"];
        return json({ duplicate: false, order: procurement.orders[0] });
      }
      if (path === "/api/v1/admin/procurement/sales/nomenclature") {
        // Живой справочник иногда отдаёт одну запись дважды. Разбору продаж
        // выбирать между копиями нечего — он обязан их схлопнуть.
        const candidate = { sabyId: "TEST-SABY-1", code: "TEST-001", article: "TEST-ARTICLE", name: "Фикус Бенджамина D12", balance: 4, price: 100 };
        return json({ items: [candidate, candidate] });
      }
      if (path === "/api/v1/admin/procurement/sales/unlinked") {
        return json({ items: [{ channel: "ozon", externalId: "fikus-benjamina-12", article: "fikus-benjamina-12", name: "Фикус Бенджамина 12 см", days: 3, units: 7, grossRub: 4900, lastSale: "2026-08-10" }] });
      }
      if (path === "/api/v1/admin/procurement/sales/link" && init?.method === "POST") {
        return json({ link: { channel: "ozon", externalId: "fikus-benjamina-12", sabyId: "TEST-SABY-1", sabyName: "Фикус Бенджамина D12", linkedRows: 3, linkedUnits: 7, takenFrom: "", remaining: 0 } });
      }
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, dashboard, procurement, orderDetail, requestError: options.requestError || "", catalogMatch: !!options.catalogMatch, dimensionReview: !!options.dimensionReview, sizeDefaults: !!options.sizeDefaults });
}

test("@desktop @phone request validates integer quantity, traps focus, archives and restores", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin?section=procurement&view=requests");
  await page.getByRole("button", { name: "Добавить", exact: true }).press("Enter");
  const dialog = page.getByRole("dialog", { name: "Добавить к закупке" });
  await dialog.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("button", {name:"Добавить",exact:true})).toBeFocused();
  await page.getByRole("button", {name:"Добавить",exact:true}).click();
  await dialog.getByLabel("Название товара", { exact: true }).fill("Проверка запроса");
  await dialog.getByLabel("Количество, шт.", { exact: true }).fill("1.5");
  await dialog.getByRole("button", { name: "Добавить", exact: true }).click();
  await expect(dialog.getByText("Введите целое число от 1 до 1 000 000")).toBeVisible();
  await expect(dialog.getByLabel("Количество, шт.", { exact: true })).toBeFocused();
  await dialog.getByLabel("Количество, шт.", { exact: true }).fill("3");
  await dialog.getByLabel("Нужно к дате").fill("2030-02-15");
  await dialog.getByLabel("Ответственный").fill("Закупщик");
  await dialog.getByRole("button", { name: "Добавить", exact: true }).press("Tab");
  await expect(dialog.getByRole("button", { name: "Закрыть", exact: true })).toBeFocused();
  await dialog.getByRole("button", { name: "Добавить", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.getByRole("row").filter({ hasText: "Проверка запроса" });
  await expect(row).toContainText("Закупщик");
  await row.getByRole("button", {name:"Изменить",exact:true}).click();
  const editDialog=page.getByRole("dialog",{name:"Редактировать запрос"});
  await editDialog.getByLabel("Ответственный").fill("Другой закупщик");
  await editDialog.getByRole("button",{name:"Сохранить",exact:true}).click();
  await expect(editDialog).toBeHidden();
  await expect(row).toContainText("Другой закупщик");
  page.once("dialog", d => d.accept());
  await row.getByRole("button", { name: "Отменить", exact: true }).click();
  await expect(row).toHaveCount(0);
  await page.getByLabel("Показать запросы").selectOption("archive");
  await expect(row).toContainText("Отменён");
  await row.getByRole("button", { name: "Вернуть в работу", exact: true }).click();
  await page.getByLabel("Показать запросы").selectOption("active");
  await expect(row).toContainText("Открыт");
});

test("@desktop request save error is visible inside the dialog and preserves inputs", async ({ page }) => {
  await mockProcurement(page, {requestError:"Сервис временно недоступен"});
  await page.goto("/admin?section=procurement&view=requests");
  await page.getByRole("button",{name:"Добавить",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"Добавить к закупке"});
  await dialog.getByLabel("Название товара",{exact:true}).fill("Сохранить введённое растение");
  await dialog.getByRole("button",{name:"Добавить",exact:true}).click();
  await expect(dialog.getByRole("alert")).toContainText("Сервис временно недоступен");
  await expect(dialog.getByRole("alert")).toBeFocused();
  await expect(dialog.getByLabel("Название товара",{exact:true})).toHaveValue("Сохранить введённое растение");
});

test("@desktop calculator keeps its inputs across tabs and creates a request", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin?section=procurement&view=calculator");
  await expect(page.getByLabel("Название", { exact: true })).toHaveCount(0);
  await page.getByLabel("Цена поставщика, €").fill("4,85");
  await page.getByLabel("Диаметр горшка, см").fill("14");
  await page.getByLabel("Высота растения, см").fill("40");
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByRole("button", { name: "Калькулятор", exact: true }).click();
  await expect(page.getByLabel("Цена поставщика, €")).toHaveValue("4,85");
  await page.getByRole("button", { name: "Создать запрос", exact: true }).click();
  await expect(page.getByRole("dialog").getByLabel("Название товара", { exact: true })).toHaveValue("");
  await expect(page.getByRole("dialog").getByLabel("Комментарий", { exact: true })).toHaveValue(/Предварительный расчёт/);
});

test("@desktop procurement direct order and filters survive reload", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin?section=procurement&view=orders&order=4&q=TEST&status=all");
  await expect(page.getByRole("dialog", { name: "TEST-100" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("dialog", { name: "TEST-100" })).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect(page.getByLabel("Поиск закупки")).toHaveValue("TEST");
  await expect(page.getByLabel("Статус закупки")).toHaveValue("all");
});

test("@desktop @phone PDF upload rejects non-PDF before sending", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin?section=procurement");
  await page.getByRole("button", { name: "Загрузить инвойс", exact: true }).click();
  await page.getByLabel("PDF-файл").setInputFiles({ name: "invoice.txt", mimeType: "text/plain", buffer: Buffer.from("not a PDF") });
  await expect(page.getByText("Выберите PDF-файл поставщика.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Загрузить и разобрать", exact: true })).toBeDisabled();
});

test("@desktop procurement opens inside the existing admin panel", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Заказ → инвойс → сверка → поступление")).toHaveCount(0);
  await expect(page.getByText("TEST-100").first()).toBeVisible();
  await page.getByText("Инвойсы и счета", { exact: false }).click();
  await expect(page.getByText("Российский счёт")).toBeVisible();
  await expect(page.getByText("Суммы сходятся")).toBeVisible();
  await page.getByText("Связи с товарами СБИС", { exact: false }).click();
  await expect(page.getByText("Тестовая строка D10")).toBeVisible();
  await expect(page.getByText("Нет связи с СБИС: 2",{exact:false})).toBeVisible();
  await page.getByRole("button", { name: "Выбрать товар СБИС", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Выбрать товар СБИС" })).toBeVisible();
  await expect(page.getByText("Тестовый товар D10")).toBeVisible();
  await expect(page.getByRole("button", { name: "Это новый товар" })).toBeVisible();
});

test("@phone procurement does not break the admin layout", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Заказ → инвойс → сверка → поступление")).toHaveCount(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  const addSupplier = page.getByRole("button", { name: "Поставщики" });
  await expect(addSupplier).toBeVisible();
  await addSupplier.click({ force: true });
  await expect(page.getByRole("dialog", { name: "Поставщики" })).toBeVisible();
});

test("@desktop supplier archive preserves history and offers restore", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Поставщики" }).click();
  await page.getByRole("button", { name: "В архив", exact:true }).first().click();

  const confirmation = page.getByRole("alertdialog", { name: "Перенести поставщика в архив?" });
  await expect(confirmation).toBeVisible();
  await expect(confirmation.getByText("Тестовый поставщик")).toBeVisible();
  await confirmation.getByRole("button", { name: "В архив", exact:true }).click();
  await expect(confirmation).toBeHidden();
  await expect(page.getByRole("dialog", { name: "Поставщики" }).getByText("Тестовый поставщик",{exact:true})).toBeVisible();
  await expect(page.getByRole("button",{name:"Восстановить",exact:true})).toBeVisible();
});

test("@desktop marketplace check shows its result immediately", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Интеграции" }).click();
  const wb = page.locator(".integration-health-grid article").filter({ hasText: "Wildberries" });
  await wb.getByRole("button", { name: "Проверить зеркало" }).click();
  await expect(wb.getByRole("status")).toHaveText("Wildberries: подключение работает");
  await expect(wb).toContainText("Успешная проверка подключения:");
  await expect(wb).toContainText("Продажи:");
});

// Продажи, не связанные с товаром, в расчёт закупки не попадают. Разбор их
// руками — единственный путь после автосвязывания, и он должен доводиться до
// конца: выбор товара обязан чинить уже загруженные строки, а не только
// будущие выгрузки.
test("@desktop unlinked marketplace sales are matched by hand", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Продажи без товара" }).click();

  await expect(page.getByText("fikus-benjamina-12").first()).toBeVisible();
  // Голый код ничего не говорит человеку, поэтому рядом стоит подпись
  // карточки, прочитанная с площадки.
  await expect(page.getByText("Фикус Бенджамина 12 см")).toBeVisible();
  await page.getByRole("button", { name: "Сопоставить", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Сопоставить продажи" });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator(".procurement-candidates article")).toHaveCount(1);
  await expect(dialog.getByText("Фикус Бенджамина D12")).toBeVisible();
  await dialog.getByRole("button", { name: "Связать" }).click();
  await expect(page.getByRole("status")).toContainText("В расчёт вернулось строк продаж: 3");
});

test("@desktop procurement blocks calculation until invoice checks pass", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.locator(".procurement-orders").getByText("TEST-100").click();
  await expect(page.getByText("Нужно проверить")).toBeVisible();
  await expect(page.getByText("Не выбраны товары СБИС: 2")).toBeVisible();
  await expect(page.getByRole("button", { name: "Рассчитать" })).toBeDisabled();
});

test("@desktop procurement opens an order with no validation blockers", async ({ page }) => {
  await mockProcurement(page, { blockers: null, currency: "EUR" });
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.locator(".procurement-orders").getByText("TEST-100").click();

  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByText("Проверки пройдены")).toHaveCount(0);

  const delivery = page.getByLabel("Москва → Рязань, ₽");
  await delivery.fill("");
  await delivery.pressSequentially("14000");
  await expect(delivery).toHaveValue("14000");
});

test("@desktop procurement directory includes active products without a Saby code", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Товары", exact: true }).click();
  await expect(page.getByText("Новый товар без кода СБИС")).toBeVisible();
  await expect(page.getByText("WB-22")).toBeVisible();
  await expect(page.getByText("OZON-22")).toBeVisible();
});

test("@desktop invoice can be attached from the saved order", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.locator(".procurement-orders").getByText("TEST-100").click();
  await expect(page.getByText("Нужно проверить")).toBeVisible();
  await expect(page.getByText("Загрузите инвойс",{exact:true})).toBeVisible();
  await page.getByLabel("Заменить инвойс").setInputFiles({ name: "invoice.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4 test") });
  await expect(page.getByText("Не выбраны товары СБИС: 2")).toBeVisible();
});

test("@desktop procurement shows one markup and clear rounding settings", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Формула цены" }).click();

  await expect(page.getByLabel("Стартовая наценка на себестоимость, %")).toHaveValue("110");
  await expect(page.getByLabel("Менять цену при отклонении более, %")).toHaveValue("10");
  await expect(page.getByLabel("Округлять цены до ближайших 50 или 90")).toBeChecked();
  await expect(page.getByLabel("База Голландии")).toHaveCount(0);
  await expect(page.locator(".procurement-settings-grid").getByLabel("Объём телеги, см³")).toHaveCount(0);
});

test("@desktop procurement separates actionable and already ordered recommendations", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать" }).click();

  await expect(page.getByText("Тестовый товар D10")).toBeVisible();
  await expect(page.getByText("12 шт.")).toBeVisible();
  await page.getByRole("button", { name: "Уже заказано 1" }).click();
  await expect(page.getByText("Товар уже едет")).toBeVisible();
  await expect(page.getByText("повторная закупка исключена")).toBeVisible();
});

test("@desktop procurement filters, selects visible rows, and assigns a supplier", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();

  await expect(page.getByRole("button", { name: "Выделить все видимые" })).toBeDisabled();
  await page.getByLabel("Фильтр по количеству").selectOption("10");
  await expect(page.getByText(/1 позиция · выбрано/)).toBeVisible();
  await page.getByLabel("Фильтр по поставщику").selectOption("1");
  await page.getByRole("button", { name: "Выделить все видимые" }).click();
  await expect(page.getByLabel("Выбрать Тестовый товар D10")).toBeChecked();
  await expect(page.getByRole("button", { name: "Сформировать заказ", exact: true })).toBeEnabled();

  await page.getByLabel("Поставщик для Тестовый товар D10").selectOption("2");
  await expect(page.getByRole("button", { name: "Сформировать заказ", exact: true })).toBeDisabled();
  await page.getByLabel("Фильтр по поставщику").selectOption("2");
  await expect(page.getByLabel("Поставщик для Тестовый товар D10")).toHaveValue("2");
});

test("@desktop @phone procurement fullscreen plan stays on screen and accepts rows", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  await expect(dialog).toBeVisible();
  const viewport = page.viewportSize()!;
  const bounds = await dialog.boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(viewport.height + 1);
  await expect(dialog.getByRole("heading", { name: "Новый заказ поставщику" })).toBeInViewport();
  await expect(page.getByLabel("Рекомендации к закупке", { exact: true })).toBeHidden();
  if (viewport.width > 800) await expect(dialog.getByRole("columnheader", { name: "Название у поставщика", exact: true })).toBeVisible();
  await expect(dialog.getByText("Заказ пока пуст", { exact: true })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "+ Из рекомендаций", exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel("Название у поставщика",{exact:true})).toHaveCount(1);
  await expect(dialog.getByText("1 позиция", { exact: false })).toBeVisible();
  const addRow = dialog.getByRole("button", { name: "+ Новая строка", exact: true });
  await expect(addRow).toBeInViewport();
  await expect(dialog.getByLabel("Название у поставщика",{exact:true})).toBeVisible();
  await dialog.getByLabel("Количество упаковок", { exact: true }).fill("2");
  await dialog.getByLabel("Штук в упаковке", { exact: true }).fill("12");
  await expect(dialog.getByText("1 позиция", { exact: false })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Заказ отправлен поставщику →", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "Заказ отправлен поставщику →", exact: true })).toBeInViewport();
});

test("@desktop procurement recommendation keeps category and purchasing context in the order", async ({ page }) => {
  await mockProcurement(page, { anotherRecommendation: true });
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  await dialog.getByRole("button",{name:"Каталог и рекомендации",exact:true}).click();
  const drawer = page.getByLabel("Рекомендации к закупке", { exact: true });
  const drawerBounds = await drawer.boundingBox();
  const viewport = page.viewportSize()!;
  expect(drawerBounds).not.toBeNull();
  expect(drawerBounds!.x + drawerBounds!.width).toBeGreaterThanOrEqual(viewport.width - 1);
  expect(drawerBounds!.y).toBe(0);
  expect(drawerBounds!.height).toBeGreaterThanOrEqual(viewport.height - 1);
  await expect(drawer.getByText("Олива D18", { exact: true })).toBeVisible();
  await expect(drawer.getByText("Все рекомендации добавлены", { exact: true })).toHaveCount(0);

  await expect(dialog.locator("tbody tr")).toHaveCount(1);
  await expect(dialog.locator('input[value="Цитрус"]')).toBeVisible();
  await expect(dialog.getByText("Тестовый товар D10", { exact: true })).toBeVisible();
  await expect(dialog.getByText("Рекомендовано: 12 шт.", { exact: true })).toBeVisible();
  await expect(dialog.locator('input[value="SUP-1"]')).toBeVisible();
  await expect(dialog.getByLabel("Горшок, см", { exact: true })).toHaveValue("12");
  await expect(dialog.getByLabel("Высота, см", { exact: true })).toHaveValue("35");
  await expect(dialog.getByLabel("Штук в упаковке", { exact: true })).toHaveValue("6");
  await expect(dialog.getByLabel("Цена в рублях", { exact: true })).toHaveValue("5,3");
  await expect(dialog.getByText("31,80 ₽", { exact: true })).toBeVisible();
  await expect(dialog.locator("tbody tr").first().getByText("Остаток: 2 шт.", { exact: true })).toBeVisible();
  expect((await dialog.locator(".procurement-plan-table-wrap").boundingBox())!.height).toBeGreaterThan(200);
});

test("@desktop saved procurement draft is shared and a new recommendation starts fresh", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  await dialog.getByLabel("Название у поставщика",{exact:true}).fill("Цитрус");
  await dialog.getByPlaceholder("Артикул", { exact: true }).fill("NL-42");
  await dialog.getByLabel("Название плана").click();
  await expect(dialog.getByText(/Сохранено для команды/)).toBeVisible();
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).first().click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByPlaceholder("Артикул", { exact: true })).toHaveValue("SUP-1");
  await page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByRole("button", { name: "Закрыть", exact: true }).first().click();
  await page.getByRole("button", { name: "Планы", exact: false }).last().click();
  await page.getByRole("button", { name: "Продолжить" }).first().click();
  await expect(page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByLabel("Название у поставщика",{exact:true})).toHaveValue("Цитрус");
  await expect(page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByPlaceholder("Артикул", { exact: true })).toHaveValue("NL-42");
  await page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByPlaceholder("Артикул", { exact: true }).fill("NL-43");
  await page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByRole("button", { name: "Закрыть", exact: true }).first().click();
  await page.getByRole("button", { name: "Продолжить" }).first().click();
  await expect(page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByPlaceholder("Артикул", { exact: true })).toHaveValue("NL-43");
});

test("@desktop procurement saves and sorts after leaving the edited row", async ({ page }) => {
  await mockProcurement(page, { anotherRecommendation: true });
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  await expect(dialog.getByRole("columnheader", { name: "Остаток СБИС", exact: true })).toHaveCount(0);
  await expect(dialog.locator("tbody tr").first().getByText("Остаток: 2 шт.")).toBeVisible();
  await dialog.getByRole("button",{name:"Каталог и рекомендации",exact:true}).click();
  await dialog.getByLabel("Рекомендации к закупке").getByRole("button", { name: "+ Добавить" }).click();
  await dialog.getByRole("button",{name:"Закрыть рекомендации",exact:true}).click();
  await expect(dialog.locator("tbody tr")).toHaveCount(2);
  const firstPrice = dialog.locator("tbody tr").first().getByLabel("Цена в рублях");
  await firstPrice.fill("20");
  await page.waitForTimeout(500);
  await expect(dialog.locator("tbody tr").first().getByText("Тестовый товар D10")).toBeVisible();
  await expect(dialog.getByText("Сохраним при выходе")).toBeVisible();
  await dialog.getByLabel("Название плана").click();
  await expect(dialog.locator("tbody tr").first().getByText("Олива D18")).toBeVisible();
  await expect(dialog.getByText(/Сохранено для команды/)).toBeVisible();
  const costCell = dialog.locator("tbody tr").last().locator(".procurement-cost-cell");
  expect((await costCell.boundingBox())!.height).toBeLessThan(90);
});

test("@desktop procurement prices use a comma without trailing zeros", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  const price = dialog.getByLabel("Цена в рублях");
  await expect(price).toHaveValue("5,3");
  await price.fill("4.2500");
  await expect(price).toHaveValue("4.2500");
  await dialog.getByLabel("Название плана").click();
  await expect(price).toHaveValue("4,25");
  await expect(dialog.getByText(/Сохранено для команды/)).toBeVisible();
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).first().click();
  await page.getByRole("button", { name: "Планы", exact: false }).last().click();
  await page.getByRole("button", { name: "Продолжить" }).first().click();
  const reopenedPrice = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).getByLabel("Цена в рублях");
  await expect(reopenedPrice).toHaveValue("4,25");
  await reopenedPrice.fill("0,0001");
  await dialog.getByLabel("Название плана").click();
  await expect(reopenedPrice).toHaveValue("0,0001");
});

test("@desktop procurement touched rows highlight automatically and survive reopening a plan", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  const row = dialog.locator("tbody tr").first();
  await expect(row).not.toHaveClass(/procurement-row-touched/);
  await row.click();
  await expect(row).toHaveClass(/procurement-row-touched/);
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).first().click();
  await page.getByRole("button", { name: "Планы", exact: false }).last().click();
  await page.getByRole("button", { name: "Продолжить" }).first().click();
  const reopenedRow = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true }).locator("tbody tr").first();
  await expect(reopenedRow).toHaveClass(/procurement-row-touched/);
  await reopenedRow.getByLabel("Цена в рублях").fill("6,7");
  await expect(reopenedRow).toHaveClass(/procurement-row-touched/);
});

test("@desktop procurement can add a linked Saby product outside recommendations", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки", exact: true }).click();
  await page.getByRole("button", { name: "Что заказать", exact: true }).click();
  await page.getByLabel("Выбрать Тестовый товар D10").check();
  await page.getByRole("button", { name: "Сформировать заказ", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "Новый заказ поставщику", exact: true });
  await dialog.getByRole("button",{name:"Каталог и рекомендации",exact:true}).click();
  const drawer = page.getByLabel("Рекомендации к закупке", { exact: true });
  await drawer.getByPlaceholder("Название, код или артикул").fill("Bonsai Zantaxilum");
  await expect(drawer.getByText("Bonsai Zantaxilum D15", { exact: true })).toBeVisible();
  await expect(drawer.getByText("Остаток 3", { exact: true })).toBeVisible();
  await expect(drawer.getByText("X616872557", { exact: true })).toBeVisible();
  await expect(drawer.getByText("Инвойсы Голландии", { exact: true })).toBeVisible();
  await expect(drawer.getByText("Продажи и рекомендация подтянутся после добавления товара.", { exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "+ Добавить", exact: true }).click();

  await expect(dialog.locator("tbody tr")).toHaveCount(2);
  await expect(dialog.getByText("Bonsai Zantaxilum D15", { exact: true })).toBeVisible();
  await expect(dialog.getByText("2 позиции", { exact: false })).toBeVisible();
});

test("@desktop @phone invoice picker stays in viewport from a scrolled row and restores focus", async ({ page }) => {
  await mockProcurement(page, { invoicePicker: true });
  await page.goto("/admin?section=procurement&view=orders&order=4");
  const order = page.getByRole("dialog", { name: "TEST-100", exact: true });
  await expect(order.getByRole("tabpanel")).toBeVisible();
  const more = order.getByRole("button", { name: "Показать ещё строки закупки", exact: true });
  while (await more.count()) await more.click();
  const trigger = order.getByRole("button", { name: "Выбрать из инвойса", exact: true });
  await trigger.scrollIntoViewIfNeeded();
  const table = order.locator(".procurement-order-body");
  const scroll = await table.evaluate(element => element.scrollTop);
  expect(scroll).toBeGreaterThan(300);
  await trigger.press("Enter");
  const picker = page.getByRole("dialog", { name: "Выбрать строки инвойса", exact: true });
  await expect(picker).toBeVisible();
  await expect(picker).toBeFocused();
  const bounds = await picker.boundingBox();
  const viewport = page.viewportSize()!;
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(viewport.height);
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width);
  await expect(picker.getByRole("heading", { name: "Выбрать строки инвойса" })).toBeInViewport();
  await expect(picker.getByRole("button", { name: "Отмена", exact: true })).toBeInViewport();
  await picker.getByRole("button", { name: "Отмена", exact: true }).press("Tab");
  await expect(picker.getByRole("button", { name: "Закрыть выбор строки инвойса", exact: true })).toBeFocused();
  await picker.press("Escape");
  await expect(picker).toBeHidden();
  await expect(order).toBeVisible();
  await expect(trigger).toBeFocused();
  expect(await table.evaluate(element => element.scrollTop)).toBeCloseTo(scroll, 0);
  const lastRow=order.locator(".procurement-order-lines").getByRole("row").filter({hasText:"Товар 60"});
  await lastRow.locator(".procurement-row-more > summary").click();
  await lastRow.getByRole("button", { name: "Сменить строку инвойса", exact: true }).click();
  await expect(picker).toBeVisible();
  await expect(picker.getByRole("heading")).toBeInViewport();
  await picker.getByRole("button", { name: "Отмена", exact: true }).click();
  await trigger.click();
  await picker.getByRole("button", { name: "Сопоставить", exact: true }).first().click();
  await expect(picker).toBeHidden();
  await expect(order.getByRole("row").filter({ hasText: "Нижняя строка без инвойса" })).toContainText("Added invoice 1");
});


test("@desktop @phone procurement uses quiet checks, styled controls and a single dimension form",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,blockers:[]});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await expect(order.getByText('Проверки пройдены',{exact:true})).toHaveCount(0);
 await expect(order.getByText('Артикул в PDF не указан',{exact:true})).toHaveCount(0);
 await order.getByRole('tab',{name:'Только в закупке 1',exact:true}).click();
 const row=order.getByRole('row').filter({hasText:'Нижняя строка без инвойса'});
 await row.locator('.procurement-row-more > summary').click();
 await row.getByRole('button',{name:'Исправить размеры',exact:true}).press("Enter");
 const editor=page.getByRole('dialog',{name:'Размеры и размещение',exact:true});
 await expect(editor).toBeVisible();
 await editor.getByLabel('Высота растения, см').fill('0');
 await expect(editor.getByRole('button',{name:'Сохранить',exact:true})).toBeDisabled();
 await editor.getByLabel('Диаметр горшка, см').fill('15,5');
 await editor.getByLabel('Высота растения, см').fill('45');
 await editor.getByLabel('Телега / коробка').fill('2');
 const save=editor.getByRole('button',{name:'Сохранить',exact:true});
 expect(await save.evaluate(el=>getComputedStyle(el).borderRadius)).not.toBe('0px');
 const rect=await editor.boundingBox();expect(rect!.y).toBeGreaterThanOrEqual(0);expect(rect!.y+rect!.height).toBeLessThanOrEqual(page.viewportSize()!.height);
 await save.click();await expect(editor).toBeHidden();
 await expect(row).toContainText('D15.5 · 45 см');
 await expect(row.getByRole('button',{name:'Исправить размеры',exact:true})).toBeFocused();
 await order.locator(".procurement-order-final > details > summary").press("Tab");
 await expect(order.getByRole("button",{name:"Закрыть",exact:true})).toBeFocused();
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});


test("@desktop @phone purchase tabs switch groups, keep totals and return to all",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,receivingTabs:true,blockers:[]});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 const tabs=order.getByRole('tablist',{name:'Строки закупки'});
 const all=tabs.getByRole('tab',{name:'Все 91',exact:true});
 await expect(all).toHaveAttribute('aria-selected','true');
 await expect(tabs.getByRole('tab')).toHaveCount(5);
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(5);
 await order.getByRole('button',{name:'Показать ещё строки закупки',exact:true}).click();
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(10);
 await tabs.getByRole('tab',{name:'Разобрать миксы 1',exact:true}).click();
 await expect(order.locator('.procurement-order-lines > tbody > tr')).toHaveCount(1);
 await expect(order.getByRole('tabpanel')).toContainText('Товар 1');
 await expect(order.getByRole('tabpanel')).not.toContainText('Товар 4');
 await tabs.getByRole('tab',{name:'Только в инвойсе 30',exact:true}).click();
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(5);
 await expect(order.getByRole('button',{name:'Это новая позиция',exact:true})).toHaveCount(5);
 await order.getByLabel('Поиск строки закупки').fill('Added invoice 1');
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(5);
 await expect(tabs.getByRole('tab',{name:'Только в инвойсе 30',exact:true})).toHaveAttribute('aria-selected','true');
 await tabs.getByRole('tab',{name:'Только в закупке 1',exact:true}).click();
 await expect(order.getByLabel('Поиск строки закупки')).toHaveValue('');
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(1);
 await expect(order.getByRole('button',{name:'Выбрать из инвойса',exact:true})).toBeVisible();
 await tabs.getByRole('tab',{name:'Передать клиенту 1',exact:true}).click();
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(1);
 await expect(order.getByRole('tabpanel')).toContainText('Товар 2');
 await expect(order.getByRole('tabpanel')).not.toContainText('Товар 3');
 await tabs.getByRole('tab',{name:'Передать клиенту 1',exact:true}).press('ArrowRight');
 await expect(all).toBeFocused();
 await expect(all).toHaveAttribute('aria-selected','true');
 await expect(order.getByRole('tabpanel').locator('tbody > tr')).toHaveCount(5);
 await expect(all).toBeInViewport();
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});


test("@desktop @phone product pricing keeps market retail and Ozon FBS costs after reload",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,catalogMatch:true});
 await page.goto("/admin?section=procurement&order=4");
 const order=page.getByRole("dialog",{name:"TEST-100",exact:true});
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 const row=order.getByRole("row").filter({hasText:"Бонсай Зантоксилум D15"});
 await row.locator(".procurement-row-more > summary").click();
 await row.getByRole("button",{name:"Расходы продажи",exact:true}).click();
 const editor=page.getByRole("dialog",{name:"Цена и расходы продажи",exact:true});
 await editor.getByLabel("Розничная цена после проверки рынка, ₽").fill("1650");
 await editor.locator("summary").filter({hasText:"Ozon · FBS"}).click();
 await editor.getByLabel("Отдельные расходы этого товара").last().check();
 await editor.getByLabel("Цена Ozon после проверки рынка, ₽").fill("2700");
 await editor.getByLabel("Комиссия, %",{exact:true}).fill("52");
 await editor.getByLabel("Эквайринг, %",{exact:true}).fill("1");
 await editor.getByLabel("Упаковка, ₽",{exact:true}).fill("150");
 await editor.getByLabel("Логистика, последняя миля, обработка, ₽",{exact:true}).fill("200");
 await editor.getByLabel("Потери, возвраты и прочие затраты, ₽",{exact:true}).fill("30");
 await editor.getByRole("button",{name:"Сохранить для пересчёта",exact:true}).click();
 await expect(editor).toBeHidden();
 await page.reload();
 await order.getByRole("textbox",{name:"Поиск строки закупки",exact:true}).fill("BONSAI");
 const reloaded=page.getByRole("dialog",{name:"TEST-100",exact:true}).getByRole("row").filter({hasText:"Бонсай Зантоксилум D15"});
 await reloaded.locator(".procurement-row-more > summary").click();
 await reloaded.getByRole("button",{name:"Расходы продажи",exact:true}).click();
 await expect(editor.getByLabel("Розничная цена после проверки рынка, ₽")).toHaveValue("1650");
 await editor.locator("summary").filter({hasText:"Ozon · FBS"}).click();
 await expect(editor.getByLabel("Цена Ozon после проверки рынка, ₽")).toHaveValue("2700");
 await expect(editor.getByLabel("Комиссия, %",{exact:true})).toHaveValue("52");
 await expect(editor.getByLabel("Логистика, последняя миля, обработка, ₽",{exact:true})).toHaveValue("200");
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone pricing shows monthly costs and leaves unknown marketplace charges blank",async({page})=>{
 await mockProcurement(page);
 await page.goto("/admin?section=procurement&view=settings");
 await expect(page.getByLabel("Расходы в месяц, ₽",{exact:true})).toHaveValue("300000");
 const channels=page.locator("details.procurement-explanation").filter({has:page.locator("summary",{hasText:"FBS"})});
 await expect(channels).toHaveCount(2);
 for(const channel of await channels.all()) {
  await expect(channel.getByLabel("Комиссия, %",{exact:true})).toHaveValue("");
  await expect(channel.getByLabel("Логистика, последняя миля, обработка, ₽",{exact:true})).toHaveValue("");
 }
 await expect(page.getByLabel("Резерв, %",{exact:true})).toHaveCount(0);
 await expect(page.getByLabel("Логистика маркетплейса, ₽ за см высоты",{exact:true})).toHaveCount(0);
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});


test("@desktop @phone dimension blockers lead to explicit review and unblock calculation",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,currency:"EUR",dimensionReview:true});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 const calculate=order.getByRole('button',{name:'Рассчитать',exact:true});
 await expect(calculate).toBeDisabled();
 await expect(calculate).toHaveAccessibleDescription(/Размеры в 2 поз/);
 await expect(order.locator('.procurement-checklist li')).toHaveCount(0);
 await order.getByRole('textbox',{name:'Курс оплаты',exact:true}).fill('123');
 await order.getByRole('button',{name:'Проверить размеры (2)',exact:true}).click();
 await expect(order.getByRole('tab',{name:'Проверить размеры 2',exact:true})).toBeFocused();
 await expect(order.locator('.procurement-order-lines > tbody > tr')).toHaveCount(2);
 const first=order.getByRole('row').filter({hasText:'Бонсай D10'});
 await first.getByRole('button',{name:'Проверить размеры',exact:true}).click();
 const editor=page.getByRole('dialog',{name:'Проверить размеры',exact:true});
 await expect(editor).toContainText('Инвойс: D12, H20 см. Карточка: D10.');
 await editor.getByRole('button',{name:'Отмена',exact:true}).click();
 await expect(order.getByRole('tab',{name:'Проверить размеры 2',exact:true})).toBeVisible();
 await first.getByRole('button',{name:'Проверить размеры',exact:true}).click();
 await editor.getByRole('button',{name:'Использовать размеры инвойса',exact:true}).click();
 await expect(calculate).toBeDisabled();
 await editor.getByLabel('Высота растения, см').fill('21');
 await editor.getByRole('button',{name:'Подтвердить размеры',exact:true}).click();
 await expect(editor).toBeHidden();
 await expect(order.getByRole('tab',{name:'Проверить размеры 1',exact:true})).toBeVisible();
 await expect(order.getByRole('textbox',{name:'Курс оплаты',exact:true})).toHaveValue('123');
 const second=order.getByRole('row').filter({hasText:'Непентес микс'});
 await expect(second.getByRole('button',{name:'Выбрать товар СБИС',exact:true})).toHaveCount(0);
 await second.getByRole('button',{name:'Проверить размеры',exact:true}).click();
 await expect(editor.getByLabel('Диаметр горшка, см')).toHaveValue('12');
 await editor.getByRole('button',{name:'Использовать размеры инвойса',exact:true}).click();
 await expect(editor.getByLabel('Диаметр горшка, см')).toHaveValue('9');
 await expect(editor.getByLabel('Высота растения, см')).toHaveValue('20');
 await editor.getByRole('button',{name:'Подтвердить размеры',exact:true}).click();
 await expect(editor).toBeHidden();
 await expect(order.getByRole('tab',{name:'Проверить размеры 0',exact:true})).toHaveAttribute('aria-selected','true');
 await expect(calculate).toBeEnabled();
 await expect(order.locator('.procurement-checklist')).toHaveCount(0);
 await page.reload();
 await expect(calculate).toBeEnabled();
 await expect(order.getByRole('tab',{name:/^Проверить размеры /})).toHaveCount(0);
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});


test("@desktop @phone dimension review keeps arithmetic blockers visible",async({page})=>{
 await mockProcurement(page,{invoicePicker:true,currency:"EUR",dimensionReview:true,blockers:["Итог документа не совпадает с суммой строк"]});
 await page.goto('/admin?section=procurement&order=4');
 const order=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await order.getByRole('button',{name:'Проверить размеры (2)',exact:true}).click();
 for(let i=0;i<2;i++) {
   await order.getByRole('button',{name:'Проверить размеры',exact:true}).first().click();
   const editor=page.getByRole('dialog',{name:'Проверить размеры',exact:true});
   await editor.getByRole('button',{name:'Подтвердить размеры',exact:true}).click();
   await expect(editor).toBeHidden();
   await expect(order.getByRole('tab',{name:`Проверить размеры ${1-i}`,exact:true})).toBeVisible();
 }
 await expect(order.getByRole('button',{name:'Рассчитать',exact:true})).toBeDisabled();
 await expect(order.getByRole('button',{name:'Рассчитать',exact:true})).toHaveAccessibleDescription(/Итог документа не совпадает/);
 await expect(order.locator('.procurement-checklist li')).toHaveText(['Итог документа не совпадает с суммой строк']);
});

 test("@desktop @phone unified prices are searchable and do not send prices",async({page})=>{
 await mockProcurement(page,{unified:true});
 await page.goto('/admin?section=procurement&order=4');
 const dialog=page.getByRole('dialog',{name:'TEST-100',exact:true});
 const preview=dialog.locator('details').filter({has:page.getByText('Цены по общей формуле',{exact:true})});
 await expect(preview).toContainText('Рассчитано для 3 из 3 растений');
 await expect(preview.getByRole('button',{name:/Отправить|Применить/})).toHaveCount(0);
 await expect(preview).toContainText('1 705');
 await preview.getByRole('searchbox',{name:'Найти растение в расчёте'}).fill('absent');
 await expect(preview).toContainText('Растения не найдены');
 await preview.getByRole('searchbox',{name:'Найти растение в расчёте'}).fill('Unified');
 await expect(preview.getByRole('row').filter({hasText:'Unified plant D12'})).toHaveCount(1);
 for(const value of ['1 705','2 191']){
  const box=await preview.getByRole('cell').filter({hasText:value}).boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x+box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
 }
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
 });

 test("@desktop unified settings show the active markup and hide retired rounding",async({page})=>{
 await mockProcurement(page,{unified:true});
 await page.goto('/admin?section=procurement');
 await page.getByRole('button',{name:'Формула цены',exact:true}).click();
 await expect(page.getByLabel('Заработок от себестоимости, %')).toHaveValue('60');
 await expect(page.getByLabel('Стартовая наценка на себестоимость, %')).toHaveCount(0);
 await expect(page.getByLabel('Округлять цены до ближайших 50 или 90')).toHaveCount(0);
 const wb=page.locator('details').filter({has:page.getByText('Wildberries · FBS',{exact:true})});
 await expect(wb.getByLabel('Упаковка, ₽',{exact:true})).toHaveCount(0);
 await expect(page.getByLabel('Материалы на отправление, ₽')).toHaveValue('100');
 });

 test("@desktop @phone completed supply prepares only the selected unified channel",async({page})=>{
 await mockProcurement(page,{unified:true,unifiedReceived:true});
 await page.goto('/admin?section=procurement&order=4');
 const dialog=page.getByRole('dialog',{name:'TEST-100',exact:true});
 await dialog.locator('summary').filter({hasText:'Расчёт и доставка'}).click();
 await expect(dialog.getByRole('button',{name:'Рассчитать',exact:true})).toBeDisabled();
 await dialog.getByRole('button',{name:'Подготовить Ozon',exact:true}).click();
 const batch=dialog.locator('.procurement-batch').filter({has:page.getByRole('heading',{name:'Изменение цены · Ozon',exact:true})});
 await expect(batch).toContainText('Unified plant D12');
 await expect(batch).toContainText('2 191');
 await expect(batch).toContainText('Черновик');
 await expect(batch.getByRole('button',{name:'Подтвердить 1 строк',exact:true})).toBeEnabled();
 await expect(dialog.locator('.procurement-batch')).toHaveCount(1);
 await expect(dialog.getByText('Завершено',{exact:true})).toBeVisible();
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
 });


test("@desktop @phone first Saby file prices include hidden new rows after explicit review",async({page},testInfo)=>{
 await mockProcurement(page,{firstSabyPrices:true});
 await page.goto('/admin?section=procurement&order=4');
 const button=page.getByRole('button',{name:'Скачать цены для СБИС',exact:true});
 await expect(button).toBeVisible();
 const rejected=page.waitForEvent('dialog');
 const click=button.click();
 const review=await rejected;
 expect(review.message()).toContain('Новый товар 8: 643');
 await review.dismiss();await click;
 expect(await page.evaluate(()=>Reflect.get(window,'sabyExports'))).toEqual([]);
 const accepted=page.waitForEvent('dialog');
 const download=page.waitForEvent('download');
 const exportClick=button.click();
 await (await accepted).accept();await exportClick;
 expect((await download).suggestedFilename()).toBe('first-prices.xlsx');
 expect(await page.evaluate(()=>Reflect.get(window,'sabyExports'))).toEqual([{method:'POST',body:{firstPrices:Array.from({length:8},(_,index)=>({sabyId:`NEW-${index+1}`,priceRub:636+index}))}}]);
 await page.screenshot({path:testInfo.outputPath('first-saby-prices.png'),fullPage:true});
});

test('@desktop @phone поиск размера при закупке заполняет выбранный вариант',async({page},testInfo)=>{
 await mockProcurement(page,{sizeDefaults:true});
 await page.goto('/admin?section=procurement&view=recommendations');
 await page.getByLabel('Выбрать Тестовый товар D10').check();
 await page.getByRole('button',{name:'Сформировать заказ',exact:true}).click();
 const d=page.getByRole('dialog',{name:'Новый заказ поставщику',exact:true});
 const picker=d.getByRole('combobox',{name:'Размер для Тестовый товар D10',exact:true});
 await picker.fill('SIZE-LARGE');
 await expect(d.getByRole('listbox',{name:'Найденные товары'}).getByRole('option')).toHaveCount(1);
 await page.screenshot({path:testInfo.outputPath('size-search.png')});
 await d.getByRole('listbox',{name:'Найденные товары'}).getByRole('option').click();
 await expect(d.getByLabel('Горшок, см',{exact:true})).toHaveValue('17');
 await expect(d.getByLabel('Высота, см',{exact:true})).toHaveValue('70');
 await expect(d.getByLabel('Штук в упаковке',{exact:true})).toHaveValue('8');
 await expect(d.locator('input[value="SIZE-LARGE"]')).toBeVisible();
});

test("@desktop @phone calculator infers source from edited amount and preserves parcel dimensions", async ({page}) => {
 await mockProcurement(page);
 await page.goto('/admin?section=procurement&view=calculator');
 await page.evaluate(()=>{
  const w=window as unknown as {calculatorInputs: Record<string,unknown>[]}; w.calculatorInputs=[];
  const original=window.fetch.bind(window);
  window.fetch=async (url,init)=>{
   if(String(url).includes('/pricing/preview')) {
    const input=JSON.parse(String(init?.body)); w.calculatorInputs.push(input);
    const retail=input.mode==='retail'?input.retailRub:1490;
    const q={priceRub:2490,contributionRub:600,missing:[]};
    return new Response(JSON.stringify({retailRub:retail,quotes:{site:{...q,priceRub:retail},wb:q,ozon:q}}),{headers:{'Content-Type':'application/json'}});
   }
   return original(url,init);
  };
 });
 const lastInput=()=>page.evaluate(()=>(window as unknown as {calculatorInputs: Record<string,unknown>[]}).calculatorInputs.at(-1));
 const calc=page.getByRole('region',{name:'Калькулятор цен'});
 await expect(calc.locator('input')).toHaveCount(3);
 await page.screenshot({path:`../../calculator-initial-${test.info().project.name}.png`,fullPage:true});
 await expect(calc.getByLabel('Курс, ₽/€')).toHaveCount(0);
 await calc.getByLabel('Цена поставщика, €').fill('4,85');
 await calc.getByLabel('Диаметр горшка, см').fill('14');
 await calc.getByLabel('Высота растения, см').fill('40');
 await calc.getByLabel('Длина коробки, см').fill('15');
 await calc.getByLabel('Ширина коробки, см').fill('15');
 await calc.getByLabel('Высота коробки, см').fill('50');
 await expect.poll(lastInput).toBeTruthy();
 await expect(calc.getByRole('combobox')).toHaveCount(0);
 await calc.getByLabel('Себестоимость, ₽').fill('1000,25');
 await expect(calc.getByLabel('Диаметр горшка, см')).toHaveCount(0);
 await calc.getByLabel('Наценка, %').fill('75');
 await expect.poll(async()=>(await lastInput())?.costRub).toBe(1000.25);
 await expect.poll(async()=>(await lastInput())?.markupRate).toBe(.75);
 await expect(calc.getByLabel('Цена в магазине, ₽')).toHaveValue('1490');
 await expect(calc.getByLabel('Цена WB, ₽')).toHaveCount(0);
 await expect(calc.getByLabel('Цена Ozon, ₽')).toHaveCount(0);
 await expect(calc.locator('aside')).toContainText('Wildberries');
 await expect(calc.locator('aside')).toContainText('Ozon');
 await calc.getByLabel('Цена в магазине, ₽').fill('2000,37');
 await expect.poll(async()=>(await lastInput())?.retailRub).toBe(2000.37);
 expect((await lastInput())?.costRub).toBeUndefined();
 await expect(calc.getByText(/остаётся .* с продажи/)).toHaveCount(0);
 await expect(calc.getByLabel('Наценка, %')).toHaveCount(0);
 await expect(calc.getByLabel('Диаметр горшка, см')).toHaveCount(0);
 await expect(calc.getByLabel('Высота растения, см')).toHaveCount(0);
 await expect(calc.getByLabel('Длина коробки, см')).toHaveValue('15');
 await expect(calc.getByRole('button',{name:'Копировать расчёт'})).toBeEnabled();
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
 await page.screenshot({path:`../../calculator-${test.info().project.name}.png`,fullPage:true});
 await page.getByRole('button',{name:'Что заказать',exact:true}).click();
 await page.getByRole('button',{name:'Калькулятор',exact:true}).click();
 await expect(calc.getByLabel('Цена в магазине, ₽')).toHaveValue('2000,37');
 await expect(calc.getByLabel('Высота коробки, см')).toHaveValue('50');
 await calc.getByLabel('Цена поставщика, €').fill('4,85');
 await expect(calc.getByLabel('Диаметр горшка, см')).toBeEnabled();
 await expect(calc.getByLabel('Высота растения, см')).toHaveValue('40');
 await calc.getByLabel('Себестоимость, ₽').fill('1000,25');
 await expect(calc.getByLabel('Диаметр горшка, см')).toHaveCount(0);
 await expect(calc.getByLabel('Наценка, %')).toBeEnabled();
});
