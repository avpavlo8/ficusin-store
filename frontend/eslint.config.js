import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": [
        "error",
        {
          allowConstantExport: true,
          allowExportNames: [
            "STORAGE_EVENT", "useStoreUser", "orderCategoryTree",
            "normalizeProcurementOrderDetail", "procurementStatusLabels", "procurementSourceLabels", "procurementParserLabels",
            "channelLabel", "batchStatusLabel", "actionStatus",
            "availabilityLabel", "salesChannelLabel", "salesSyncLabel", "recommendationStatusLabel",
            "recommendationEmptyTitle", "recommendationEmptyText", "integrationChannelLabel",
            "updateAvailability", "setExclusion", "updateRequestStatus",
            "sabyFieldLabels", "money", "roles", "roleLabel", "paymentLabels", "paymentMethodLabels",
            "orderStatuses", "statusLabels", "api", "selectZeroNumberInput",
          ],
        },
      ],
    },
  },
);
