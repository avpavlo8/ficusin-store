import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

const adminOwner = {
  ...owner,
  user: { ...owner.user, adminRole: "owner" as const },
};

const adminProduct = {
  id: 1,
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

async function unifiedPhotoFixture(page: import('@playwright/test').Page, role: 'owner'|'manager'|'administrator') {
  const user={...owner,user:{...owner.user,adminRole:role}};
  await mockApi(page,user);
  let saved={...adminProduct};
  const variant={id:10,productId:1,sku:'1',label:'D12',price:1490,stock:5,wholesaleMinQty:1,active:true,archived:false,attributes:{pot_diameter_cm:12,height_cm:35},externalIds:adminProduct.externalIds,images:[]};
  let plan={variants:[]};let uploadCount=0;let nextID=100;
  type Photo={id:number;productId:number;variantId?:number;url:string;primary:boolean;sortOrder:number};
  const media:Photo[]=[{id:7,productId:1,url:adminProduct.image,primary:true,sortOrder:0},{id:9,productId:1,variantId:10,url:'/assets/hero-monstera.webp?variant=10',primary:true,sortOrder:0}];
  const mutations:Array<{path:string;method:string}>=[];
  await page.route('**/api/v1/admin/**',async route=>{
    const req=route.request(),path=new URL(req.url()).pathname,method=req.method();
    const send=(json:unknown,status=200)=>route.fulfill({status,json});
    if(!['GET','HEAD'].includes(method))mutations.push({path,method});
    if(path.endsWith('/dashboard'))return send({role,user:user.user,permissions:['products.read','products.edit',...(role==='administrator'?[]:['products.manage'])],dashboard:{products:1,variants:1,orders:0,customers:1,wholesalePending:0,recentOrders:[]}});
    if(path==='/api/v1/admin/products')return send({products:[saved]});
    if(path==='/api/v1/admin/categories')return send({categories:[{id:1,parentId:null,name:'Растения',slug:'plants',sortOrder:1,productsCount:1}]});
    if(path.endsWith('/attributes'))return send({attributes:[]});
    if(path.endsWith('/variants'))return send({variants:[variant]});
    if(path.endsWith('/collection-definitions'))return send({collections:[]});
    if(path.endsWith('/catalog-editor/references'))return send({categories:[],folders:[]});
    if(path.endsWith('/editor-plan/sync-existing'))return send({queued:0,skipped:0,warnings:[]});
    if(path.endsWith('/editor-plan')){if(method==='PUT'){if(role==='administrator')return send({error:'Недостаточно прав'},403);plan=req.postDataJSON().config;}return send({config:plan,publications:[],links:{},barcodes:{},publicationMode:'disabled'});}
    if(path.endsWith('/catalog-editor/settings'))return send({values:{'cdek.packaging_season':'summer'}});
    if(path==='/api/v1/admin/products/1'&&method==='PATCH'){saved={...saved,...req.postDataJSON()};return send({product:saved});}
    const photoRoute=path.match(/\/(products|variants)\/(\d+)\/media(?:\/(copy|\d+)(?:\/(primary))?)?$/);
    if(photoRoute){
      const [,kind,rawID,action,primary]=photoRoute,id=Number(rawID);
      const belongs=(photo:Photo)=>kind==='products'?photo.productId===id&&photo.variantId===undefined:photo.variantId===id;
      if(method==='GET')return send({media:media.filter(belongs)});
      if(action==='copy'&&method==='POST'){
        const source=media.find(photo=>photo.id===req.postDataJSON().mediaId);
        if(!source||kind!=='variants'||id!==10)return send({error:'Чужое фото'},400);
        const copy={...source,id:++nextID,variantId:10,primary:false,sortOrder:media.filter(belongs).length};media.push(copy);return send({media:copy},201);
      }
      if(primary){for(const photo of media.filter(belongs))photo.primary=photo.id===Number(action);return send({ok:true});}
      if(method==='DELETE'){
        if(role==='administrator')return send({error:'Недостаточно прав'},403);
        const at=media.findIndex(photo=>belongs(photo)&&photo.id===Number(action));if(at<0)return send({error:'Фото не найдено'},404);media.splice(at,1);return send({deleted:true});
      }
      if(method==='POST'){
        uploadCount++;const photo={id:++nextID,productId:1,...(kind==='variants'?{variantId:id}:{}),url:`/assets/hero-monstera.webp?uploaded=${nextID}`,primary:false,sortOrder:media.filter(belongs).length};media.push(photo);return send({media:photo},201);
      }
    }
    return send({});
  });
  return {media,mutations,get uploadCount(){return uploadCount;}};
}
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=','base64');

for(const role of ['owner','manager'] as const)test(`@desktop @phone ${role} opens PDP photos in the unified editor and saves its draft gallery`,async({page})=>{
  const f=await unifiedPhotoFixture(page,role);await page.goto('/product/1');await page.getByRole('button',{name:'Фотографии',exact:true}).click();
  await expect(page).toHaveURL(/\/admin\?section=products&product=1&editor=media#product-media$/);
  const editor=page.locator('.editor-page'),gallery=editor.getByRole('complementary',{name:'Фото и видео товара',exact:true});
  await expect(gallery).toBeFocused();await expect(page.getByRole('dialog')).toHaveCount(0);
  await gallery.locator('input[type="file"]').setInputFiles({name:'plant.png',mimeType:'image/png',buffer:png});
  const uploaded=gallery.getByRole('button',{name:/^Фото \d+: plant.png\./});await expect(uploaded).toBeVisible();expect(f.uploadCount).toBe(0);
  // The same accessible ordering gesture as the catalogue moves the new image
  // past the variant photo and then onto the one shared product cover.
  await uploaded.press('Alt+ArrowLeft');await uploaded.press('Alt+ArrowLeft');
  await expect(gallery.locator('.editor-gallery-cover img')).toHaveAttribute('alt','plant.png');
  await editor.getByRole('tab',{name:'Вопросы и ответы',exact:true}).click();
  await page.goto('/product/1');await page.getByRole('button',{name:'Фотографии',exact:true}).click();
  await expect(editor.getByRole('tab',{name:'Товар',exact:true})).toHaveAttribute('aria-selected','true');
  await expect(gallery).toBeFocused();await expect(gallery.locator('.editor-gallery-cover img')).toHaveAttribute('alt','plant.png');expect(f.uploadCount).toBe(0);
  await editor.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(editor.locator('.editor-notice')).toContainText('Изменения товара сохранены');
  expect(f.uploadCount).toBe(1);const cover=f.media.find(photo=>photo.variantId===undefined&&photo.primary)!;expect(cover.url).toContain('uploaded=');
  await expect(gallery.locator('.editor-gallery-cover img')).toHaveAttribute('src',cover.url);
  await gallery.getByRole('button',{name:/^Удалить фото /}).first().click();
  await editor.getByRole('button',{name:'Сохранить',exact:true}).click();await expect.poll(()=>f.mutations.filter(req=>req.method==='DELETE').length).toBe(1);
  expect(f.mutations.find(req=>req.method==='DELETE')!.path).toMatch(/^\/api\/v1\/admin\/variants\/10\/media\/\d+$/);
  await page.reload();await expect(gallery).toBeFocused();await expect(gallery.locator('.editor-gallery-cover img')).toHaveAttribute('src',cover.url);
});

test('@desktop administrator opens the same PDP photo editor with upload access and protected deletion',async({page})=>{
  const f=await unifiedPhotoFixture(page,'administrator');await page.goto('/product/1');await page.getByRole('button',{name:'Фотографии',exact:true}).click();
  const editor=page.locator('.editor-page'),gallery=editor.getByRole('complementary',{name:'Фото и видео товара',exact:true});
  await expect(gallery).toBeFocused();await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(gallery.getByRole('button',{name:'Удалить обложку',exact:true})).toBeDisabled();
  await gallery.locator('input[type="file"]').setInputFiles({name:'admin.png',mimeType:'image/png',buffer:png});
  await expect(gallery.getByRole('button',{name:/^Фото \d+: admin.png\./})).toBeVisible();expect(f.uploadCount).toBe(0);
  await editor.getByRole('button',{name:'Сохранить',exact:true}).click();await expect(editor.locator('.editor-notice')).toContainText('Изменения товара сохранены');
  expect(f.uploadCount).toBe(1);expect(f.mutations.filter(req=>req.method==='DELETE')).toHaveLength(0);
  expect(f.mutations.filter(req=>req.path.endsWith('/editor-plan')&&req.method==='PUT')).toHaveLength(0);
  expect(f.mutations.filter(req=>req.path.endsWith('/editor-plan/sync-existing'))).toHaveLength(1);
});
