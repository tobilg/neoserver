import { defineConfig } from "@playwright/test";

// No live server, bootstrap token, Docker, or global setup. API writes are mocked.
export default defineConfig({
  testDir: "./browser-tests",
  outputDir: "./test-results/browser",
  fullyParallel: true,
  workers: 2,
  // A failed query retries twice with exponential backoff before the error
  // panel renders, so error-state assertions need more than Playwright's 5s
  // default on a loaded CI runner.
  expect: { timeout: 15_000 },
  use: {
    baseURL: "http://127.0.0.1:5178",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command:
      process.env.UI_TEST_BUILD === "true"
        ? "npx vite preview --host 127.0.0.1 --port 5178 --strictPort"
        : "npm run dev -- --host 127.0.0.1 --port 5178 --strictPort",
    url: "http://127.0.0.1:5178/admin/login",
    reuseExistingServer: false,
  },
});
