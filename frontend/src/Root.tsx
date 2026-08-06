import App from "./App";
import StorefrontPage from "./StorefrontPage";
import AdminPage from "./AdminPage";
import ProductPage from "./ProductPage";
import FavoritesPage from "./FavoritesPage";
import AccountPage from "./AccountPage";
import { LoginPage, RegisterPage } from "./AuthPages";
import {
  DeliveryPage,
  OfferPage,
  PrivacyPage,
  RequisitesPage,
} from "./LegalPages";

export default function Root() {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  if (path.startsWith("/product/")) {
    return <ProductPage slug={decodeURIComponent(path.slice("/product/".length))} />;
  }
  if (path.startsWith("/account/orders/")) {
    return <AccountPage
      section="orders"
      orderNumber={decodeURIComponent(path.slice("/account/orders/".length))}
    />;
  }
  // The old App still owns the cart drawer and checkout, so it stays behind
  // ?cart=1 and ?paid=... The storefront is what a visitor lands on.
  if (path === "/" && !window.location.search) {
    return <StorefrontPage />;
  }
  switch (path) {
    case "/login":
      return <LoginPage />;
    case "/register":
      return <RegisterPage />;
    case "/account":
      return <AccountPage section="orders" />;
    case "/account/profile":
      return <AccountPage section="profile" />;
    case "/account/favorites":
      return <AccountPage section="favorites" />;
    case "/admin":
      return <AdminPage />;
    case "/favorites":
      return <FavoritesPage />;
    case "/offer":
      return <OfferPage />;
    case "/privacy":
      return <PrivacyPage />;
    case "/requisites":
      return <RequisitesPage />;
    case "/delivery-and-returns":
      return <DeliveryPage />;
    default:
      return <App />;
  }
}
