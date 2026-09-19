import { writeFile } from "node:fs/promises";
import { apiURL, FIXTURE_PATH, type ProvisionedFixture } from "./fixtures";

/**
 * Provisions the catalog the specs run against, using the bootstrap token the
 * fixture's `store-init` produced. Everything here goes through the documented
 * management API, so the RBAC states the specs exercise are real rather than
 * simulated.
 */
export default async function globalSetup() {
  const bootstrapToken = process.env.CONSOLE_BOOTSTRAP_TOKEN;
  if (!bootstrapToken) {
    throw new Error(
      "CONSOLE_BOOTSTRAP_TOKEN is not set. Run the suite through `make ui-e2e`.",
    );
  }

  const workspace = "e2e";

  async function call<T>(
    path: string,
    init: RequestInit & { allowConflict?: boolean } = {},
  ): Promise<T> {
    const { allowConflict, ...request } = init;
    const response = await fetch(apiURL(path), {
      ...request,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${bootstrapToken}`,
        ...(request.headers ?? {}),
      },
    });
    if (response.status === 409 && allowConflict) {
      return undefined as T;
    }
    if (!response.ok) {
      throw new Error(
        `${request.method ?? "GET"} ${path} failed: ${response.status} ${await response.text()}`,
      );
    }
    return (await response.json().catch(() => undefined)) as T;
  }

  await call("/api/v1/workspaces", {
    method: "POST",
    allowConflict: true,
    body: JSON.stringify({
      name: workspace,
      description: "Disposable workspace for console end-to-end tests",
    }),
  });

  async function createKey(name: string, role: string): Promise<string> {
    const created = await call<{ key: string }>(
      `/api/v1/workspaces/${workspace}/apikeys`,
      {
        method: "POST",
        body: JSON.stringify({
          name,
          owner_name: name,
          role_id: role,
        }),
      },
    );
    if (!created?.key) {
      throw new Error(`API key '${name}' was created without a secret`);
    }
    return created.key;
  }

  const fixture: ProvisionedFixture = {
    bootstrapToken,
    workspace,
    workspaceAdminKey: await createKey("e2e-workspace-admin", "admin"),
    viewerKey: await createKey("e2e-viewer", "viewer"),
  };

  await writeFile(FIXTURE_PATH, JSON.stringify(fixture, null, 2));
}
