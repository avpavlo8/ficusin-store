import {expect,test,type Page} from '@playwright/test';
import {owner} from './helpers';
const categories=[{id:1,parentId:null,name:'Растения',slug:'plants',sortOrder:1,productsCount:0,childrenCount:1},{id:2,parentId:1,name:'Аглаонемы',slug:'aglaonema',sortOrder:2,productsCount:2,childrenCount:0}];
const passport={origin:'',lighting:'Рассеянный свет',watering:'Умеренный',humidity:'',temperature:'',soil:'',fertilizer:'',repotting:'',careDifficulty:'',growthRate:'',matureSize:'',toxicity:'',problems:'',pests:'',faq:[]};
const product=(id:number)=>({id,sabyId:'',sabyCode:'',slug:`plant-${id}`,name:id===1?'Аглаонема Мария':id===2?'Хлорофитум':`Растение ${id}`,latinName:'Aglaonema',shortDescription:'Старое краткое описание',description:id===2?'':'Описание растения',careInstructions:'Уход',status:'draft',featured:false,image:'/assets/hero-monstera.webp',price:1490,stock:0,sku:String(id),variantLabel:'D11',wholesaleMinQty:1,overrideFields:[],sabyFields:['stock'],passport,importantWarnings:[],externalIds:[],attributes:{light:'diffused'},categoryId:2,catalogSection:'plants',variantCount:2,updatedAt:'2026-10-15T12:00:00Z'});
const schema=[{id:1,code:'light',name:'Освещение',dataType:'enum',unit:'',options:['diffused'],optionLabels:{diffused:'Рассеянный свет'},audience:'customer',scope:'product',required:false,filterable:true,showOnPdp:true,keyCharacteristic:true,badge:false,sortOrder:1}];
const effective=['length_cm','width_cm','height_cm','weight_grams'].map((code,i)=>({id:i+2,code:`package_${code}`,name:['Длина упаковки','Ширина упаковки','Высота упаковки','Вес упаковки'][i],unit:i===3?'г':'см',dataType:'number',scope:'variant',audience:'technical',active:true,excluded:false,required:false,options:[],global:true}));
const size=(id:number,pid:number,label:string)=>({id,productId:pid,sku:String(id),label,price:1490,stock:0,wholesaleMinQty:1,active:false,archived:false,attributes:{package_length_cm:20,package_width_cm:20,package_height_cm:50,package_weight_grams:1000},externalIds:[],images:[],stockSource:'saby'});
async function fixture(page:Page,{total=2,failSave=false,variantTasks=[] as string[]}={}){
 let products=Array.from({length:total},(_,i)=>product(i+1));if(variantTasks.length)Object.assign(products[0],{variantTasks});const variants=new Map(products.map(p=>[p.id,[size(p.id*10,p.id,'D11'),size(p.id*10+1,p.id,'D17')]]));const updates:Array<Record<string,any>>=[],calls:string[]=[];let nextId=1000;
 await page.route('**/api/v1/**',async route=>{const req=route.request(),path=new URL(req.url()).pathname,method=req.method();calls.push(path);const send=(body:unknown,status=200)=>route.fulfill({status,json:body});
 if(path==='/api/v1/auth/me')return send({user:{...owner.user,adminRole:'owner'}});
 if(path==='/api/v1/admin/dashboard')return send({user:{fullName:'Владелец'},role:'owner',permissions:['dashboard.read','products.read','products.edit','products.manage','products.sync','catalog.delete'],dashboard:{products:products.length,variants:products.length*2,orders:0,customers:0,wholesalePending:0,lastSync:null,recentOrders:[]}});
 if(path==='/api/v1/admin/products'&&method==='GET')return send({products});
 if(path==='/api/v1/admin/products'&&method==='POST'){const body=req.postDataJSON();const p={...product(++nextId),...body,image:'',price:0,status:'draft'};products.push(p);variants.set(p.id,[size(++nextId,p.id,'Основной')]);return send({product:p});}
 if(path==='/api/v1/admin/categories')return send({categories});
 if(/\/categories\/\d+\/attributes$/.test(path))return send({attributes:schema});
 if(/\/categories\/\d+\/effective-attributes$/.test(path))return send({attributes:effective});
 const rows=path.match(/\/products\/(\d+)\/variants$/);if(rows)return send({variants:variants.get(Number(rows[1]))||[]});
 if(path.endsWith('/relationships'))return send({relationships:{mappings:[],suppliers:[],sales:[]}});
 if(path.endsWith('/price-proposals'))return send({proposals:[]});if(path.endsWith('/media'))return send({media:[]});
 if(path==='/api/v1/admin/collection-definitions')return send({collections:[{id:1,title:'Для тёмной комнаты',slug:'shade',active:true,mode:'dynamic',rules:[{attribute:'light',operator:'eq',value:'diffused'}],products:[1],coverImage:'/assets/hero-monstera.webp',sortOrder:1}]});
 if(path.endsWith('/collection-definitions/preview'))return send({products:products.map(p=>p.id)});
 if(path==='/api/v1/admin/attributes')return send({attributes:schema.map(a=>({...a,active:true,options:[{code:'diffused',label:'Рассеянный свет',active:true}]}))});
 if(path==='/api/v1/admin/catalog-filters')return send({filters:[]});if(path.endsWith('/reviews'))return send({reviews:[]});
 if(path.endsWith('/products/publish'))return send({published:[1],blocked:[{productId:2,name:'Хлорофитум',reason:'Нет активного размера'}]});
 const update=path.match(/\/products\/(\d+)$/);if(update&&method==='PATCH'){const id=Number(update[1]),body=req.postDataJSON();updates.push({id,body});if(failSave)return send({error:'Ошибка сохранения'},500);for(const v of body.variants||[]){const current=variants.get(id)||[];const old=current.find(x=>x.id===v.id);if(old)Object.assign(old,v,{price:v.priceMinor/100,attributes:{...old.attributes,...v.attributes}});else current.push({...size(++nextId,id,v.label),...v,id:nextId,price:v.priceMinor/100});variants.set(id,current)}const i=products.findIndex(p=>p.id===id);const {variants:_,...patch}=body;products[i]={...products[i],...patch};return send({product:products[i]})}
 return send({});});return{updates,calls,variants};
}
async function open(page:Page){await page.goto('/admin?section=products');await expect(page.locator('table.products tbody tr').first()).toBeVisible();if(process.env.CATALOG_VISUAL_DIR)await page.screenshot({path:`${process.env.CATALOG_VISUAL_DIR}/list-${test.info().project.name}.png`,fullPage:false})}
async function edit(page:Page){await page.getByRole('link',{name:'Аглаонема Мария',exact:true}).click();return page.getByRole('dialog')}

async function manualCollection(page:Page,fail=false){
 await fixture(page);
 let collection={id:1,title:'Ручная подборка',slug:'manual',note:'',coverUrl:'/assets/hero-monstera.webp',active:false,mode:'manual',rules:[],products:[1],sortOrder:1};
 const writes:any[]=[];
 await page.route('**/api/v1/admin/collection-definitions',route=>route.fulfill({json:{collections:[collection]}}));
 await page.route('**/api/v1/admin/collection-definitions/1',route=>{const body=route.request().postDataJSON();writes.push(body);if(fail)return route.fulfill({status:500,json:{error:'Ошибка записи подборки'}});collection={...collection,...body};return route.fulfill({json:{collection}})});
 return writes;
}

test('@desktop follow-up selected size survives tabs and saving a new copy keeps the editor usable',async({page})=>{
 await fixture(page);await open(page);const d=await edit(page);
 await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await d.getByRole('button',{name:/D17/}).click();
 await d.getByRole('button',{name:'Основное',exact:true}).click();await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();
 await expect(d.getByLabel('Название размера')).toHaveValue('D17');
 await d.getByRole('button',{name:'Копировать как новый размер'}).click();await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();
 await expect(d.getByRole('status')).toContainText('Товар и размеры сохранены');await expect(d.getByLabel('Название размера')).toBeVisible();
 await expect(d.getByText('Размеров пока нет. Добавьте первый размер.')).toHaveCount(0);
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
 await e.getByLabel('Название',{exact:true}).fill('Для офиса');await expect(e.getByLabel('Адрес подборки')).toHaveValue('dlya-ofisa');page.once('dialog',dialog=>void dialog.dismiss());await e.getByRole('button',{name:'Отмена',exact:true}).click();await expect(e).toBeVisible();
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

test('@desktop follow-up scroll resets between sections and an unset boolean can explicitly become false',async({page})=>{
 const f=await fixture(page);
 await page.route('**/api/v1/admin/categories/2/attributes',route=>route.fulfill({json:{attributes:[...schema,{...schema[0],id:20,code:'flowering',name:'Цветение',dataType:'boolean',options:[]}]}}));
 await open(page);const d=await edit(page);await d.getByRole('button',{name:'Уход и FAQ',exact:true}).click();await d.getByLabel('Важные предупреждения').click();
 expect(await d.locator('.product-editor-content').evaluate(el=>el.scrollTop)).toBeGreaterThan(0);
 await d.getByRole('button',{name:'Характеристики',exact:true}).click();await expect.poll(()=>d.locator('.product-editor-content').evaluate(el=>el.scrollTop)).toBe(0);
 await expect(d.getByLabel('Цветение',{exact:true})).toHaveValue('');await d.getByLabel('Цветение',{exact:true}).selectOption('false');
 await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(d.getByRole('status')).toContainText('Товар и размеры сохранены');expect(f.updates[0].body.attributes.flowering).toBe(false);
 await expect(d.getByRole('link',{name:'На сайте ↗'})).toHaveCount(0);
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

test('@desktop @phone follow-up catalog checkboxes have aligned native geometry without overflowing switch thumbs',async({page})=>{
 await fixture(page);await open(page);const d=await edit(page);await d.getByText('Дополнительные действия',{exact:true}).click();
 const featured=d.getByLabel('Выделять товар в каталоге');
 const inspect=async(input:any)=>input.evaluate((el:HTMLInputElement)=>{const label=el.closest('label')!;const box=el.getBoundingClientRect(),row=label.getBoundingClientRect();return{width:box.width,height:box.height,left:Math.round(box.left-row.left),rowHeight:row.height,appearance:getComputedStyle(el).appearance,thumb:getComputedStyle(el,'::after').content}});
 let geometry=await inspect(featured);expect(geometry.width).toBe(18);expect(geometry.height).toBe(18);expect(geometry.left).toBeLessThanOrEqual(1);expect(geometry.rowHeight).toBeLessThanOrEqual(48);expect(geometry.appearance).not.toBe('none');expect(geometry.thumb).toBe('none');
 await featured.scrollIntoViewIfNeeded();
 if(process.env.CATALOG_VISUAL_DIR)await page.screenshot({path:`${process.env.CATALOG_VISUAL_DIR}/main-aligned-${test.info().project.name}.png`});
 await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();geometry=await inspect(d.getByLabel('Размер в продаже',{exact:true}));expect(geometry.width).toBe(18);expect(geometry.left).toBeLessThanOrEqual(1);expect(geometry.thumb).toBe('none');expect(geometry.rowHeight).toBeLessThanOrEqual(72);
 await d.getByLabel('Размер в продаже',{exact:true}).scrollIntoViewIfNeeded();
 if(test.info().project.name==='desktop'){const center=async(input:any)=>input.evaluate((el:HTMLElement)=>{const rect=el.getBoundingClientRect();return rect.top+rect.height/2});expect(Math.abs(await center(d.getByLabel('Размер в продаже',{exact:true}))-await center(d.getByLabel('Оптовый минимум, шт.',{exact:true})))).toBeLessThanOrEqual(2)}
 if(process.env.CATALOG_VISUAL_DIR)await page.screenshot({path:`${process.env.CATALOG_VISUAL_DIR}/sizes-aligned-${test.info().project.name}.png`});
});

test('@desktop редактор без ИИ, краткого описания и старого предпросмотра',async({page})=>{const f=await fixture(page);await open(page);const d=await edit(page);await expect(d.getByLabel('Статус товара')).toHaveValue('draft');await expect(d.getByText(/AI|ИИ|Сгенерировать|SEO/)).toHaveCount(0);await expect(d.getByLabel('Короткое описание')).toHaveCount(0);await d.getByRole('button',{name:'Уход и FAQ',exact:true}).click();await expect(d.getByLabel('Введение в уход')).toBeVisible();await expect(d.getByText('Так инструкция выглядит для покупателей')).toHaveCount(0);expect(f.calls.filter(p=>p.includes('ai-'))).toEqual([])});

test('@desktop единое сохранение товара, двух размеров и ОВХ с проверкой после перезагрузки',async({page})=>{const f=await fixture(page);await open(page);let d=await edit(page);await d.getByLabel('Описание',{exact:true}).fill('Новое описание');await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await d.getByLabel('Название размера').fill('D11 обновлён');await d.getByLabel('Вес упаковки, кг').fill('1.25');await d.getByRole('button',{name:/D17/u}).click();await d.getByLabel('Цена на сайте, ₽').fill('1990');await d.getByRole('button',{name:'Основное',exact:true}).click();await expect(d.getByLabel('Описание',{exact:true})).toHaveValue('Новое описание');await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await d.getByRole('button',{name:/D11 обновлён/}).click();await expect(d.getByLabel('Вес упаковки, кг')).toHaveValue('1.25');await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(d.getByRole('status')).toContainText('Товар и размеры сохранены');expect(f.updates).toHaveLength(1);expect(f.updates[0].body.description).toBe('Новое описание');expect(f.updates[0].body.variants).toHaveLength(2);expect(f.updates[0].body.variants[0].attributes.package_weight_grams).toBe(1250);await page.reload();d=page.getByRole('dialog');await expect(d.getByLabel('Название размера')).toHaveValue('D11 обновлён');await expect(d.getByLabel('Вес упаковки, кг')).toHaveValue('1.25')});

test('@desktop ошибка сохранения сохраняет черновик и не открывает следующий товар',async({page})=>{await fixture(page,{failSave:true});await open(page);const d=await edit(page);await d.getByLabel('Описание',{exact:true}).fill('Ручной текст');await d.getByRole('button',{name:'Сохранить и следующий',exact:true}).click();await expect(d.getByRole('alert')).toContainText('Ошибка сохранения');await expect(d.getByLabel('Описание',{exact:true})).toHaveValue('Ручной текст');await expect(d.getByLabel('Название',{exact:true})).toHaveValue('Аглаонема Мария')});

test('@desktop выделение сбрасывается при поиске и смене страницы',async({page})=>{await fixture(page,{total:65});await open(page);await page.getByLabel('Выбрать товары на странице').check();await expect(page.getByRole('region',{name:'Выбранные товары'})).toContainText('Выбрано: 50');await page.getByLabel('Поиск товаров').fill('Хлорофитум');await expect(page.getByRole('region',{name:'Выбранные товары'})).toHaveCount(0);await expect(page.locator('table.products tbody tr')).toHaveCount(1);await page.getByRole('button',{name:'Сбросить фильтры',exact:true}).click();await page.getByLabel('Выбрать Аглаонема Мария',{exact:true}).check();await page.getByRole('button',{name:'Далее →',exact:true}).click();await expect(page.locator('table.products tbody tr')).toHaveCount(15);await expect(page.getByRole('region',{name:'Выбранные товары'})).toHaveCount(0)});

test('@desktop пробелы не ломают поиск, URL восстанавливает контекст, пустой список имеет сброс',async({page})=>{await fixture(page);await open(page);await page.getByLabel('Поиск товаров').fill('  Аглаонема   Мария  ');await expect(page.locator('table.products tbody tr')).toHaveCount(1);await page.reload();await expect(page.getByLabel('Поиск товаров')).toHaveValue('  Аглаонема   Мария  ');await page.getByLabel('Поиск товаров').fill('Такого товара нет');await expect(page.getByRole('heading',{name:'Товары не найдены'})).toBeVisible();await page.locator('.catalog-empty').getByRole('button',{name:'Сбросить фильтры'}).click();await expect(page.locator('table.products tbody tr')).toHaveCount(2);await expect(page.getByRole('heading',{name:'Отзывы'})).toHaveCount(0)});

test('@desktop копия размера без складского остатка и внешних идентификаторов',async({page})=>{const f=await fixture(page);await open(page);const d=await edit(page);await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await d.getByRole('button',{name:'Копировать как новый размер'}).click();await expect(d.getByRole('button',{name:'В архив после сохранения',exact:true})).toHaveCount(0);expect(f.updates).toHaveLength(0);await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(d.getByRole('status')).toContainText('Товар и размеры сохранены');const v=f.updates[0].body.variants.find((v:any)=>v.id<0);expect(v.active).toBe(false);expect(v.externalIds).toEqual([]);expect(v.stock).toBeUndefined()});

test('@desktop создание открывает тот же редактор скрытого черновика',async({page})=>{await fixture(page);await open(page);await page.getByRole('button',{name:'Добавить товар',exact:true}).click();let d=page.getByRole('dialog');await d.getByLabel('Название товара').fill('Новый фикус');await d.getByRole('button',{name:'Создать черновик и продолжить'}).click();d=page.getByRole('dialog');await expect(d.getByLabel('Название',{exact:true})).toHaveValue('Новый фикус');await expect(d.getByLabel('Статус товара')).toHaveValue('draft');await expect(d.getByRole('button',{name:'Фотографии',exact:true})).toBeVisible();await expect(d.getByLabel('URL фото')).toHaveCount(0)});

test('@desktop несохранённые правки защищены при закрытии',async({page})=>{await fixture(page);await open(page);const d=await edit(page);await d.getByLabel('Описание',{exact:true}).fill('Несохранённый текст');page.once('dialog',confirmation=>void confirmation.dismiss());await d.locator('.product-editor-footer').getByRole('button',{name:'Закрыть',exact:true}).click();await expect(d).toBeVisible();await expect(d.getByLabel('Описание',{exact:true})).toHaveValue('Несохранённый текст')});

test('@desktop фокус указывает на ошибочное поле',async({page})=>{await fixture(page);await open(page);const d=await edit(page);await d.getByLabel('Название',{exact:true}).fill('');await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(d.getByRole('alert')).toContainText('Укажите название');await expect(d.getByLabel('Название',{exact:true})).toBeFocused()});

test('@desktop массовая публикация показывает конкретный пропуск',async({page})=>{await fixture(page);await open(page);await page.getByLabel('Выбрать товары на странице').check();await page.getByRole('button',{name:'Опубликовать',exact:true}).click();await page.getByRole('button',{name:'Подтвердить публикацию (2)',exact:true}).click();await expect(page.locator('.admin-bulk-result')).toContainText('Успешно: 1 · Пропущено: 1 · Ошибка: 0');await expect(page.locator('.admin-bulk-result')).toContainText('Нет активного размера')});

test('@phone список и редактор без горизонтальной прокрутки, черновик размера сохраняется между разделами',async({page})=>{await fixture(page);await open(page);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);const d=await edit(page);await expect(d.getByRole('heading',{name:'Аглаонема Мария',exact:true})).toBeVisible();await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await d.getByLabel('Название размера').fill('Горшок 14 см');await d.getByRole('button',{name:'Основное',exact:true}).click();await d.getByRole('button',{name:'Размеры и цены',exact:true}).click();await expect(d.getByLabel('Название размера')).toHaveValue('Горшок 14 см');expect(await d.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true);expect((await d.locator('.product-editor-footer').boundingBox())!.height).toBeLessThan(155);if(process.env.CATALOG_VISUAL_DIR)await page.screenshot({path:`${process.env.CATALOG_VISUAL_DIR}/editor-${test.info().project.name}.png`})});

test('@desktop категории учитывают вложенные товары, код характеристики создаётся из названия',async({page})=>{await fixture(page);await page.goto('/admin?section=categories');const root=page.locator('tbody tr').filter({hasText:'Растения'});await expect(root).toContainText('2');await expect(root.getByRole('button',{name:'Удалить',exact:true})).toBeDisabled();await page.getByRole('button',{name:'Характеристики',exact:true}).click();await page.getByRole('button',{name:'Добавить характеристику'}).click();const d=page.locator('.admin-pim-editor');await d.getByLabel('Название',{exact:true}).fill('Вхождение в подборке');await d.getByText('Технический код',{exact:true}).click();await expect(d.getByLabel('Код характеристики')).toHaveValue('vhozhdenie_v_podborke')});

test('@desktop предпросмотр правил подборки показывает весь состав до сохранения',async({page})=>{const f=await fixture(page,{total:14});await page.goto('/admin?section=collections&collection=1');await expect(page.getByText('Подходит: 14 товаров',{exact:true})).toBeVisible();await expect(page.locator('.admin-collection-preview tbody tr')).toHaveCount(14);await expect(page.locator('.admin-collection-preview')).toContainText('1 · D11');await expect(page.locator('.admin-collection-preview')).toContainText('Освещение равно Рассеянный свет');expect(f.updates).toHaveLength(0)});

test('@desktop Назад спрашивает один раз и сохраняет адрес незакрытого черновика',async({page})=>{await fixture(page);await open(page);const d=await edit(page);await d.getByLabel('Описание',{exact:true}).fill('Сохранить черновик');const address=page.url();let confirms=0;page.on('dialog',async confirmation=>{confirms++;await confirmation.dismiss()});await page.goBack();await expect(d).toBeVisible();await expect(d.getByLabel('Описание',{exact:true})).toHaveValue('Сохранить черновик');await expect(page).toHaveURL(address);expect(confirms).toBe(1)});

 test('@desktop обязательное поле размера исключает карточку из списка без замечаний',async({page})=>{await fixture(page,{variantTasks:['Размер D11 · артикул 10: Вес упаковки']});await open(page);await expect(page.getByRole('button',{name:'Черновики без замечаний 0',exact:true})).toBeVisible();const row=page.locator('table.products tbody tr').filter({hasText:'Аглаонема Мария'});await expect(row).toContainText('Вес упаковки');await page.getByRole('button',{name:'Характеристики 1',exact:true}).click();await expect(page.locator('table.products tbody tr')).toHaveCount(1)});

test('@desktop уход и вопросы после сохранения не оставляют ложный несохранённый черновик',async({page})=>{await fixture(page);await open(page);let d=await edit(page);await d.getByRole('button',{name:'Уход и FAQ',exact:true}).click();await d.getByLabel('Вопросы и ответы').fill('Как поливать? | После просыхания.\n');await d.getByLabel('Важные предупреждения').fill('Не ставить рядом с батареей.\n');await d.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(d.getByRole('status')).toContainText('Товар и размеры сохранены');await expect(d.getByRole('button',{name:'Сохранить изменения',exact:true})).toBeDisabled();await page.reload();d=page.getByRole('dialog');await expect(d.getByLabel('Вопросы и ответы')).toHaveValue('Как поливать? | После просыхания.');await expect(d.getByLabel('Важные предупреждения')).toHaveValue('Не ставить рядом с батареей.')});
