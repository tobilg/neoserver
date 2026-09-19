import { defineConfig } from "@playwright/test";

// The console suite runs against a real neoserver, not the Vite dev server, so
// that a passing `publish-layer` genuinely means a layer reached the catalog.
// `scripts/console/run-e2e.sh` brings the fixture up and sets these.
const consoleURL = process.env.CONSOLE_URL;

if (!consoleURL) {
  throw new Error(
    "CONSOLE_URL is not set. Run the suite through `make ui-e2e`, which starts " +
      "the disposable server fixture, or point CONSOLE_URL at a running " +
      "console (for example http://localhost:19100/admin).",
  );
}

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results/live",
  globalSetup: "./e2e/global-setup.ts",
  // A real server is slower and less deterministic than route mocks; retry once
  // in CI rather than treating a single flake as a failure.
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: consoleURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    // Only the disposable local Compose fixture opts into this mapping.
    // Preserve the actual issuer hostname without editing the host's DNS files.
    launchOptions:
      process.env.CONSOLE_LOCAL_KEYCLOAK === "true"
        ? {
            args: [
              "--host-resolver-rules=MAP keycloak 127.0.0.1",
              "--proxy-bypass-list=keycloak;localhost;127.0.0.1",
            ],
          }
        : undefined,
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
