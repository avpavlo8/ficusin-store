import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import Root from "./Root";
import { registerServiceWorker } from "./lib/pwa";
import { initAnalytics } from "./lib/analytics";
import "./styles.css";
import "./styles/account-mobile-fix.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);

registerServiceWorker();
initAnalytics();
