import App from "./App";
import AdminPage from "./AdminPage";
import { AccountPage, LoginPage, RegisterPage } from "./AuthPages";
import {
  DeliveryPage,
  OfferPage,
  PrivacyPage,
  RequisitesPage,
} from "./LegalPages";

export default function Root() {
  switch (window.location.pathname.replace(/\/+$/, "") || "/") {
    case "/login":
      return <LoginPage />;
    case "/register":
      return <RegisterPage />;
    case "/account":
      return <AccountPage />;
    case "/admin":
      return <AdminPage />;
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
