import { gotoConsole } from "./fixtures";
import { expect, test, type Page } from "@playwright/test";
import {
  apiURL,
  KEYCLOAK_USERS,
  MAPPED_REALM_ROLE,
  readFixture,
  signInWithToken,
} from "./fixtures";

/**
 * End-to-end proof of the OIDC group-authorization pipeline (PRD §6.9): a real
 * authorization-code + PKCE flow against Keycloak, a real ID token carrying
 * `realm_access.roles`, and real claim-to-role resolution in the server.
 *
 * Nothing here is mocked. If the issuer, audience, PKCE exchange, claim
 * extraction or role resolution is wrong, these fail.
 */

async function signInThroughKeycloak(
  page: Page,
  user: { username: string; password: string },
) {
  await gotoConsole(page, "/login");
  await page.getByRole("button", { name: /continue with/i }).click();
  await page.waitForURL(/\/realms\/neoserver\/protocol\/openid-connect\/auth/);
  await page.getByLabel(/username|email/i).fill(user.username);
  await page
    .getByRole("textbox", { name: "Password", exact: true })
    .fill(user.password);
  await page.getByRole("button", { name: /sign in|log in/i }).click();
  // The callback page still needs to exchange the code and create the cookie.
  await page.waitForURL(
    (url) =>
      url.pathname.includes("/admin") &&
      !url.pathname.endsWith("/auth/callback"),
  );
}

async function createClaimMapping(
  workspace: string,
  bootstrapToken: string,
  role: string,
) {
  const response = await fetch(
    apiURL(`/api/v1/workspaces/${workspace}/claim-mappings`),
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${bootstrapToken}`,
      },
      body: JSON.stringify({
        claim_name: "realm_access.roles",
        claim_value: MAPPED_REALM_ROLE,
        role_id: role,
        priority: 10,
      }),
    },
  );
  if (!response.ok && response.status !== 409) {
    throw new Error(`claim mapping failed: ${await response.text()}`);
  }
}

test("the login page offers the configured identity provider", async ({
  page,
}) => {
  await gotoConsole(page, "/login");
  await expect(
    page.getByRole("button", { name: /continue with/i }),
  ).toBeVisible();
});

test("an unmapped group lands on no-access with its claims listed", async ({
  page,
}) => {
  await signInThroughKeycloak(page, KEYCLOAK_USERS.unmapped);
  await expect(page).toHaveURL(/\/no-access/);

  // The claims must be shown verbatim so an operator can build the mapping by
  // copy-paste; an empty shell here is the failure this page exists to prevent.
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: "realm_access.roles = gis-nobody" }),
  ).toBeVisible();
});

test("a mapped group grants the workspace role it maps to", async ({
  page,
}) => {
  const { workspace, bootstrapToken } = await readFixture();
  await createClaimMapping(workspace, bootstrapToken, "admin");

  await signInThroughKeycloak(page, KEYCLOAK_USERS.mapped);

  // The session is now cookie-based; the workspace resolved purely from the
  // group claim in the ID token.
  await gotoConsole(page, `/workspaces/${workspace}/layers`);
  await expect(page.getByRole("heading", { name: "Layers" })).toBeVisible();
});

test("the OIDC session authenticates workspace API calls", async ({ page }) => {
  const { workspace, bootstrapToken } = await readFixture();
  await createClaimMapping(workspace, bootstrapToken, "admin");
  await signInThroughKeycloak(page, KEYCLOAK_USERS.mapped);

  const response = await page.request.get(
    apiURL(`/api/v1/workspaces/${workspace}/services`),
  );
  expect(response.status()).toBe(200);
});

test("the identity page shows the redirect URI operators must register", async ({
  page,
}) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, "/identity");
  await expect(page.getByText("/admin/auth/callback")).toBeVisible();
});
