/** Shared handles for the live end-to-end fixture. */

export interface ProvisionedFixture {
  /** Super-admin bootstrap token from `neoserver init`. */
  bootstrapToken: string;
  /** Workspace every catalog spec publishes into. */
  workspace: string;
  /** API key holding `admin` on `workspace` only. */
  workspaceAdminKey: string;
  /** API key holding `viewer` on `workspace` only. */
  viewerKey: string;
}

export const FIXTURE_PATH = "e2e/.fixture.json";

export const KEYCLOAK_USERS = {
  mapped: { username: "gis-admin", password: "gis-admin-password" },
  unmapped: { username: "no-roles", password: "no-roles-password" },
} as const;

/** Realm role that the console maps to a workspace role during the OIDC spec. */
export const MAPPED_REALM_ROLE = "gis-admins";

export function apiURL(path: string): string {
  const base = process.env.CONSOLE_API_URL ?? "http://localhost:19100";
  return `${base}${path}`;
}

export async function readFixture(): Promise<ProvisionedFixture> {
  const { readFile } = await import("node:fs/promises");
  return JSON.parse(await readFile(FIXTURE_PATH, "utf8")) as ProvisionedFixture;
}

/**
 * Signs in through the console's own token-exchange form, so the session
 * cookie and CSRF token are established exactly as they are for a real user.
 */
export async function signInWithToken(
  page: import("@playwright/test").Page,
  token: string,
  next = "/",
): Promise<void> {
  await gotoConsole(page, `/login?next=${encodeURIComponent(next)}`);
  await chooseTokenSignIn(page);
  await page.getByLabel("API key or JWT").fill(token);
  await page.getByRole("button", { name: "Start session" }).click();
  await page.waitForURL((url) => !url.pathname.endsWith("/login"));
}

/** Resolve console routes without dropping the deployment's /admin prefix. */
export function consoleURL(
  path: string,
  base = process.env.CONSOLE_URL ?? "http://localhost:19100/admin",
): string {
  return new URL(path.replace(/^\/+/, ""), base.replace(/\/$/, "") + "/").href;
}

export async function gotoConsole(
  page: import("@playwright/test").Page,
  path: string,
) {
  return page.goto(consoleURL(path));
}

/** Fixture writes using a browser session must observe the same CSRF policy. */
export async function sessionHeaders(page: import("@playwright/test").Page) {
  const cookie = (await page.context().cookies(apiURL("/"))).find(
    (item) => item.name === "neosrv_csrf",
  );
  if (!cookie) throw new Error("The test session has no CSRF cookie");
  return { "X-CSRF-Token": decodeURIComponent(cookie.value) };
}

/** Selects token sign-in when the page offers more than one method. */
export async function chooseTokenSignIn(page: import("@playwright/test").Page) {
  const tab = page.getByRole("tab", { name: "Token" });
  if (await tab.count()) await tab.click();
}
