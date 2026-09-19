import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";

export default tseslint.config(
  {
    ignores: [
      "src/api/generated/**",
      "../../internal/admin/dist/**",
      "test-results/**",
      "playwright-report/**",
      "coverage/**",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["scripts/**/*.mjs"],
    languageOptions: { ecmaVersion: 2024, globals: globals.node },
  },
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { ecmaVersion: 2024, globals: globals.browser },
    plugins: { "react-hooks": reactHooks, "react-refresh": reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": [
        "warn",
        { allowConstantExport: true },
      ],
      "@typescript-eslint/no-explicit-any": "off",
    },
  },
  {
    // These files are generated verbatim by the shadcn CLI and are guarded by
    // the provenance check. Keep project rules from forcing local edits.
    files: ["src/components/ui/**/*.{ts,tsx}", "src/hooks/use-mobile.ts"],
    rules: {
      "react-hooks/set-state-in-effect": "off",
      // badge/button/sidebar/tabs each export a cva variant map or a hook
      // beside their components. Splitting those out is the only way to
      // satisfy the rule, and it would fork files that `shadcn add --diff` is
      // meant to keep comparable upstream.
      "react-refresh/only-export-components": "off",
    },
  },
);
