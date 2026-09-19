import { defineConfig } from "vitest/config";
import { loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "NEOSRV_DEV_");
  const prefix = (env.NEOSRV_DEV_BASE_PATH ?? "").replace(/\/$/, "");
  if (prefix && (!prefix.startsWith("/") || prefix.startsWith("//")))
    throw new Error(
      "NEOSRV_DEV_BASE_PATH must be an absolute path, for example /geo",
    );
  const target = env.NEOSRV_DEV_API_TARGET || "http://localhost:9000";
  return {
    base: "./",
    plugins: [
      react(),
      tailwindcss(),
      {
        name: "embedded-console-preview-assets",
        configurePreviewServer(server) {
          // Match internal/admin's relative-asset routing for direct SPA links.
          server.middlewares.use((request, _response, next) => {
            const assetAt = request.url?.lastIndexOf("/assets/") ?? -1;
            if (assetAt >= 0) request.url = request.url?.slice(assetAt);
            next();
          });
        },
      },
    ],
    resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
    build: {
      outDir: "../../internal/admin/dist",
      manifest: true,
      emptyOutDir: true,
      // The largest chunks are route-level lazy imports that each carry one big
      // vendor library: PreviewPage (MapLibre) and StyleEditorPage (CodeMirror).
      // Neither is in the initial download, and the metric that actually matters
      // -- initial JavaScript -- is enforced separately by `npm run check:budget`.
      // Raise the warning threshold deliberately so it stays silent for these
      // known chunks rather than becoming noise that hides a real regression.
      chunkSizeWarningLimit: 1000,
    },
    server: {
      proxy: Object.fromEntries(
        ["/api", "/health", "/ready", "/workspaces"].map((route) => [
          `${prefix}${route}`,
          target,
        ]),
      ),
    },
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
      exclude: [
        "e2e/**",
        "browser-tests/**",
        "scripts/**",
        "**/node_modules/**",
        "**/dist/**",
      ],
    },
  };
});
