import { mergeConfig } from "vite";
import base from "./vite.config.ts";
import { procurementPreview } from "../scripts/procurement-preview-api.mjs";

// Explicitly selected local preview; never used by the production build.
export default mergeConfig(base, {
  plugins: [procurementPreview()],
  server: { host: "127.0.0.1", port: 5173, strictPort: true, proxy: {} },
});
