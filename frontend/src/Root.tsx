import App from "./App";
import AdminPage from "./AdminPage";
import ProductPage from "./ProductPage";
import FavoritesPage from "./FavoritesPage";
import { AccountPage, LoginPage, RegisterPage } from "./AuthPages";
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
  switch (path) {
    case "/login":
      return <LoginPage />;
    case "/register":
      return <RegisterPage />;
    case "/account":
      return <AccountPage />;
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
