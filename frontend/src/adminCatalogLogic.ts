import type { Category, CategoryAttribute, Product } from "./adminTypes";

export type ReadinessCode = "variants_missing" | "no_photo" | "no_description" | "invalid_category" | "stage1_missing" | "latin_name_missing" | "care_missing" | "unknown_enum" | "draft" | "ready" | "published";
export type ProductReadiness = { tasks: Array<{label:string;section:"main"|"attributes"|"variants"|"care"|"photos"}>; codes: ReadinessCode[]; blockers: ReadinessCode[]; missingStage1: string[]; unknownEnums: string[] };

export const readinessLabels: Record<ReadinessCode, string> = {
  variants_missing:"Проверьте размеры и цены", no_photo: "Нет основного фото", no_description: "Нет описания",
  invalid_category: "Некорректная категория", stage1_missing: "Не заполнены характеристики",
  latin_name_missing: "Нет латинского названия", care_missing: "Не заполнен уход",
  unknown_enum: "Есть неизвестные значения", draft: "Черновик",
  ready: "Черновики без замечаний", published: "Опубликовано",
};

export function rootCategory(categories: Category[], categoryId?: number) {
  let category = categories.find((item) => item.id === categoryId);
  const visited = new Set<number>();
  while (category?.parentId && !visited.has(category.id)) {
    visited.add(category.id);
    category = categories.find((item) => item.id === category?.parentId);
  }
  return category;
}

export function isPlantProduct(product: Pick<Product, "categoryId" | "catalogSection">, categories: Category[]) {
  const root = rootCategory(categories, product.categoryId);
  return root ? root.slug === "plants" : product.catalogSection === "plants";
}

export function valueMissing(value: unknown) {
  return value == null || value === "" || (Array.isArray(value) && value.length === 0);
}

export function readinessFor(product: Product, categories: Category[], schema: CategoryAttribute[]): ProductReadiness {
  const codes: ReadinessCode[] = [];
  const missingStage1 = schema.filter((item) => item.scope === "product" && (item.keyCharacteristic || item.required) && valueMissing(product.attributes?.[item.code])).map((item) => item.name);
  const unknownEnums = schema.filter((item) => item.scope === "product" && (item.dataType === "enum" || item.dataType === "multi_enum")).filter((item) => {
    const raw = product.attributes?.[item.code];
    const values = Array.isArray(raw) ? raw.map(String) : valueMissing(raw) ? [] : [String(raw)];
    return values.some((value) => !item.options.includes(value));
  }).map((item) => item.name);
  if(product.variantsReady===false)codes.push("variants_missing");
  if (!product.image) codes.push("no_photo");
  if (!(product.description || "").trim()) codes.push("no_description");
  const root = rootCategory(categories, product.categoryId);
  if (!product.categoryId || !categories.some((item) => item.id === product.categoryId) || !root || !["plants","pots","soil","fertilizer","accessories"].includes(root.slug)) codes.push("invalid_category");
  if (missingStage1.length || product.variantStage1Missing || product.variantTasks?.length) codes.push("stage1_missing");
  if (isPlantProduct(product, categories) && !product.latinName.trim()) codes.push("latin_name_missing");
  if (isPlantProduct(product, categories) && (!product.careInstructions.trim() || !product.passport?.lighting || !product.passport?.watering)) codes.push("care_missing");
  if (unknownEnums.length || product.unknownEnumValues) codes.push("unknown_enum");
  const blockers = [...codes];
  if (product.status === "published") codes.push("published");
  else if (product.status === "draft") codes.push("draft");
  if (product.status === "draft" && blockers.length === 0) codes.push("ready");
  const tasks: ProductReadiness["tasks"] = [];
  if(codes.includes("variants_missing"))tasks.push({label:"Добавьте активный размер и проверьте цены",section:"variants"});
  if (codes.includes("no_photo")) tasks.push({label:"Добавьте основное фото",section:"photos"});
  if (codes.includes("no_description")) tasks.push({label:"Добавьте описание",section:"main"});
  if (codes.includes("invalid_category")) tasks.push({label:"Выберите категорию",section:"attributes"});
  missingStage1.forEach(name=>tasks.push({label:`Заполните: ${name}`,section:"attributes"}));
  (product.variantTasks || []).forEach(label=>tasks.push({label,section:"variants"}));
  if (product.variantStage1Missing && !product.variantTasks?.length) tasks.push({label:"Проверьте характеристики размеров",section:"variants"});
  if (codes.includes("latin_name_missing")) tasks.push({label:"Добавьте латинское название",section:"main"});
  if (codes.includes("care_missing")) tasks.push({label:"Заполните рекомендации по уходу",section:"care"});
  unknownEnums.forEach(name=>tasks.push({label:`Выберите актуальное значение: ${name}`,section:"attributes"}));
  if (product.unknownEnumValues && !unknownEnums.length) tasks.push({label:"Проверьте значения размеров",section:"variants"});
  return { tasks, codes, blockers, missingStage1, unknownEnums };
}

export function displayValue(value: unknown, attribute?: CategoryAttribute) {
  if (valueMissing(value)) return "Не заполнено";
  const values = Array.isArray(value) ? value.map(String) : [String(value)];
  return values.map((item) => attribute?.optionLabels?.[item] || item).join(", ");
}

export function normalizeCatalogQuery(value:string) {return value.trim().replace(/\s+/g," ").toLocaleLowerCase("ru");}
export function categoryPath(categories:Category[],id?:number) {
 const names:string[]=[];const seen=new Set<number>();let current=categories.find(x=>x.id===id);
 while(current&&!seen.has(current.id)){seen.add(current.id);names.unshift(current.name);current=categories.find(x=>x.id===current?.parentId)}
 return names.join(" / ");
}
export const channelLabels:Record<string,string>={saby:"СБИС",wildberries:"Wildberries",wb:"Wildberries",ozon:"Ozon",avito:"Авито",site:"Сайт",ficusin:"Фикусин"};
export const mappingLabels:Record<string,string>={code:"Код СБИС",id:"Идентификатор",uuid:"Идентификатор СБИС",vendor_code:"Артикул продавца",offer_id:"Артикул продавца",nm_id:"Код карточки",legacy_sku:"Прежний артикул"};
