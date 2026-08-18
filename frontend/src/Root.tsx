import type { ReactNode } from "react";
import StorefrontPage from "./StorefrontPage";
import AdminPage from "./AdminPage";
import ProductPage from "./ProductPage";
import FavoritesPage from "./FavoritesPage";
import AccountPage from "./AccountPage";
import CartPage from "./CartPage";
import { StoreFooter } from "./StoreFooter";
import { LoginPage, RegisterPage } from "./AuthPages";
import {
  DeliveryPage,
  OfferPage,
  PrivacyPage,
  RequisitesPage,
  ContactsPage,
} from "./LegalPages";

export default function Root() {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  const withFooter = (page: ReactNode) => <>{page}<StoreFooter /></>;
  if (path.startsWith("/product/")) {
    return withFooter(<ProductPage slug={decodeURIComponent(path.slice("/product/".length))} />);
  }
  if (path.startsWith("/account/orders/")) {
    return withFooter(<AccountPage
      section="orders"
      orderNumber={decodeURIComponent(path.slice("/account/orders/".length))}
    />);
  }
  if (path === "/") {
    return withFooter(<StorefrontPage />);
  }
  switch (path) {
    case "/login":
      return <LoginPage />;
    case "/register":
      return <RegisterPage />;
    case "/account":
      return withFooter(<AccountPage section="orders" />);
    case "/account/profile":
      return withFooter(<AccountPage section="profile" />);
    case "/account/favorites":
      return withFooter(<AccountPage section="favorites" />);
    case "/account/reviews":
      return withFooter(<AccountPage section="reviews" />);
    case "/admin":
      return <AdminPage />;
    case "/favorites":
      return withFooter(<FavoritesPage />);
    case "/cart":
      return withFooter(<CartPage />);
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
      return withFooter(<StorefrontPage />);
  }
}
