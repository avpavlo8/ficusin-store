import { lazy, Suspense, type ReactNode } from "react";
import StorefrontPage from "./StorefrontPage";
import ProductPage from "./ProductPage";
import { StoreFooter } from "./StoreFooter";

const AdminPage = lazy(() => import("./AdminPage"));
const FavoritesPage = lazy(() => import("./FavoritesPage"));
const AccountPage = lazy(() => import("./AccountPage"));
const AccountOrderPage = lazy(() => import("./AccountOrderPage"));
const CartPage = lazy(() => import("./CartPage"));
const NotFoundPage = lazy(() => import("./NotFoundPage"));
const LoginPage = lazy(() => import("./AuthPages").then((module) => ({ default: module.LoginPage })));
const RegisterPage = lazy(() => import("./AuthPages").then((module) => ({ default: module.RegisterPage })));
const DeliveryPage = lazy(() => import("./LegalPages").then((module) => ({ default: module.DeliveryPage })));
const OfferPage = lazy(() => import("./LegalPages").then((module) => ({ default: module.OfferPage })));
const PrivacyPage = lazy(() => import("./LegalPages").then((module) => ({ default: module.PrivacyPage })));
const RequisitesPage = lazy(() => import("./LegalPages").then((module) => ({ default: module.RequisitesPage })));
const ContactsPage = lazy(() => import("./LegalPages").then((module) => ({ default: module.ContactsPage })));

function RouteLoading() {
  return <main className="route-loading" aria-live="polite"><span>Загружаем…</span></main>;
}

export default function Root() {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  const ready = (page: ReactNode) => <Suspense fallback={<RouteLoading />}>{page}</Suspense>;
  const withFooter = (page: ReactNode) => <Suspense fallback={<RouteLoading />}>{page}<StoreFooter /></Suspense>;
  if (path.startsWith("/product/")) {
    return withFooter(<ProductPage slug={decodeURIComponent(path.slice("/product/".length))} />);
  }
  if (path.startsWith("/account/orders/")) {
    return withFooter(<AccountOrderPage
      orderNumber={decodeURIComponent(path.slice("/account/orders/".length))}
    />);
  }
  if (path === "/") {
    return withFooter(<StorefrontPage />);
  }
  switch (path) {
    case "/login":
      return ready(<LoginPage />);
    case "/register":
      return ready(<RegisterPage />);
    case "/account":
      return withFooter(<AccountPage section="orders" />);
    case "/account/profile":
      return withFooter(<AccountPage section="profile" />);
    case "/account/favorites":
      return withFooter(<AccountPage section="favorites" />);
    case "/account/reviews":
      return withFooter(<AccountPage section="reviews" />);
    case "/admin":
      return ready(<AdminPage />);
    case "/favorites":
      return withFooter(<FavoritesPage />);
    case "/cart":
      return withFooter(<CartPage />);
    case "/checkout":
      return withFooter(<CartPage checkout />);
    case "/offer":
      return withFooter(<OfferPage />);
    case "/privacy":
      return withFooter(<PrivacyPage />);
    case "/requisites":
      return withFooter(<RequisitesPage />);
    case "/contacts":
      return withFooter(<ContactsPage />);
    case "/delivery-and-returns":
      return withFooter(<DeliveryPage />);
    default:
      return withFooter(<NotFoundPage />);
  }
}
