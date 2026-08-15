import StorefrontPage from "./StorefrontPage";
import AdminPage from "./AdminPage";
import ProductPage from "./ProductPage";
import FavoritesPage from "./FavoritesPage";
import AccountPage from "./AccountPage";
import CartPage from "./CartPage";
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
  if (path === "/") {
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
    case "/account/reviews":
      return <AccountPage section="reviews" />;
    case "/admin":
      return <AdminPage />;
    case "/favorites":
      return <FavoritesPage />;
    case "/cart":
      return <CartPage />;
    case "/offer":
      return <OfferPage />;
    case "/privacy":
      return <PrivacyPage />;
    case "/requisites":
      return <RequisitesPage />;
    case "/delivery-and-returns":
      return <DeliveryPage />;
    default:
      return <StorefrontPage />;
  }
}
