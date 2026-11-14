import {expect,test,type Page} from '@playwright/test';
import {owner} from './helpers';
const categories=[{id:1,parentId:null,name:'Растения',slug:'plants',sortOrder:1,productsCount:0},{id:2,parentId:1,name:'Аглаонемы',slug:'aglaonema',sortOrder:2,productsCount:2}];
const passport={origin:'',lighting:'Рассеянный свет',watering:'Умеренный',humidity:'',temperature:'',soil:'',fertilizer:'',repotting:'',careDifficulty:'',growthRate:'',matureSize:'',toxicity:'',problems:'',pests:'',faq:[]};
const product=(id:number)=>({id,sabyCode:'',slug:`plant-${id}`,name:id===1?'Аглаонема Мария':id===2?'Хлорофитум':`Растение ${id}`,latinName:'Aglaonema',shortDescription:'Старое краткое описание',description:id===2?'':'Описание растения',careInstructions:'Уход',status:'draft',featured:false,image:'/assets/hero-monstera.webp',price:1490,stock:0,sku:String(id),variantLabel:'D11',wholesaleMinQty:1,passport,importantWarnings:[],externalIds:[],attributes:{light:'diffused'},categoryId:2,catalogSection:'plants',variantCount:2,updatedAt:'2026-10-15T12:00:00Z'});
const schema=[{id:1,code:'light',name:'Освещение',dataType:'enum',unit:'',options:['diffused'],optionLabels:{diffused:'Рассеянный свет'},audience:'customer',scope:'product',required:false,filterable:true,showOnPdp:true,keyCharacteristic:true,badge:false,sortOrder:1}];
const effective=['length_cm','width_cm','height_cm','weight_grams'].map((code,i)=>({id:i+2,code:`package_${code}`,name:['Длина упаковки','Ширина упаковки','Высота упаковки','Вес упаковки'][i],unit:i===3?'г':'см',dataType:'number',scope:'variant',audience:'technical',active:true,excluded:false,required:false,options:[],global:true}));
effective.push(...['length_cm','width_cm','height_cm'].map((code,i)=>({id:20+i,code:`package_winter_${code}`,name:['Длина зимней упаковки','Ширина зимней упаковки','Высота зимней упаковки'][i],unit:'см',dataType:'number',scope:'variant',audience:'technical',active:true,excluded:false,required:false,options:[],global:false})));
const size=(id:number,pid:number,label:string)=>({id,productId:pid,sku:String(id),label,price:1490,stock:0,wholesaleMinQty:1,active:false,archived:false,sabyId:`fixture-saby-${id}`,attributes:{pot_diameter_cm:Number(label.slice(1)),height_cm:40,package_length_cm:20,package_width_cm:20,package_height_cm:50,package_weight_grams:1000},externalIds:[{provider:'saby',type:'id',externalId:`fixture-saby-${id}`}],images:[],stockSource:'saby'});
async function fixture(page:Page,{total=2,failSave=false,loseCreateResponse=false,sharedGalleryPhoto=false,variantGalleryPhoto=false,variantTasks=[] as string[],role='owner' as 'owner'|'administrator'|'manager'}={}){
 const contentOnly=role==='administrator';
 let products=Array.from({length:total},(_,i)=>product(i+1));if(variantTasks.length)Object.assign(products[0],{variantTasks});const variants=new Map(products.map(p=>[p.id,[size(p.id*10,p.id,'D11'),size(p.id*10+1,p.id,'D17')]]));const updates:Array<Record<string,any>>=[],calls:string[]=[],plans=new Map<number,any>(),creations=new Map<string,any>(),mutations:Array<{path:string;method:string;body:any}>=[];let nextId=1000;
 let nextMediaId=100000;
 const media:Array<{id:number;productId:number;variantId?:number;url:string;primary:boolean;sortOrder:number}>=products.map(p=>({id:p.id*100,productId:p.id,url:p.image,primary:true,sortOrder:0}));
 if(sharedGalleryPhoto)media.push({id:++nextMediaId,productId:1,url:'/assets/hero-monstera.webp?photo=own',primary:false,sortOrder:1});
 if(variantGalleryPhoto)media.push({id:++nextMediaId,productId:1,variantId:10,url:'/assets/hero-monstera.webp?photo=variant',primary:true,sortOrder:0});
 await page.route('**/api/v1/**',async route=>{const req=route.request(),path=new URL(req.url()).pathname,method=req.method();calls.push(path);if(!['GET','HEAD'].includes(method)&&!path.endsWith('/pricing/preview'))mutations.push({path,method,body:req.postDataJSON()});const send=(body:unknown,status=200)=>route.fulfill({status,json:body});
 if(path==='/api/v1/auth/me')return send({user:{...owner.user,adminRole:role}});
 if(path==='/api/v1/admin/dashboard')return send({user:{fullName:'Владелец'},role,permissions:contentOnly?['dashboard.read','products.read','products.edit']:role==='manager'?['dashboard.read','products.read','products.edit','products.manage']:['dashboard.read','products.read','products.edit','products.manage','integrations.edit','catalog.delete'],dashboard:{products:products.length,variants:products.length*2,orders:0,customers:0,wholesalePending:0,lastSync:null,recentOrders:[]}});
 if(path==='/api/v1/admin/products'&&method==='GET')return send({products});
 if(path==='/api/v1/admin/products'&&method==='POST'){const body=req.postDataJSON();if(body.creationId&&creations.has(body.creationId))return send({product:creations.get(body.creationId)});const p={...product(++nextId),...body,image:'',price:body.priceMinor/100,status:'draft'};products.push(p);variants.set(p.id,[{...size(++nextId,p.id,'Основной'),externalIds:[],price:p.price,attributes:body.variantAttributes}]);if(body.creationId)creations.set(body.creationId,p);if(loseCreateResponse){loseCreateResponse=false;return send({error:'Ответ создания потерян'},503)}return send({product:p});}
 if(path==='/api/v1/admin/catalog-editor/references')return send({categories:[{id:'fixture-category',name:'Тестовая категория СБИС'}],folders:[{id:'fixture-folder',name:'Тестовый раздел СБИС'}]});
 if(path.endsWith('/editor-plan/sync-existing')&&method==='POST')return send({queued:0});
 const planMatch=path.match(/\/products\/(\d+)\/editor-plan$/);if(planMatch){const id=Number(planMatch[1]);if(method==='PUT'){if(contentOnly)return send({error:'Недостаточно прав'},403);plans.set(id,req.postDataJSON().config)};return send({config:plans.get(id)||{variants:[]},publications:[],links:{},barcodes:{},publicationMode:'disabled'});}
 if(path==='/api/v1/admin/catalog-editor/settings')return send({values:{'cdek.packaging_season':'summer'}});
 if(path==='/api/v1/admin/settings')return role==='owner'?send({values:{'cdek.packaging_season':'summer'}}):send({error:'Недостаточно прав'},403);
 if(path==='/api/v1/admin/procurement/pricing/preview')return send({quotes:{wb:{priceRub:2490,missing:[]},ozon:{priceRub:2990,missing:[]}}});
 if(path==='/api/v1/admin/categories')return send({categories});
 if(/\/categories\/\d+\/attributes$/.test(path))return send({attributes:[...schema,...effective,...[{code:'pot_diameter_cm',name:'Диаметр горшка'},{code:'height_cm',name:'Высота'}].map((f,i)=>({...schema[0],...f,id:40+i,dataType:'number',unit:'см',scope:'variant',options:[]}))]});
 if(/\/categories\/\d+\/effective-attributes$/.test(path))return send({attributes:effective});
 const rows=path.match(/\/products\/(\d+)\/variants$/);if(rows)return send({variants:variants.get(Number(rows[1]))||[]});
 const mediaRoute=path.match(/\/(products|variants)\/(\d+)\/media(?:\/(copy|\d+)(?:\/(primary))?)?$/);
 if(mediaRoute){const [,kind,rawId,action,primary]=mediaRoute;const id=Number(rawId);const belongs=(row:typeof media[number])=>kind==='products'?row.productId===id:row.variantId===id;
  if(method==='GET')return send({media:media.filter(belongs)});
  if(action==='copy'&&method==='POST'){const source=media.find(row=>row.id===req.postDataJSON().mediaId);const variant=[...variants.values()].flat().find(row=>row.id===id);if(!source||!variant||source.productId!==variant.productId)return send({error:'Фото не найдено'},404);const owned=media.filter(belongs);const copy={...source,id:++nextMediaId,variantId:id,primary:owned.length===0,sortOrder:owned.length};media.push(copy);return send({media:copy},201);}
  if(primary){for(const row of media.filter(belongs))row.primary=row.id===Number(action);return send({ok:true});}
  if(method==='DELETE'){if(contentOnly)return send({error:'Недостаточно прав'},403);const index=media.findIndex(row=>belongs(row)&&row.id===Number(action));if(index>=0)media.splice(index,1);return send({ok:true});}
  return send({error:'Unmocked media mutation'},400);
 }
 if(path==='/api/v1/admin/collection-definitions')return send({collections:[{id:1,title:'Для тёмной комнаты',slug:'shade',active:true,mode:'dynamic',rules:[{attribute:'light',operator:'eq',value:'diffused'}],products:[1],coverImage:'/assets/hero-monstera.webp',sortOrder:1}]});
 if(path.endsWith('/collection-definitions/preview'))return send({products:products.map(p=>p.id)});
 if(path==='/api/v1/admin/attributes')return send({attributes:schema.map(a=>({...a,active:true,options:[{code:'diffused',label:'Рассеянный свет',active:true}]}))});
 if(path==='/api/v1/admin/catalog-filters')return send({filters:[]});if(path.endsWith('/reviews'))return send({reviews:[]});
 if(path.endsWith('/products/publish'))return send({published:[1],blocked:[{productId:2,name:'Хлорофитум',reason:'Нет активного размера'}]});
 const update=path.match(/\/products\/(\d+)$/);if(update&&method==='PATCH'){const id=Number(update[1]),body=req.postDataJSON();updates.push({id,body});if(failSave)return send({error:'Ошибка сохранения'},500);for(const v of body.variants||[]){const current=variants.get(id)||[];const old=current.find(x=>x.id===v.id);if(v.remove){if(contentOnly)return send({error:'Недостаточно прав'},403);if(old?.sabyId||old?.externalIds?.length){Object.assign(old,{active:false,archived:true})}else if(old)current.splice(current.indexOf(old),1);continue;}if(old)Object.assign(old,v,{price:typeof v.priceMinor==='number'?v.priceMinor/100:old.price,attributes:{...old.attributes,...v.attributes}});else current.push({...size(++nextId,id,v.label),...v,id:nextId,price:v.priceMinor/100});variants.set(id,current)}const i=products.findIndex(p=>p.id===id);const {variants:_,...patch}=body;products[i]={...products[i],...patch};return send({product:products[i]})}
 return send({});});return{updates,calls,variants,plans,mutations,creations,media};
}
async function open(page:Page){await page.goto('/admin?section=products');await expect(page.locator('table.products tbody tr').first()).toBeVisible()}
async function edit(page:Page){await page.getByRole('link',{name:'Аглаонема Мария',exact:true}).click();const editor=page.locator('.editor-page');await expect(editor.getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();return editor}

async function sabyFields(editor:ReturnType<Page['locator']>){
 for(const [label,name] of [['Категория в СБИС','Тестовая категория СБИС'],['Раздел в СБИС','Тестовый раздел СБИС']]){
  await editor.getByRole('combobox',{name:label,exact:true}).click();await editor.locator('.editor-reference-options').getByRole('button',{name,exact:true}).click();
 }
}
async function chooseCategory(editor:ReturnType<Page['locator']>){await editor.getByRole('combobox',{name:'Найти категорию'}).fill('Аглаонемы');await editor.locator('.category-picker-list').getByRole('button',{name:'Растения / Аглаонемы',exact:true}).click();}
async function saveEditor(editor:ReturnType<Page['locator']>){await editor.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(editor.locator('.editor-notice')).toContainText('Изменения товара сохранены');}

async function manualCollection(page:Page,fail=false){
 await fixture(page);
 let collection={id:1,title:'Ручная подборка',slug:'manual',note:'',coverUrl:'/assets/hero-monstera.webp',active:false,mode:'manual',rules:[],products:[1],sortOrder:1};
 const writes:any[]=[];
 await page.route('**/api/v1/admin/collection-definitions',route=>route.fulfill({json:{collections:[collection]}}));
 await page.route('**/api/v1/admin/collection-definitions/1',route=>{const body=route.request().postDataJSON();writes.push(body);if(fail)return route.fulfill({status:500,json:{error:'Ошибка записи подборки'}});collection={...collection,...body};return route.fulfill({json:{collection}})});
 return writes;
}

test('@desktop selected variant survives tabs and a new variant saves with category-based naming',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);
 await e.locator('.editor-variant-card').getByRole('button',{name:/^D17/}).click();
 await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();await e.getByRole('tab',{name:'Товар',exact:true}).click();
 await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('17');
 await e.getByRole('button',{name:'+ Добавить вариант',exact:true}).click();await e.getByLabel('Диаметр горшка',{exact:true}).fill('24');await sabyFields(e);await saveEditor(e);
 expect(f.variants.get(1)).toHaveLength(3);expect(f.variants.get(1)?.at(-1)?.label).toBe('D24');await expect(e.locator('.editor-variant-card').getByRole('button',{name:/^D24/})).toHaveAttribute('aria-pressed','true');
});

test('@desktop follow-up collection settings and members save together and survive reload',async({page})=>{
 const writes=await manualCollection(page);await page.goto('/admin?section=collections&collection=1');const e=page.locator('.collection-editor-v2');
 await e.getByLabel('Название',{exact:true}).fill('Обновлённая подборка');await e.locator('.admin-collection-list input').last().check();
 expect(writes).toHaveLength(0);await expect(e.getByRole('status')).toContainText('несохранённые');
 await e.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(e.getByRole('status')).toContainText('сохранены');
 expect(writes).toHaveLength(1);expect(writes[0].products).toEqual([1,2]);expect(writes[0].title).toBe('Обновлённая подборка');
 await page.reload();await expect(e.getByLabel('Название',{exact:true})).toHaveValue('Обновлённая подборка');await expect(e.locator('.admin-collection-list input').last()).toBeChecked();
});

test('@desktop follow-up failed collection save and cancelled close/navigation preserve the draft',async({page})=>{
 await manualCollection(page,true);await page.goto('/admin?section=collections&collection=1');const e=page.locator('.collection-editor-v2');
 await e.getByLabel('Название',{exact:true}).fill('Не терять');await e.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(e.getByRole('alert')).toContainText('Ошибка записи');
 page.on('dialog',dialog=>void dialog.dismiss());await page.getByRole('button',{name:/Ручная подборка Скрыта/}).click();await expect(e.getByLabel('Название',{exact:true})).toHaveValue('Не терять');
 await page.getByRole('navigation',{name:'Каталог',exact:true}).getByRole('link',{name:'Категории'}).click();await expect(e).toBeVisible();await expect(page).toHaveURL(/section=collections/);
});

test('@desktop follow-up Cyrillic collection slug and cancel guard',async({page})=>{
 await fixture(page);await page.goto('/admin?section=collections');await page.getByRole('button',{name:'Новая подборка',exact:true}).click();const e=page.getByRole('region',{name:'Новая подборка',exact:true});
 await e.getByLabel('Название',{exact:true}).fill('Для офиса');await expect(e.getByLabel('Адрес подборки',{exact:true})).toHaveValue('dlya-ofisa');page.once('dialog',dialog=>void dialog.dismiss());await e.getByRole('button',{name:'Отмена',exact:true}).click();await expect(e).toBeVisible();
});

test('@phone follow-up collection controls stay visible and selection stays local until save',async({page})=>{
 const writes=await manualCollection(page);await page.goto('/admin?section=collections&collection=1');const e=page.locator('.collection-editor-v2');
 await e.getByLabel('Найти товар в подборку').fill('  Хлорофитум  ');await expect(e.locator('.admin-collection-list label')).toHaveCount(1);
 const checkbox=e.locator('.admin-collection-list input');expect(await checkbox.evaluate(el=>({width:el.getBoundingClientRect().width,opacity:getComputedStyle(el).opacity}))).toEqual({width:18,opacity:'1'});
 await checkbox.check();expect(writes).toHaveLength(0);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
});

test('@desktop follow-up category search and Cyrillic address',async({page})=>{
 await fixture(page);await page.goto('/admin?section=categories');await page.getByLabel('Название категории').fill('Редкие растения');await expect(page.getByLabel('Адрес категории')).toHaveValue('redkie-rasteniya');
 await page.getByLabel('Поиск категорий').fill('Аглаонемы');await expect(page.locator('tbody tr')).toHaveCount(1);await expect(page.locator('tbody tr')).toContainText('Аглаонемы');
});

test('@desktop explicit false plant attribute is saved without losing the selected tab draft',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);
 await expect(e.getByLabel('Цветущее',{exact:true})).toHaveValue('');await e.getByLabel('Цветущее',{exact:true}).selectOption('false');
 await e.getByRole('tab',{name:'Вопросы и ответы',exact:true}).click();await e.getByRole('tab',{name:'Товар',exact:true}).click();await expect(e.getByLabel('Цветущее',{exact:true})).toHaveValue('false');await saveEditor(e);expect(f.updates[0].body.attributes.flowering).toBe(false);
});

test('@desktop follow-up collection rules use readable boolean and multi-select values',async({page})=>{
 await fixture(page);
 const attributes=[{id:1,code:'flowering',name:'Цветение',dataType:'boolean',active:true,unit:'',options:[]},{id:2,code:'rooms',name:'Помещения',dataType:'multi_enum',active:true,unit:'',options:[{code:'office',label:'Офис',active:true},{code:'bedroom',label:'Спальня',active:true}]}];
 await page.route('**/api/v1/admin/attributes',route=>route.fulfill({json:{attributes}}));
 await page.route('**/api/v1/admin/collection-definitions',route=>route.fulfill({json:{collections:[{id:1,title:'По правилам',slug:'rules',note:'',coverUrl:'',sortOrder:1,active:false,mode:'dynamic',rules:[{attribute:'flowering',operator:'eq',value:false}],products:[]}]}}));
 await page.goto('/admin?section=collections&collection=1');await expect(page.getByLabel('Значение условия 1')).toHaveValue('false');
 await page.getByLabel('Характеристика условия 1').selectOption('rooms');await page.getByLabel('Сравнение условия 1').selectOption('contains');
 await page.getByLabel('Значение условия 1').selectOption('bedroom');await expect(page.getByLabel('Значение условия 1')).toHaveValue('bedroom');
 await page.getByLabel('Сравнение условия 1').selectOption('in');const choices=page.getByRole('group',{name:'Значение условия 1'});await choices.getByLabel('Офис').check();await choices.getByLabel('Спальня').check();
 await expect(choices.getByRole('checkbox')).toHaveCount(2);await expect(page.locator('.collection-editor-v2').getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();
});

test('@desktop @phone a restored draft waits for category and schema metadata before creating D variants',async({page})=>{
 const f=await fixture(page);let hold=false,categoryRequests=0,schemaRequests=0;let releaseCategories!:()=>void,releaseSchema!:()=>void;const categoriesGate=new Promise<void>(resolve=>{releaseCategories=resolve});const schemaGate=new Promise<void>(resolve=>{releaseSchema=resolve});
 await page.route('**/api/v1/admin/categories',async route=>{if(hold){categoryRequests++;await categoriesGate;}await route.fallback();});await page.route('**/api/v1/admin/categories/2/attributes',async route=>{if(hold){schemaRequests++;await schemaGate;}await route.fallback();});
 await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');await e.getByLabel('Название товара *',{exact:true}).fill('Черновик с поздними категориями');await chooseCategory(e);await sabyFields(e);await e.getByLabel('Диаметр горшка',{exact:true}).fill('11.3');await e.getByLabel('Высота',{exact:true}).fill('40');await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1490.99');
 hold=true;await page.reload();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Черновик с поздними категориями');await expect.poll(()=>categoryRequests).toBeGreaterThan(0);await expect.poll(()=>schemaRequests).toBeGreaterThan(0);const create=e.getByRole('button',{name:'Создать товар',exact:true});await expect(create).toBeDisabled();expect(f.creations.size).toBe(0);
 releaseCategories();await expect(e.getByRole('combobox',{name:'Найти категорию'})).toHaveValue('Растения / Аглаонемы');await expect(create).toBeDisabled();expect(f.creations.size).toBe(0);releaseSchema();hold=false;await expect(create).toBeEnabled();await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();await page.getByRole('button',{name:'Добавить товар',exact:true}).click();await expect(create).toBeEnabled();await create.click();await expect(page).toHaveURL(/product=1001/);
 expect(f.mutations.find(r=>r.path==='/api/v1/admin/products')!.body.catalogSection).toBe('plants');expect(f.variants.get(1001)?.[0].label).toBe('D11.3');expect(f.variants.get(1001)?.[0].attributes.pot_diameter_cm).toBe(11.3);expect(f.variants.get(1001)?.[0].price).toBe(1490.99);
});

test('@desktop category load errors preserve the existing draft and can be retried before Save',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Не потерять правку при ошибке категорий');await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();let fail=true;await page.route('**/api/v1/admin/categories',route=>fail?route.fulfill({status:503,json:{error:'Категории временно недоступны'}}):route.fallback());await page.getByRole('link',{name:'Аглаонема Мария',exact:true}).click();await expect(e.getByRole('button',{name:'Повторить загрузку категорий',exact:true})).toBeVisible();await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeDisabled();await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Не потерять правку при ошибке категорий');expect(f.updates).toHaveLength(0);fail=false;await e.getByRole('button',{name:'Повторить загрузку категорий',exact:true}).click();await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();await saveEditor(e);expect(f.updates[0].body.description).toBe('Не потерять правку при ошибке категорий');
});

test('@desktop an unavailable saved category blocks Save with a retry and preserves the draft',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Черновик с недоступной категорией');await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();let missing=true;await page.route('**/api/v1/admin/categories',route=>route.fulfill({json:{categories:missing?[categories[0]]:categories}}));await page.getByRole('link',{name:'Аглаонема Мария',exact:true}).click();await expect(e.getByRole('alert').filter({hasText:'Выбранная категория недоступна'})).toBeVisible();await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeDisabled();await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Черновик с недоступной категорией');expect(f.updates).toHaveLength(0);missing=false;await e.getByRole('button',{name:'Повторить загрузку категорий',exact:true}).click();await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();await saveEditor(e);expect(f.updates[0].body.description).toBe('Черновик с недоступной категорией');
});

test('@desktop @phone category picker supports keyboard selection after retaining pointer focus',async({page})=>{
 await fixture(page);await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');const search=e.getByRole('combobox',{name:'Найти категорию'});await search.fill('Аглаонемы');const option=e.locator('.category-picker-list').getByRole('button',{name:'Растения / Аглаонемы',exact:true});await expect(option).toBeVisible();await search.press('ArrowDown');await expect(e.locator('.category-picker-list').getByRole('button',{name:'Не указано',exact:true})).toBeFocused();await page.keyboard.press('ArrowDown');await expect(option).toBeFocused();await page.keyboard.press('Escape');await expect(e.locator('.category-picker-list')).toHaveCount(0);await expect(search).toBeFocused();await search.fill('Аглаонемы');await expect(option).toBeVisible();await search.press('ArrowDown');await page.keyboard.press('End');await expect(option).toBeFocused();await page.keyboard.press('Home');await expect(e.locator('.category-picker-list').getByRole('button',{name:'Не указано',exact:true})).toBeFocused();await page.keyboard.press('End');await page.keyboard.press('Enter');await expect(search).toHaveValue('Растения / Аглаонемы');await expect(e.getByLabel('Диаметр горшка',{exact:true})).toBeVisible();
});

test('@desktop @phone creation destinations have native accessible checkboxes and stay near Save',async({page})=>{
 await fixture(page);await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page'),dest=e.getByRole('group',{name:'Где создать товар'});
 await expect(dest.getByLabel('Сайт',{exact:true})).toBeChecked();await expect(dest.getByLabel('СБИС',{exact:true})).toBeDisabled();
 const wb=dest.getByLabel('Wildberries',{exact:true});await expect(wb).not.toBeChecked();await wb.check();await expect(wb).toBeChecked();await expect(dest.getByLabel('Ozon',{exact:true})).not.toBeChecked();
 const box=await wb.boundingBox();expect(box!.width).toBeGreaterThanOrEqual(16);expect(box!.height).toBeGreaterThanOrEqual(16);await expect(e.locator('.editor-save-actions').getByRole('button',{name:'Создать товар',exact:true})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
});

test('@desktop route editor has product, marketplace and FAQ tabs with editable care notes',async({page})=>{
 await fixture(page);await open(page);const e=await edit(page);await expect(page).toHaveURL(/product=1/);await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(e.getByRole('tab')).toHaveCount(3);
 await e.locator('.editor-care-topic > summary').filter({hasText:'На заметку'}).click();await expect(e.getByLabel('На заметку',{exact:true})).toBeVisible();
});

test('@desktop unified product and two variant edits survive reload',async({page})=>{
 const f=await fixture(page);await open(page);let e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Новое описание');await e.getByLabel('Диаметр горшка',{exact:true}).fill('13');await e.getByLabel('Вес, кг',{exact:true}).fill('1.25');
 await e.locator('.editor-variant-card').getByRole('button',{name:/^D17/}).click();await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1990');await e.locator('.editor-variant-card').getByRole('button',{name:/^D13/}).click();await expect(e.getByLabel('Вес, кг',{exact:true})).toHaveValue('1.25');await saveEditor(e);
 expect(f.updates).toHaveLength(1);expect(f.updates[0].body.description).toBe('Новое описание');expect(f.updates[0].body.variants).toHaveLength(2);expect(f.variants.get(1)?.[0].attributes.package_weight_grams).toBe(1250);expect(f.variants.get(1)?.[1].price).toBe(1990);
 await page.reload();e=page.locator('.editor-page');await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Новое описание');await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('13');await expect(e.getByLabel('Вес, кг',{exact:true})).toHaveValue('1.25');
});

test('@desktop failed save preserves the draft across reload',async({page})=>{
 const f=await fixture(page,{failSave:true});await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Ручной текст');await e.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(e.getByRole('alert')).toContainText('Ошибка сохранения');expect(f.plans.size).toBe(0);
 await page.reload();await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Ручной текст');await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Аглаонема Мария');
});

test('@desktop выделение сбрасывается при поиске и смене страницы',async({page})=>{await fixture(page,{total:65});await open(page);await page.getByLabel('Выбрать товары на странице').check();await expect(page.getByRole('region',{name:'Выбранные товары'})).toContainText('Выбрано: 50');await page.getByLabel('Поиск товаров').fill('Хлорофитум');await expect(page.getByRole('region',{name:'Выбранные товары'})).toHaveCount(0);await expect(page.locator('table.products tbody tr')).toHaveCount(1);await page.getByRole('button',{name:'Сбросить фильтры',exact:true}).click();await page.getByLabel('Выбрать Аглаонема Мария',{exact:true}).check();await page.getByRole('button',{name:'Далее →',exact:true}).click();await expect(page.locator('table.products tbody tr')).toHaveCount(15);await expect(page.getByRole('region',{name:'Выбранные товары'})).toHaveCount(0)});

test('@desktop пробелы не ломают поиск, URL восстанавливает контекст, пустой список имеет сброс',async({page})=>{await fixture(page);await open(page);await page.getByLabel('Поиск товаров').fill('  Аглаонема   Мария  ');await expect(page.locator('table.products tbody tr')).toHaveCount(1);await page.reload();await expect(page.getByLabel('Поиск товаров')).toHaveValue('  Аглаонема   Мария  ');await page.getByLabel('Поиск товаров').fill('Такого товара нет');await expect(page.getByRole('heading',{name:'Товары не найдены'})).toBeVisible();await page.locator('.catalog-empty').getByRole('button',{name:'Сбросить фильтры'}).click();await expect(page.locator('table.products tbody tr')).toHaveCount(2);await expect(page.getByRole('heading',{name:'Отзывы'})).toHaveCount(0)});

test('@desktop new variant has no inherited external IDs or stock',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('button',{name:'+ Добавить вариант',exact:true}).click();await e.getByLabel('Диаметр горшка',{exact:true}).fill('24');await sabyFields(e);expect(f.updates).toHaveLength(0);await saveEditor(e);
 const v=f.updates[0].body.variants.find((v:any)=>v.id<0);expect(v.active).toBe(false);expect(v.externalIds).toEqual([]);expect(v.stock).toBeUndefined();
});

test('@desktop @phone creation keeps a local draft until an explicit create and uses the same route editor',async({page})=>{
 const f=await fixture(page);await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');await expect(page).toHaveURL(/create=1/);
 await e.getByLabel('Название товара *',{exact:true}).fill('Новый фикус');await chooseCategory(e);await sabyFields(e);await e.getByLabel('Диаметр горшка',{exact:true}).fill('11.3');await e.getByLabel('Высота',{exact:true}).fill('40');await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1490.99');
 await page.reload();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Новый фикус');expect(f.mutations.filter(r=>r.path==='/api/v1/admin/products')).toHaveLength(0);
 await e.getByRole('button',{name:'Создать товар',exact:true}).click();await expect(page).toHaveURL(/product=1001/);await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('11.3');expect(f.plans.get(1001).variants[0].saby.category.id).toBe('fixture-category');expect(f.mutations.filter(r=>r.path==='/api/v1/admin/products')).toHaveLength(1);
});

test('@desktop @phone a completed creation starts a fresh draft and never overwrites the first product',async({page})=>{
 const f=await fixture(page);await open(page);const e=page.locator('.editor-page');
 for(const [index,name] of ['Первый новый товар','Второй новый товар'].entries()){
  await page.getByRole('button',{name:'Добавить товар',exact:true}).click();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('');
  await e.getByLabel('Название товара *',{exact:true}).fill(name);await chooseCategory(e);await sabyFields(e);await e.getByLabel('Диаметр горшка',{exact:true}).fill(String(12+index));await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill(String(1500+index));
  await e.getByRole('button',{name:'Создать товар',exact:true}).click();await expect(page).toHaveURL(new RegExp(`product=${1001+index*2}`));await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toBeEnabled();
  expect(await page.evaluate(()=>localStorage.getItem('ficusin:product-creation-draft:v1'))).toBeNull();
  expect(await page.evaluate(id=>JSON.parse(localStorage.getItem(`ficusin:product-edit-draft:v1:${id}`)!).name,1001+index*2)).toBe(name);
  await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();
 }
 const creates=f.mutations.filter(r=>r.path==='/api/v1/admin/products'&&r.method==='POST');expect(creates).toHaveLength(2);expect(creates[0].body.creationId).not.toBe(creates[1].body.creationId);expect(f.creations.size).toBe(2);
 expect(f.updates.filter(row=>row.id===1001).every(row=>row.body.name!=='Второй новый товар')).toBe(true);
 await page.getByRole('link',{name:'Первый новый товар',exact:true}).click();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Первый новый товар');await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('12');
});

test('@desktop @phone closing an unfinished creation keeps its draft for the next opening',async({page})=>{
 const f=await fixture(page);await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');await e.getByLabel('Название товара *',{exact:true}).fill('Незавершённый товар');await chooseCategory(e);await e.getByLabel('Диаметр горшка',{exact:true}).fill('14');
 await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();await page.getByRole('button',{name:'Добавить товар',exact:true}).click();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Незавершённый товар');await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('14');expect(f.creations.size).toBe(0);expect(f.updates).toHaveLength(0);
});

test('@desktop a lost creation response survives reload and retries the persisted creation identity',async({page})=>{
 const f=await fixture(page,{loseCreateResponse:true});await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');await e.getByLabel('Название товара *',{exact:true}).fill('Один товар после потери ответа');await chooseCategory(e);await sabyFields(e);await e.getByLabel('Диаметр горшка',{exact:true}).fill('12');await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1500');await e.getByRole('button',{name:'Создать товар',exact:true}).click();await expect(e.getByRole('alert')).toContainText('Ответ создания потерян');expect(f.creations.size).toBe(1);
 const first=f.mutations.find(r=>r.path==='/api/v1/admin/products')!.body.creationId;expect(first).toMatch(/^[0-9a-f-]{36}$/);await page.reload();await e.getByRole('button',{name:'Создать товар',exact:true}).click();await expect(page).toHaveURL(/product=1001/);expect(f.creations.size).toBe(1);expect(f.mutations.filter(r=>r.path==='/api/v1/admin/products').map(r=>r.body.creationId)).toEqual([first,first]);expect(f.variants.get(1001)).toHaveLength(1);
});

test('@desktop closing route editor keeps the autosaved draft without a confirmation',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Несохранённый текст');let dialogs=0;page.on('dialog',d=>{dialogs++;void d.dismiss()});await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();await expect(e).toHaveCount(0);await edit(page);await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Несохранённый текст');expect(dialogs).toBe(0);expect(f.updates).toHaveLength(0);
});

test('@desktop required site price prevents a server write',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('');await e.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(e.getByRole('alert')).toContainText('Укажите цену на сайте');expect(f.updates).toHaveLength(0);expect(f.plans.size).toBe(0);
});

test('@desktop массовая публикация показывает конкретный пропуск',async({page})=>{await fixture(page);await open(page);await page.getByLabel('Выбрать товары на странице').check();await page.getByRole('button',{name:'Опубликовать',exact:true}).click();await page.getByRole('button',{name:'Подтвердить публикацию (2)',exact:true}).click();await expect(page.locator('.admin-bulk-result')).toContainText('Успешно: 1 · Пропущено: 1 · Ошибка: 0');await expect(page.locator('.admin-bulk-result')).toContainText('Нет активного размера')});

test('@phone editor fields and selected variant survive tab navigation without horizontal overflow',async({page})=>{
 await fixture(page);await open(page);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);const e=await edit(page);await e.getByLabel('Диаметр горшка',{exact:true}).fill('14');await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();await e.getByRole('tab',{name:'Товар',exact:true}).click();await expect(e.getByLabel('Диаметр горшка',{exact:true})).toHaveValue('14');expect(await e.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true);expect((await e.locator('.editor-save-actions').boundingBox())!.height).toBeLessThan(155);
});

test('@desktop категории учитывают вложенные товары, код характеристики создаётся из названия',async({page})=>{await fixture(page);await page.goto('/admin?section=categories');const root=page.locator('tbody tr').filter({hasText:'Растения'});await expect(root).toContainText('2');await expect(root.getByRole('button',{name:'Удалить',exact:true})).toBeDisabled();await page.getByRole('button',{name:'Характеристики',exact:true}).click();await page.getByRole('button',{name:'Добавить характеристику'}).click();const d=page.locator('.admin-pim-editor');await d.getByLabel('Название',{exact:true}).fill('Вхождение в подборке');await d.getByText('Технический код',{exact:true}).click();await expect(d.getByLabel('Код характеристики')).toHaveValue('vhozhdenie_v_podborke')});

test('@desktop предпросмотр правил подборки показывает весь состав до сохранения',async({page})=>{const f=await fixture(page,{total:14});await page.goto('/admin?section=collections&collection=1');await expect(page.getByText('Подходит: 14 товаров',{exact:true})).toBeVisible();await expect(page.locator('.admin-collection-preview tbody tr')).toHaveCount(14);await expect(page.locator('.admin-collection-preview')).toContainText('1 · D11');await expect(page.locator('.admin-collection-preview')).toContainText('Освещение равно Рассеянный свет');expect(f.updates).toHaveLength(0)});

test('@desktop browser Back leaves the editor and reopening restores its local draft',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Сохранить черновик');await page.goBack();await expect(e).toHaveCount(0);await edit(page);await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Сохранить черновик');expect(f.updates).toHaveLength(0);
});

test('@desktop care warning bullets and FAQ rows save and survive reload',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.locator('.editor-care-topic > summary').filter({hasText:'Важно перед началом'}).click();await e.getByLabel('Важно перед началом',{exact:true}).fill('Не ставить рядом с батареей.\nНе переливать.');
 await e.getByRole('tab',{name:'Вопросы и ответы',exact:true}).click();await e.getByRole('button',{name:'Добавить вопрос',exact:true}).click();await e.getByLabel('Вопрос',{exact:true}).fill('Как поливать?');await e.getByLabel('Ответ',{exact:true}).fill('После просыхания.');await saveEditor(e);
 expect(f.updates[0].body.importantWarnings).toEqual(['Не ставить рядом с батареей.','Не переливать.']);expect(f.updates[0].body.passport.faq).toEqual([{question:'Как поливать?',answer:'После просыхания.'}]);await page.reload();await expect(e.getByLabel('Вопрос',{exact:true})).toHaveValue('Как поливать?');await e.getByRole('tab',{name:'Товар',exact:true}).click();await e.locator('.editor-care-topic > summary').filter({hasText:'Важно перед началом'}).click();await expect(e.getByLabel('Важно перед началом',{exact:true})).toHaveValue('• Не ставить рядом с батареей.\n• Не переливать.');
});

for (const differentIdentity of [false, true]) {
 test(`@desktop согласование контента перед объединением ${differentIdentity ? 'блокирует разные виды' : 'сохраняет только различающиеся тексты'}`,async({page})=>{
  await fixture(page);
  const products=[1,2].map(id=>({...product(id),name:'Арека',latinName:differentIdentity&&id===2?'Chamaedorea elegans':'Chrysalidocarpus lutescens',description:'Одинаковое описание',status:'published',shortDescription:id===1?'Основной текст':'Старый текст',heightClass:id===1?'high':'low'}));
  const writes:any[]=[];let merges=0;
  await page.route('**/api/v1/admin/products',route=>route.fulfill({json:{products}}));
  await page.route('**/api/v1/admin/products/*',route=>{
   if(route.request().method()!=='PATCH')return route.fallback();
   const id=Number(new URL(route.request().url()).pathname.split('/').pop()),body=route.request().postDataJSON();writes.push({id,body});
   Object.assign(products.find(p=>p.id===id)!,body);return route.fulfill({json:{product:products.find(p=>p.id===id)}});
  });
  await page.route('**/api/v1/admin/products/merge-published',route=>{merges++;return route.fulfill({json:{}})});
  await open(page);await page.getByLabel('Выбрать товары на странице').check();await page.getByRole('button',{name:'Проверить объединение',exact:true}).click();
  const d=page.getByRole('dialog');await d.getByText('Прежнее краткое описание',{exact:true}).click();
  await expect(d.getByText('Основной текст',{exact:true})).toBeVisible();await expect(d.getByText('Старый текст',{exact:true})).toBeVisible();expect(writes).toHaveLength(0);
  const save=d.getByRole('button',{name:'Сохранить контент основной в исходных карточках'});
  if(differentIdentity){await expect(save).toBeDisabled();return;}
  await save.click();await expect(d.getByRole('status')).toContainText('Контент сохранён');
  expect(writes).toEqual([{id:2,body:{shortDescription:'Основной текст',heightClass:'high'}}]);
  expect(products[1].price).toBe(1490);expect(products[1].sku).toBe('2');expect(products[1].status).toBe('published');expect(merges).toBe(0);
 });
}

test('@desktop ссылки на витрину используют постоянный код, сохраняя прежний slug',async({page})=>{
 await fixture(page);
 const p={...product(1),status:'published',slug:'old-name-d12',productCode:'201'};
 await page.route('**/api/v1/admin/products',route=>route.fulfill({json:{products:[p]}}));
 await open(page);await expect(page.locator('table.products').getByRole('link',{name:'На сайте ↗'})).toHaveAttribute('href','/product/201');
 await edit(page);await expect(page).toHaveURL(/product=1/);
});

for (const published of [false, true]) {
 test(`@desktop @phone поиск основной карточки ${published ? 'опубликованных товаров' : 'черновиков'}`, async ({page}) => {
  const f = await fixture(page);
  if (published) await page.route('**/api/v1/admin/products', route => route.fulfill({json:{products:[1,2].map(id=>({...product(id),status:'published'}))}}));
  let merges = 0;
  await page.route('**/api/v1/admin/products/merge*', route => { merges++; return route.fulfill({json:{}}); });
  await open(page);
  await page.getByRole('checkbox',{name:'Выбрать Аглаонема Мария',exact:true}).check();
  await page.getByRole('checkbox',{name:'Выбрать Хлорофитум',exact:true}).check();
  await page.getByRole('button',{name:published ? 'Проверить объединение' : 'Объединить размеры',exact:true}).click();
  const d = page.getByRole('dialog');
  const picker = d.getByRole('combobox',{name:'Основная карточка',exact:true});
  await picker.fill('  хЛОРО  2 ');
  await expect(d.getByRole('option')).toHaveCount(1);
  await expect(d.getByRole('option')).toContainText('Хлорофитум');
  await picker.press('ArrowDown');
  await picker.press('Enter');
  await expect(picker).toHaveValue(/Хлорофитум/);
  if (published) await expect(d.getByLabel('Подтверждённый вид или сорт')).toHaveValue('Хлорофитум');
  else await expect(d.getByText('Будет одна карточка: Хлорофитум')).toBeVisible();
  await picker.fill('нет-такого-товара');
  await expect(d.getByRole('status')).toContainText('Товар не найден');
  await picker.press('Escape');
  await expect(d).toBeVisible();
  await expect(picker).toHaveValue(/Хлорофитум/);
  await picker.fill('D11');
  await expect(d.getByRole('option')).toHaveCount(2);
  await picker.fill('Aglaonema plant-1');
  await expect(d.getByRole('option')).toHaveCount(1);
  await d.getByRole('option').click();
  await expect(picker).toHaveValue(/Аглаонема Мария/);
  expect(f.updates).toHaveLength(0);
  expect(merges).toBe(0);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
 });
}

test('@desktop @phone поиск товара в подборку по размеру и артикулу', async ({page})=>{
 const writes=await manualCollection(page);
 await page.route('**/api/v1/admin/products', route=>route.fulfill({json:{products:[{...product(1),sku:'FIC-D12',variantLabel:'D12'},{...product(2),sku:'MON-D17',variantLabel:'D17'}]}}));
 await page.goto('/admin?section=collections&collection=1');
 const e=page.locator('.collection-editor-v2');
 await e.getByLabel('Найти товар в подборку').fill('MON-D17');
 await expect(e.locator('.admin-collection-list label')).toHaveCount(1);
 await expect(e.locator('.admin-collection-list label')).toContainText('Хлорофитум');
 await e.getByLabel('Найти товар в подборку').fill('D12');
 await expect(e.locator('.admin-collection-list label')).toHaveCount(1);
 expect(writes).toHaveLength(0);
});


test('@desktop @phone Save persists the common cover once and copies only the inherited variant photograph',async({page})=>{
 const f=await fixture(page,{sharedGalleryPhoto:true});await open(page);const e=await edit(page);await expect(e.locator('.editor-gallery img')).toHaveCount(2);await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1600');await saveEditor(e);
 const copies=f.mutations.filter(r=>r.path.endsWith('/media/copy'));expect(copies).toHaveLength(2);expect(copies.map(r=>r.body.mediaId)).toEqual([100001,100001]);expect(f.media.filter(row=>row.productId===1&&row.url==='/assets/hero-monstera.webp')).toHaveLength(1);
 for(const variantId of [10,11]){const own=f.media.filter(row=>row.variantId===variantId);expect(own).toHaveLength(1);expect(own[0].url).toBe('/assets/hero-monstera.webp?photo=own');}
 const ids=f.media.map(row=>row.id);await saveEditor(e);expect(f.media.map(row=>row.id)).toEqual(ids);expect(f.mutations.filter(r=>r.path.endsWith('/media/copy'))).toHaveLength(2);
 await page.reload();await expect(e.locator('.editor-gallery img')).toHaveCount(2);await e.locator('.editor-variant-card').getByRole('button',{name:/^D17/}).click();await expect(e.locator('.editor-gallery img')).toHaveCount(2);expect(f.media.filter(row=>row.variantId===10)).toHaveLength(1);
});

test('@desktop @phone seasonal packaging stays separate and survives reload',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);
 for(const [season,width] of [['летней упаковки','15'],['зимней коробки','20']]){await e.getByLabel(`Длина ${season}, см`,{exact:true}).fill('40');await e.getByLabel(`Ширина ${season}, см`,{exact:true}).fill(width);await e.getByLabel(`Высота ${season}, см`,{exact:true}).fill(width);}
 await saveEditor(e);expect(f.variants.get(1)?.[0].attributes).toMatchObject({package_length_cm:40,package_width_cm:15,package_height_cm:15,package_winter_length_cm:40,package_winter_width_cm:20,package_winter_height_cm:20});await page.reload();await expect(e.getByLabel('Ширина летней упаковки, см',{exact:true})).toHaveValue('15');await expect(e.getByLabel('Ширина зимней коробки, см',{exact:true})).toHaveValue('20');expect(await e.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true);
});

test('@desktop @phone Saby create/link controls preserve an existing code without sending it',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await e.getByRole('button',{name:'Уже создан в СБИС',exact:true}).click();await e.getByLabel('Код товара в СБИС',{exact:true}).fill('X-TEST-42');await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();await e.getByRole('tab',{name:'Товар',exact:true}).click();await expect(e.getByLabel('Код товара в СБИС',{exact:true})).toHaveValue('X-TEST-42');await e.getByRole('button',{name:'Создать в СБИС',exact:true}).click();await expect(e.getByLabel('Код товара в СБИС',{exact:true})).toHaveCount(0);expect(f.mutations).toHaveLength(0);
});

test('@desktop Saby edits stay local until unified Save',async({page})=>{
 const f=await fixture(page);await open(page);const e=await edit(page);await sabyFields(e);expect(f.plans.size).toBe(0);expect(f.mutations).toHaveLength(0);await page.reload();await expect(e.getByRole('combobox',{name:'Категория в СБИС',exact:true})).toHaveValue('Тестовая категория СБИС');await saveEditor(e);expect(f.plans.get(1).variants[0].saby.category.id).toBe('fixture-category');expect(f.mutations.map(({path,method})=>({path,method}))).toEqual([{path:'/api/v1/admin/products/1/editor-plan',method:'PUT'}]);
});

test('@desktop @phone late catalog snapshot cannot discard a route editor draft',async({page})=>{
 const f=await fixture(page);let release!:()=>void;const held=new Promise<void>(resolve=>{release=resolve});await page.route('**/api/v1/admin/products',async route=>{if(route.request().method()!=='GET')return route.fallback();await held;await route.fulfill({json:{products:[product(1),product(2)]}})});
 await page.goto('/admin?section=products');await page.getByRole('button',{name:'Добавить товар',exact:true}).click();const e=page.locator('.editor-page');await e.getByLabel('Название товара *',{exact:true}).fill('Новый фикус после задержки');release();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Новый фикус после задержки');await expect(page).toHaveURL(/create=1/);expect(f.mutations.filter(r=>r.path==='/api/v1/admin/products')).toHaveLength(0);
});

test('@desktop tablet editor fields fit inside the information pane instead of being clipped',async({page})=>{
 await page.setViewportSize({width:768,height:1000});await fixture(page);
 await page.route('**/api/v1/admin/categories/2/attributes',route=>route.fulfill({json:{attributes:[...schema,...effective,
  {...schema[0],id:40,code:'watering',name:'Полив',options:['moderate'],optionLabels:{moderate:'Умеренный'}},
  ...[{code:'height_cm',name:'Высота'},{code:'pot_diameter_cm',name:'Диаметр горшка'}].map((f,i)=>({...schema[0],...f,id:41+i,dataType:'number',unit:'см',scope:'variant',options:[]})),
 ]}}));
 await open(page);const editor=await edit(page);await editor.getByRole('heading',{name:'Характеристики растения',exact:true}).click();
 const pane=editor.locator('.editor-information');expect(await pane.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1);
 const bounds=await pane.boundingBox();
 for(const label of ['Полив','Высота','Диаметр горшка']){
  const field=editor.getByLabel(label,{exact:true});const box=await field.boundingBox();expect(box!.x,label).toBeGreaterThanOrEqual(bounds!.x);expect(box!.x+box!.width,label).toBeLessThanOrEqual(bounds!.x+bounds!.width);
 }
});

for (const width of [768, 1024, 1280]) {
  test(`@desktop колонка товара читаема на ${width} px`, async ({page}) => {
    await page.setViewportSize({width,height:1024});
    await fixture(page); await open(page);
    const column=page.locator("table.products th").nth(2);
    expect((await column.boundingBox())!.width).toBeGreaterThanOrEqual(260);
    const row=page.locator("table.products tbody tr").first();
    expect((await row.boundingBox())!.height).toBeLessThan(250);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
  });
}

test('@desktop @phone Flowwow channel saves site-authoritative publication settings',async({page})=>{
 const state=await fixture(page);
 await page.route('**/api/v1/admin/flowwow/references?*',route=>{const q=new URL(route.request().url()).searchParams;return route.fulfill({json:q.get('kind')==='categories'?{data:[{name:'Растения',subCategories:[{subCategoryId:101,name:'Горшечные'}]}]}:{data:[{field:'height',name:'Высота Flowwow',type:'measure',required:true,measure:[{id:3}]}]}})});
 await open(page);const editor=await edit(page);await editor.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();
 const channel=editor.getByRole('region',{name:'Flowwow',exact:true});
 await channel.getByLabel('Отправлять в Flowwow').check();
 await channel.getByLabel('Подкатегория Flowwow').selectOption('101');
 await channel.getByRole('spinbutton',{name:'Высота Flowwow: значение',exact:true}).fill('35');
 await saveEditor(editor);
 const saved=state.mutations.filter(x=>x.path.endsWith('/editor-plan')&&x.method==='PUT').at(-1)?.body;
 expect(saved.submit).toBe(true);expect(saved.config.variants[0].flowwow.enabled).toBe(true);expect(saved.config.variants[0].flowwow.category.id).toBe('101');expect(saved.config.variants[0].flowwow.attributes.height).toEqual({val:35,measure:3});
});

test('@desktop @phone administrator saves product content and queues only stored marketplace connections',async({page})=>{
 const f=await fixture(page,{role:'administrator'});await open(page);const e=await edit(page);
 await expect(e.getByLabel('Цена на сайте, ₽ *',{exact:true})).toBeDisabled();
 await expect(e.getByLabel('Оптовый минимум, шт.',{exact:true})).toBeDisabled();
 await expect(e.getByRole('button',{name:'+ Добавить вариант',exact:true})).toBeDisabled();
 await expect(e.getByRole('button',{name:'Удалить обложку',exact:true})).toBeDisabled();
 await expect(e.getByRole('button',{name:'Уже создан в СБИС',exact:true})).toBeDisabled();
 await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Описание, изменённое менеджером');
 await e.getByLabel('Высота',{exact:true}).fill('47');
 await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();
 const marketplace=e.getByRole('group',{name:'Настройки маркетплейсов',exact:true});
 await expect(marketplace.locator('button').first()).toBeDisabled();
 await expect(e.getByRole('group',{name:'Где создать товар'})).toHaveCount(0);
 await e.getByRole('tab',{name:'Вопросы и ответы',exact:true}).click();
 await e.getByRole('button',{name:'Добавить вопрос',exact:true}).click();
 await e.getByLabel('Вопрос',{exact:true}).fill('Можно ли держать в спальне?');
 await e.getByLabel('Ответ',{exact:true}).fill('Да, при подходящем освещении.');
 await saveEditor(e);
 expect(f.updates[0].body.description).toBe('Описание, изменённое менеджером');
 expect(f.updates[0].body.variants[0].attributes.height_cm).toBe(47);
 expect(f.updates[0].body.variants[0]).not.toHaveProperty('priceMinor');
 expect(f.updates[0].body.variants[0]).not.toHaveProperty('externalIds');
 expect(f.updates[0].body.passport.faq).toEqual([{question:'Можно ли держать в спальне?',answer:'Да, при подходящем освещении.'}]);
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan')&&x.method==='PUT')).toHaveLength(0);
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan/sync-existing'))).toEqual([{path:'/api/v1/admin/products/1/editor-plan/sync-existing',method:'POST',body:{}}]);
 await expect(e.locator('.editor-notice')).toHaveText('Изменения товара сохранены.');
 await e.getByRole('button',{name:'Закрыть редактор',exact:true}).click();
 await page.getByRole('link',{name:'Аглаонема Мария',exact:true}).click();
 await e.getByRole('tab',{name:'Товар',exact:true}).click();
 await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Описание, изменённое менеджером');
});

test('@desktop administrator can retry a failed stored-plan sync without changing prices or marketplace links',async({page})=>{
 const f=await fixture(page,{role:'administrator'});let calls=0;
 await page.route('**/api/v1/admin/products/1/editor-plan/sync-existing',route=>{calls++;return route.fulfill({status:calls===1?503:200,json:calls===1?{error:'Обновление площадок временно недоступно'}:{queued:1}})});
 await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Сохранённый контент при повторе обмена');
 await e.getByRole('button',{name:'Сохранить',exact:true}).click();
 await expect(e.getByRole('alert')).toContainText('Обновление площадок временно недоступно');
 await expect(e.getByRole('textbox',{name:'О растении',exact:true})).toHaveValue('Сохранённый контент при повторе обмена');
 expect(f.updates).toHaveLength(1);await saveEditor(e);expect(calls).toBe(2);
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan')&&x.method==='PUT')).toHaveLength(0);
 expect(f.updates).toHaveLength(1);
});

test('@desktop administrator sees when saved content needs owner setup before marketplace sync',async({page})=>{
 await fixture(page,{role:'administrator'});await page.route('**/api/v1/admin/products/1/editor-plan/sync-existing',route=>route.fulfill({json:{queued:0,skipped:1,warnings:['Для обновления Ozon владелец должен завершить настройку карточки.']}}));
 await open(page);const e=await edit(page);await e.getByRole('textbox',{name:'О растении',exact:true}).fill('Контент сохранён, настройка площадки не потеряна');await saveEditor(e);
 await expect(e.getByRole('alert')).toContainText('Для обновления Ozon владелец должен завершить настройку карточки.');
 await expect(e.locator('.editor-notice')).toHaveText('Изменения товара сохранены.');
});

test('@desktop @phone manager has the full product editor without global delete permission',async({page})=>{
 const f=await fixture(page,{role:'manager'});await open(page);const e=await edit(page);
 await expect(e.getByLabel('Цена на сайте, ₽ *',{exact:true})).toBeEnabled();
 await expect(e.getByLabel('Оптовый минимум, шт.',{exact:true})).toBeEnabled();
 await expect(e.getByRole('button',{name:'Уже создан в СБИС',exact:true})).toBeEnabled();
 await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1990.50');
 await e.getByLabel('Оптовый минимум, шт.',{exact:true}).fill('3');
 await e.getByLabel('Высота',{exact:true}).fill('46');
 await expect(e.getByLabel('Цена Wildberries, ₽',{exact:true})).toHaveValue('2490');
 await expect(e.getByLabel('Цена Ozon, ₽',{exact:true})).toHaveValue('2990');
 expect(f.calls).toContain('/api/v1/admin/catalog-editor/settings');expect(f.calls).toContain('/api/v1/admin/procurement/pricing/preview');expect(f.calls).not.toContain('/api/v1/admin/settings');
 await e.getByRole('button',{name:'Вручную',exact:true}).click();
 await e.getByLabel('Цена Wildberries, ₽',{exact:true}).fill('2700');await e.getByLabel('Цена Ozon, ₽',{exact:true}).fill('3150');
 await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();
 await expect(e.getByRole('group',{name:'Настройки маркетплейсов',exact:true}).locator('button').first()).toBeEnabled();
 await e.getByRole('tab',{name:'Товар',exact:true}).click();
 await e.getByRole('button',{name:'+ Добавить вариант',exact:true}).click();
 await e.getByLabel('Диаметр горшка',{exact:true}).fill('24');await sabyFields(e);
 await saveEditor(e);
 const changed=f.updates[0].body.variants.find((v:any)=>v.id===10);
 expect(changed.priceMinor).toBe(199050);expect(changed.wholesaleMinQty).toBe(3);expect(changed.attributes.height_cm).toBe(46);
 expect(f.updates[0].body.variants.find((v:any)=>v.id<0).label).toBe('D24');
 expect(f.variants.get(1)!.filter(v=>!v.archived)).toHaveLength(3);
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan')&&x.method==='PUT')).toHaveLength(1);
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan/sync-existing'))).toHaveLength(0);
 expect(f.plans.get(1).variants).toHaveLength(3);
 const plan=f.plans.get(1).variants.find((v:any)=>v.variantId===10);expect(plan.wbPrice).toBe('2700');expect(plan.ozonPrice).toBe('3150');
});

test('@desktop @phone manager removes saved photos and archives a linked variant inside the editor',async({page})=>{
 const f=await fixture(page,{role:'manager',variantGalleryPhoto:true});await open(page);const e=await edit(page);
 const removePhoto=e.getByRole('button',{name:/^Удалить фото /}).first();await expect(removePhoto).toBeEnabled();await removePhoto.click();
 await e.getByRole('button',{name:'Удалить вариант D17',exact:true}).click();
 await saveEditor(e);
 expect(f.mutations.filter(x=>x.method==='DELETE'&&x.path==='/api/v1/admin/variants/10/media/100001')).toHaveLength(1);
 expect(f.updates[0].body.variants).toContainEqual({id:11,remove:true});
 expect(f.variants.get(1)!.find(v=>v.id===11)!.archived).toBe(true);
 await expect(e.locator('.editor-notice')).toContainText('В архиве из-за связей: D17');
 expect(f.mutations.filter(x=>x.path.endsWith('/editor-plan')&&x.method==='PUT')).toHaveLength(1);
});

test('@desktop administrator cannot open product creation through a direct URL',async({page})=>{
 await fixture(page,{role:'administrator'});await page.goto('/admin?section=products&create=1');
 await expect(page.getByRole('alert')).toHaveText('Нет доступа к редактору товара.');
 await expect(page.locator('.editor-page')).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Добавить товар',exact:true})).toHaveCount(0);
});

test('@desktop manager editor capability does not grant catalogue import or bulk merge',async({page})=>{
 const f=await fixture(page,{role:'manager'});await open(page);
 await expect(page.getByRole('button',{name:'Добавить товар',exact:true})).toBeEnabled();
 await expect(page.getByRole('button',{name:'Импорт из СБИС',exact:true})).toHaveCount(0);
 await page.getByRole('checkbox',{name:'Выбрать Аглаонема Мария',exact:true}).check();
 await page.getByRole('checkbox',{name:'Выбрать Хлорофитум',exact:true}).check();
 const selection=page.getByRole('region',{name:'Выбранные товары',exact:true});
 await expect(selection.locator('summary')).toContainText('Выбрано: 2');
 await expect(selection.getByRole('button',{name:'Опубликовать',exact:true})).toBeEnabled();
 await expect(selection.getByRole('button',{name:'Объединить размеры',exact:true})).toHaveCount(0);
 await expect(selection.getByRole('button',{name:'Проверить объединение',exact:true})).toHaveCount(0);
 await expect(selection.getByRole('button',{name:'Удалить черновики',exact:true})).toHaveCount(0);
 expect(f.mutations).toHaveLength(0);

});

test('@desktop @phone saved price proposals require an explicit channel confirmation with the saved fingerprint',async({page})=>{
 await fixture(page);
 let approvals=0;
 let price={id:91,variantId:10,channel:'wb',targetId:'WB-price-fixture',priceMinor:250000,compareAtMinor:300000,expectedSitePriceMinor:149000,status:'draft',message:'Готово к проверке',fingerprint:'exact-price-snapshot',executionMode:'live',canApprove:true,canRetry:false,blockReason:''};
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{prices:[price],publicationMode:'live'}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/91/approve',route=>{approvals++;expect(route.request().postDataJSON()).toEqual({fingerprint:'exact-price-snapshot'});price={...price,status:'queued',canApprove:false};return route.fulfill({json:price})});
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});
 await expect(panel.getByRole('button',{name:'Подтвердить цену',exact:true})).toBeEnabled();await expect(panel).toContainText('Базовая цена WB');expect(approvals).toBe(0);
 await saveEditor(e);await expect(panel.getByRole('button',{name:'Подтвердить цену',exact:true})).toBeEnabled();expect(approvals).toBe(0);
 await panel.getByRole('button',{name:'Подтвердить цену',exact:true}).click();await expect(panel).toContainText('В очереди');expect(approvals).toBe(1);
 await e.getByRole('tab',{name:'Товар',exact:true}).click();await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();expect(approvals).toBe(1);
});

test('@desktop price confirmation conflict remains visible and never discards the unsaved editor draft',async({page})=>{
 await fixture(page);
 let approvals=0;
 const price={id:92,variantId:10,channel:'avito',targetId:'AVITO-price-fixture',priceMinor:250000,compareAtMinor:null,expectedSitePriceMinor:149000,status:'draft',message:'Готово',fingerprint:'stale-snapshot',executionMode:'live',canApprove:true,canRetry:false,blockReason:''};
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{prices:[price],publicationMode:'live'}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/92/approve',route=>{approvals++;return route.fulfill({status:409,json:{error:'Предложение цены изменилось'}})});
 await open(page);const e=await edit(page);await e.getByLabel('Название товара *',{exact:true}).fill('Несохранённое имя');await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});
 await panel.getByRole('button',{name:'Подтвердить цену',exact:true}).click();await expect(panel.getByRole('alert')).toContainText('Предложение цены изменилось');expect(approvals).toBe(1);
 await e.getByRole('tab',{name:'Товар',exact:true}).click();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Несохранённое имя');
 await e.getByLabel('Цена на сайте, ₽ *',{exact:true}).fill('1600');await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();await expect(panel.getByRole('button',{name:'Подтвердить цену',exact:true})).toBeDisabled();expect(approvals).toBe(1);
});

test('@desktop disabled publication keeps migrated price drafts unapproved',async({page})=>{
 await fixture(page);let approvals=0;
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{publicationMode:'disabled',prices:[{id:93,variantId:10,channel:'ozon',targetId:'OZON-migrated',priceMinor:250000,compareAtMinor:320000,expectedSitePriceMinor:149000,status:'draft',message:'Перенесённый черновик',fingerprint:'migrated',executionMode:'disabled',canApprove:false,canRetry:false,blockReason:'Отправка отключена'}]}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/*/approve',route=>{approvals++;return route.fulfill({json:{}})});
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});await expect(panel).toContainText('Перенесённый черновик');await expect(panel).toContainText('Отправка цен отключена');await expect(panel.getByRole('button',{name:'Подтвердить цену',exact:true})).toHaveCount(0);expect(approvals).toBe(0);
});

test('@desktop mock price retry stays explicit and uses the operation fingerprint',async({page})=>{
 await fixture(page);let retries=0;
 let price={id:94,variantId:10,channel:'ozon',targetId:'OZON-local',priceMinor:250000,compareAtMinor:320000,expectedSitePriceMinor:149000,status:'failed',message:'Локальная ошибка',fingerprint:'exact-retry',executionMode:'mock',canApprove:false,canRetry:true,blockReason:''};
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{publicationMode:'mock',prices:[price]}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/94/retry',route=>{retries++;expect(route.request().postDataJSON()).toEqual({fingerprint:'exact-retry'});price={...price,status:'queued',canRetry:false};return route.fulfill({json:price})});
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});await expect(panel).toContainText('Реальные цены площадок не меняются');expect(retries).toBe(0);await panel.getByRole('button',{name:'Повторить отправку',exact:true}).click();await expect(panel).toContainText('В очереди');expect(retries).toBe(1);
});


test('@desktop WB price confirmation shows the effective integer-discount price before approval',async({page})=>{
 await fixture(page);let approvals=0;
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{publicationMode:'live',prices:[{id:95,variantId:10,channel:'wb',targetId:'WB-rounded',priceMinor:150025,compareAtMinor:200000,expectedSitePriceMinor:149000,status:'draft',message:'Проверьте округление',fingerprint:'rounded-price',executionMode:'live',canApprove:true,canRetry:false,blockReason:''}]}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/95/approve',route=>{approvals++;return route.fulfill({json:{}})});
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});
 await expect(panel.locator('div').filter({has:page.locator('dt').filter({hasText:'Расчётная цена'})}).last().locator('dd')).toContainText(/1\s?500,25/);
 await expect(panel.locator('div').filter({has:page.locator('dt').filter({hasText:'Скидка WB'})}).last().locator('dd')).toHaveText('24%');
 await expect(panel.locator('div').filter({has:page.locator('dt').filter({hasText:'Цена WB после округления скидки'})}).last().locator('dd')).toContainText(/1\s?520/);
 expect(approvals).toBe(0);
});


test('@desktop read-only price reconciliation keeps the frozen fingerprint after the site price changes',async({page})=>{
 const f=await fixture(page);f.variants.get(1)![0].price=1600;let checks=0;
 let price={id:96,variantId:10,channel:'wb',targetId:'WB-inflight',priceMinor:250000,compareAtMinor:300000,expectedSitePriceMinor:149000,status:'needs_attention',message:'Ответ не получен',fingerprint:'frozen-old-request',executionMode:'live',canApprove:false,canRetry:true,blockReason:''};
 await page.route('**/api/v1/admin/products/1/editor-prices',route=>route.fulfill({json:{publicationMode:'live',prices:[price]}}));
 await page.route('**/api/v1/admin/products/1/editor-prices/96/retry',route=>{checks++;expect(route.request().postDataJSON()).toEqual({fingerprint:'frozen-old-request'});price={...price,status:'queued',canRetry:false};return route.fulfill({json:price})});
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();const panel=e.getByRole('region',{name:'Подтверждение цен площадок'});const check=panel.getByRole('button',{name:'Проверить результат',exact:true});await expect(check).toBeEnabled();await check.click();await expect(panel).toContainText('В очереди');expect(checks).toBe(1);
});

test('@desktop editor price mode is explicit in saved plans and restored without losing local intent',async({page})=>{
 const f=await fixture(page);f.plans.set(1,{variants:[{variantId:10,priceMode:'manual'}]});await open(page);const e=await edit(page);const modes=e.getByRole('group',{name:'Режим цен маркетплейсов'});
 await expect(modes.getByRole('button',{name:'Вручную',exact:true})).toHaveAttribute('aria-pressed','true');
 await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10).priceMode).toBe('manual');
 await modes.getByRole('button',{name:'Авто',exact:true}).click();await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10).priceMode).toBe('auto');
 await page.reload();await expect(page.locator('.editor-page').getByRole('group',{name:'Режим цен маркетплейсов'}).getByRole('button',{name:'Авто',exact:true})).toHaveAttribute('aria-pressed','true');
});

for(const savedMode of ['manual',undefined] as const)test(`@desktop late saved ${savedMode || 'legacy'} price mode cannot overwrite an explicit auto draft choice`,async({page})=>{
 const f=await fixture(page);let release!:()=>void;const held=new Promise<void>(resolve=>{release=resolve});
 await page.route('**/api/v1/admin/products/1/editor-plan',async route=>{if(route.request().method()!=='GET')return route.fallback();await held;return route.fulfill({json:{config:{variants:[{variantId:10,priceMode:savedMode,ozonPrice:'4321.45',wbPrice:'3210.25'}]},publications:[],links:{},barcodes:{},publicationMode:'disabled'}})});
 await open(page);const e=await edit(page);const auto=e.getByRole('group',{name:'Режим цен маркетплейсов'}).getByRole('button',{name:'Авто',exact:true});await auto.click();release();await expect(auto).toHaveAttribute('aria-pressed','true');await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10).priceMode).toBe('auto');
});


test('@desktop saved manual marketplace amounts survive a fresh browser draft and Save unchanged',async({page})=>{
 const f=await fixture(page);f.plans.set(1,{variants:[{variantId:10,priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'}]});
 await open(page);let e=await edit(page);
 for(const [label,value] of [['Цена Wildberries, ₽','3210.25'],['Цена Ozon, ₽','4321.45']])await expect(e.getByLabel(label,{exact:true})).toHaveValue(value);
 await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10)).toMatchObject({priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'});
 await page.evaluate(()=>localStorage.removeItem('ficusin:product-edit-draft:v1:1'));await page.reload();e=page.locator('.editor-page');
 await expect(e.getByRole('group',{name:'Режим цен маркетплейсов'}).getByRole('button',{name:'Вручную',exact:true})).toHaveAttribute('aria-pressed','true');
 for(const [label,value] of [['Цена Wildberries, ₽','3210.25'],['Цена Ozon, ₽','4321.45']])await expect(e.getByLabel(label,{exact:true})).toHaveValue(value);
 await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10)).toMatchObject({priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'});
});

test('@desktop a late saved plan preserves newer manual price edits including zero',async({page})=>{
 await fixture(page);let release!:()=>void;const held=new Promise<void>(resolve=>{release=resolve});
 await page.route('**/api/v1/admin/products/1/editor-plan',async route=>{if(route.request().method()!=='GET')return route.fallback();await held;return route.fulfill({json:{config:{variants:[{variantId:10,priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'}]},publications:[],links:{},barcodes:{},publicationMode:'disabled'}})});
 await open(page);const e=await edit(page);const manual=e.getByRole('group',{name:'Режим цен маркетплейсов'}).getByRole('button',{name:'Вручную',exact:true});await manual.click();await e.getByLabel('Цена Wildberries, ₽',{exact:true}).fill('0');await e.getByLabel('Цена Ozon, ₽',{exact:true}).fill('5220.21');
 const response=page.waitForResponse(r=>r.url().endsWith('/editor-plan')&&r.request().method()==='GET');release();await response;
 await expect(manual).toHaveAttribute('aria-pressed','true');await expect(e.getByLabel('Цена Wildberries, ₽',{exact:true})).toHaveValue('0');await expect(e.getByLabel('Цена Ozon, ₽',{exact:true})).toHaveValue('5220.21');
});


test('@desktop migrated saved prices retain manual intent until explicit Auto',async({page})=>{
 // The backend migration materializes missing modes before exposing saved plans.
 const f=await fixture(page);f.plans.set(1,{variants:[{variantId:10,priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'}]});await open(page);const e=await edit(page);const modes=e.getByRole('group',{name:'Режим цен маркетплейсов'});
 await expect(modes.getByRole('button',{name:'Вручную',exact:true})).toHaveAttribute('aria-pressed','true');await expect(e.getByLabel('Цена Wildberries, ₽',{exact:true})).toHaveValue('3210.25');await expect(e.getByLabel('Цена Ozon, ₽',{exact:true})).toHaveValue('4321.45');expect(f.mutations).toHaveLength(0);
 await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10)).toMatchObject({priceMode:'manual',ozonPrice:'4321.45',wbPrice:'3210.25'});
 await modes.getByRole('button',{name:'Авто',exact:true}).click();await expect(e.getByLabel('Цена Wildberries, ₽',{exact:true})).toHaveValue('2490');await expect(e.getByLabel('Цена Ozon, ₽',{exact:true})).toHaveValue('2990');await saveEditor(e);expect(f.plans.get(1).variants.find((v:any)=>v.variantId===10)).toMatchObject({priceMode:'auto',ozonPrice:'2990',wbPrice:'2490'});
});

test('@desktop @phone marketplace dictionary pagination retains canonical IDs in the local draft',async({page})=>{
 const f=await fixture(page);
 f.plans.set(1,{variants:[{variantId:10,saby:{mode:'link',existingCode:'fixture-code',category:{id:'fixture-category',name:'Категория'},folder:{id:'fixture-folder',name:'Раздел'}},marketplace:{name:'',article:'10',description:'',brand:'',model:'',tnved:'',units:1,ozon:{enabled:false,mode:'create',existingId:'',category:{id:'123',typeId:456,name:'Растения Ozon'}},wb:{enabled:false,mode:'create',existingId:'',category:{id:'',name:''}}}}]});
 const reads:URLSearchParams[]=[];
 await page.route('**/api/v1/admin/catalog-editor/references?*',route=>{
  const params=new URL(route.request().url()).searchParams;
  if(params.get('channel')!=='ozon')return route.fallback();
  if(params.get('kind')==='attributes')return route.fulfill({json:{data:{result:[{id:85,name:'Бренд',dictionary_id:2}]}}});
  reads.push(params);
  return route.fulfill({json:{data:params.get('cursor')?{result:[{id:901,value:'Первая марка'},{id:902,value:'Вторая марка'}]}:{result:[{id:901,value:'Первая марка'}],next:901}}});
 });
 await open(page);const e=await edit(page);await e.getByRole('tab',{name:'Маркетплейсы',exact:true}).click();
 const brand=e.getByRole('combobox',{name:'Бренд *',exact:true});await brand.click();
 const list=e.getByRole('listbox',{name:'Бренд *',exact:true});await expect(list.getByRole('option')).toHaveCount(1);
 await list.getByRole('button',{name:'Показать ещё',exact:true}).click();await expect(list.getByRole('option')).toHaveCount(2);
 await list.getByRole('option',{name:'Вторая марка',exact:true}).click();await expect(brand).toHaveValue('Вторая марка');
 expect(reads.map(p=>({channel:p.get('channel'),kind:p.get('kind'),category:p.get('category'),typeId:p.get('typeId'),attribute:p.get('attribute'),cursor:p.get('cursor')}))).toEqual([{channel:'ozon',kind:'values',category:'123',typeId:'456',attribute:'85',cursor:''},{channel:'ozon',kind:'values',category:'123',typeId:'456',attribute:'85',cursor:'901'}]);
 await expect.poll(()=>page.evaluate(()=>JSON.parse(localStorage.getItem('ficusin:product-edit-draft:v1:1')||'{}').variants?.find((v:{serverId:number})=>v.serverId===10)?.marketplace?.brandId)).toBe(902);
 await page.reload();await expect(page.locator('.editor-page').getByRole('combobox',{name:'Бренд *',exact:true})).toHaveValue('Вторая марка');
});

test('@desktop owner integration permission keeps catalogue import available',async({page})=>{
 await fixture(page,{role:'owner'});await open(page);
 await expect(page.getByRole('button',{name:'Импорт из СБИС',exact:true})).toBeEnabled();
 await expect(page.getByRole('button',{name:'Добавить товар',exact:true})).toBeEnabled();
});

function oldBrowserDraft() {
 return {version:1,name:'Сохранённая работа V1',categoryId:2,latinName:'Aglaonema',description:'Локальное описание',shortDescription:'',care:'',warnings:'',price:'1490',wholesaleMinQty:'1',active:true,attributes:{light:'diffused'},faq:[{id:'old-question',question:'Старый вопрос',answer:'Сохранённый ответ'}],media:[],remoteInitialized:true,creationId:'unchanged-creation-id',remoteCreatedProductId:1,selectedVariantId:'legacy-10',editorTab:'product',variants:[{id:'legacy-10',serverId:10,sku:'10',label:'D11',price:'1490',wholesaleMinQty:'1',active:false,archived:false,attributes:{pot_diameter_cm:11,height_cm:40},externalIds:[],media:[{id:'legacy-file',name:'Сохранённое фото.png',type:'image/png'}],mediaInitialized:true,mediaCustomized:true,ozon:{article:'',price:'0'},wb:{article:'WB-OLD',price:'3456.70'},sabyEditor:{mode:'create',existingCode:'',category:{id:'',name:''},folder:{id:'',name:''}},sabyEditorTouched:{mode:true,existingCode:true,category:true,folder:true}}]};
}

test('@desktop @phone V1 browser work and binary photographs migrate once and survive reload',async({page})=>{
 const f=await fixture(page);const raw=JSON.stringify(oldBrowserDraft());
 await page.addInitScript(({raw})=>{
  const key='ficusin:product-edit-draft:v1:1';if(localStorage.getItem(key)!==null)return;localStorage.setItem(key,raw);
  const req=indexedDB.open('ficusin-product-drafts',1);req.onupgradeneeded=()=>req.result.createObjectStore('media');req.onsuccess=()=>{const db=req.result,tx=db.transaction('media','readwrite');const bytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=='),c=>c.charCodeAt(0));tx.objectStore('media').put({version:1,type:'image/png',bytes:bytes.buffer},'legacy-file');tx.oncomplete=()=>db.close();};
 },{raw});
 await page.goto('/admin?section=products&product=1');const e=page.locator('.editor-page');
 await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Сохранённая работа V1');
 await expect(e.getByRole('img',{name:'Сохранённое фото.png',exact:true})).toBeVisible();
 const check=()=>page.evaluate(()=>({backup:localStorage.getItem('ficusin:product-edit-draft:v1:1:migration-backup:v1'),draft:JSON.parse(localStorage.getItem('ficusin:product-edit-draft:v1:1')!)}));
 let value=await check();expect(value.backup).toBe(raw);expect(value.draft.version).toBe(2);expect(value.draft.creationId).toBe('unchanged-creation-id');expect(value.draft.remoteCreatedProductId).toBe(1);expect(value.draft.selectedVariantId).toBe('legacy-10');
 expect(value.draft.variants[0]).toMatchObject({marketplacePriceMode:'manual',marketplace:{ozon:{article:''},wb:{article:'WB-OLD'}},ozon:{price:'0'},sabyEditor:{mode:'create',existingCode:''},sabyEditorTouched:{mode:true,existingCode:true}});
 await page.reload();await expect(e.getByLabel('Название товара *',{exact:true})).toHaveValue('Сохранённая работа V1');await expect(e.getByRole('img',{name:'Сохранённое фото.png',exact:true})).toBeVisible();value=await check();expect(value.backup).toBe(raw);expect(value.draft.variants[0].media[0].id).toBe('legacy-file');expect(f.mutations).toHaveLength(0);
});

for(const failure of ['malformed','quota'] as const)test(`@desktop @phone unsafe ${failure} V1 migration preserves its source and blocks editor writes`,async({page})=>{
 const f=await fixture(page),raw=failure==='malformed'?'{broken':JSON.stringify(oldBrowserDraft());
 await page.addInitScript(({raw,failure})=>{const key='ficusin:product-edit-draft:v1:1',set=Storage.prototype.setItem;if(localStorage.getItem(key)===null)set.call(localStorage,key,raw);if(failure==='quota')Storage.prototype.setItem=function(k,v){if(k.startsWith(key))throw new DOMException('Full','QuotaExceededError');return set.call(this,k,v);};},{raw,failure});
 await page.goto('/admin?section=products&product=1');const e=page.locator('.editor-page');await expect(e.getByRole('alert')).toContainText('Исходные данные не удалены');await expect(e.getByRole('button',{name:'Сохранить',exact:true})).toHaveCount(0);await expect(e.getByLabel('Название товара *',{exact:true})).toHaveCount(0);
 await page.reload();await expect(e.getByRole('alert')).toContainText('Исходные данные не удалены');expect(await page.evaluate(()=>localStorage.getItem('ficusin:product-edit-draft:v1:1'))).toBe(raw);expect(f.mutations).toHaveLength(0);
});


test('@desktop legacy IndexedDB Blob is atomically backed up as exact binary bytes before replacement',async({page})=>{
 await fixture(page);await open(page);const raw=JSON.stringify(oldBrowserDraft());
 await page.evaluate(async raw=>{localStorage.setItem('ficusin:product-edit-draft:v1:1',raw);await new Promise<void>((resolve,reject)=>{const request=indexedDB.open('ficusin-product-drafts',1);request.onupgradeneeded=()=>request.result.createObjectStore('media');request.onsuccess=()=>{const db=request.result,tx=db.transaction('media','readwrite');const bytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=='),c=>c.charCodeAt(0));tx.objectStore('media').put(new Blob([bytes],{type:'image/png'}),'legacy-file');tx.oncomplete=()=>{db.close();resolve();};tx.onerror=()=>reject(tx.error);};});},raw);
 await page.goto('/admin?section=products&product=1&editor=media');await expect(page.getByRole('img',{name:'Сохранённое фото.png',exact:true})).toBeVisible();
 const read=()=>page.evaluate(async()=>{const db=await new Promise<IDBDatabase>(resolve=>{const r=indexedDB.open('ficusin-product-drafts',1);r.onsuccess=()=>resolve(r.result);});try{return await Promise.all(['legacy-file','migration-backup:v1:legacy-file'].map(id=>new Promise<any>(resolve=>{const r=db.transaction('media').objectStore('media').get(id);r.onsuccess=()=>resolve({version:r.result.version,type:r.result.type,bytes:Array.from(new Uint8Array(r.result.bytes))});})));}finally{db.close();}});
 await expect.poll(async()=>(await read())[0].version).toBe(1);const [current,backup]=await read();expect(current).toEqual(backup);expect(current.type).toBe('image/png');expect(current.bytes.slice(0,8)).toEqual([137,80,78,71,13,10,26,10]);await page.reload();await expect(page.getByRole('img',{name:'Сохранённое фото.png',exact:true})).toBeVisible();expect(await read()).toEqual([current,backup]);

});
