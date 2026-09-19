import { defineConfig } from "orval";

export default defineConfig({
  management: {
    input: "./openapi.json",
    output: {
      target: "./src/api/generated/management.ts",
      schemas: "./src/api/generated/models",
      client: "react-query",
      mode: "tags-split",
      override: {
        mutator: { path: "./src/api/client.ts", name: "apiFetch" },
        fetch: { includeHttpResponseReturnType: false },
      },
    },
  },
});
