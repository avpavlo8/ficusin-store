import { useEffect, useMemo, useState } from "react";
import CheckoutHost, { CartProduct } from "./CheckoutHost";
import { StoreHeader, type HeaderMenuItem } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";

export default function CartPage({ checkout = false }: { checkout?: boolean }) {
  const [products, setProducts] = useState<CartProduct[]>([]);
  const [categories, setCategories] = useState<Array<{ id:number; parentId:number|null; name:string; sortOrder:number }>>([]);
  const [cart, setCart] = useSharedCart();
  useEffect(() => {
    fetch("/api/v1/catalog").then((response) => response.json())
      .then((data: { products?: CartProduct[] }) => setProducts(data.products || [])).catch(() => setProducts([]));
  }, []);
  useEffect(() => { fetch("/api/v1/categories").then((response) => response.json()).then((body: { categories?: typeof categories }) => setCategories(body.categories || [])).catch(() => setCategories([])); }, []);
  const headerMenus = useMemo(() => {
    const children = new Map<number|null,typeof categories>();
    categories.forEach((item) => children.set(item.parentId,[...(children.get(item.parentId)||[]),item]));
    const order = (items:typeof categories) => [...items].sort((a,b)=>a.sortOrder-b.sortOrder||a.name.localeCompare(b.name,"ru"));
    const branch = (item:typeof categories[number]):HeaderMenuItem => ({id:item.id,label:item.name,children:order(children.get(item.id)||[]).map(branch)});
    const catalog = order(children.get(null)||[]).map(branch);
    const plantRoot = categories.find((item)=>item.parentId==null&&/растен/i.test(item.name));
    const leaves = (parentId:number):typeof categories => order(children.get(parentId)||[]).flatMap((item)=>children.get(item.id)?.length?leaves(item.id):[item]);
    return {catalog,plants:plantRoot?leaves(plantRoot.id).map((item)=>({id:item.id,label:item.name})):[]};
  },[categories]);
  return <main className={checkout ? "cart-page checkout-page" : "cart-page"}>
    <StoreHeader cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} homeNavigation catalogMenuItems={headerMenus.catalog} plantMenuItems={headerMenus.plants} onHomeCategoryPick={(id) => { window.location.assign(`/?category=${id}#catalog`); }} />
    <CheckoutHost cart={cart} products={products} cartOpen={!checkout}
      cartPage={!checkout} checkoutPage={checkout} onCartOpenChange={(open) => { if (!open && !checkout) window.location.assign("/#catalog"); }} onCartChange={setCart} />
  </main>;
}
