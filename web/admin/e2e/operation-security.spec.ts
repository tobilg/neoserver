import { expect, test } from "@playwright/test";
import { apiURL, readFixture, signInWithToken, gotoConsole } from "./fixtures";

test("canonical WFS grants, GET CSRF and durable credential attribution", async ({
  request,
  page,
}) => {
  test.setTimeout(60000);
  const { bootstrapToken } = await readFixture();
  const admin = { Authorization: `Bearer ${bootstrapToken}` };
  const name = `operation-${Date.now()}`;
  const workspace = await request.post(apiURL("/api/v1/workspaces"), {
    headers: admin,
    data: { name },
  });
  expect(workspace.status()).toBe(201);
  const ws = await workspace.json();
  const api = `/api/v1/workspaces/${name}`;
  const wfs = apiURL(`/workspaces/${name}/wfs`);
  const settings = await (
    await request.get(apiURL(`${api}/settings/wfs`), { headers: admin })
  ).json();
  expect(
    (
      await request.put(apiURL(`${api}/settings/wfs`), {
        headers: admin,
        data: { ...settings, enabled: true, public: false },
      })
    ).ok(),
  ).toBe(true);
  const store = await request.post(apiURL(`${api}/services`), {
    headers: admin,
    data: {
      name: "source",
      type: "postgis",
      enabled: true,
      connection_info: {
        host: "db",
        port: 5432,
        database: "postgis",
        user: "postgres",
        password: "postgres",
        sslmode: "disable",
        schemas: ["cite"],
      },
    },
  });
  expect(store.status(), await store.text()).toBe(201);
  expect(
    (
      await request.post(apiURL(`${api}/services/source/layers`), {
        headers: admin,
        data: {
          source_layer: "cite.BasicPolygons",
          public_id: "areas",
          enabled: true,
        },
      })
    ).status(),
  ).toBe(201);
  expect(
    (
      await request.post(apiURL("/api/v1/roles"), {
        headers: admin,
        data: { id: name, name },
      })
    ).status(),
  ).toBe(201);
  async function grant(operation: string, action: string) {
    const response = await request.post(
      apiURL(`/api/v1/roles/${name}/policies`),
      {
        headers: admin,
        data: { workspace: name, service: "wfs", operation, action },
      },
    );
    expect(response.status(), await response.text()).toBe(201);
  }
  await grant("GetCapabilities", "read");
  const policies = await (
    await request.get(apiURL(`/api/v1/roles/${name}/policies`), {
      headers: admin,
    })
  ).json();
  expect(policies.policies[0][1]).toBe(ws.id);
  const key = await (
    await request.post(apiURL(`${api}/apikeys`), {
      headers: admin,
      data: { name: "restricted", role_id: name },
    })
  ).json();
  const keyHeaders = {
    "X-API-Key": key.key,
    "Content-Type": "application/xml",
  };
  const xml = (operation: string, content = "") =>
    `<wfs:${operation} xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0" service="WFS" version="2.0.0">${content}</wfs:${operation}>`;
  const features = xml("GetFeature", '<wfs:Query typeNames="areas"/>');
  for (const query of [
    "",
    "?request=GetCapabilities",
    "?ReQuEsT=GetCapabilities",
  ]) {
    expect(
      (
        await request.post(wfs + query, { headers: keyHeaders, data: features })
      ).status(),
    ).toBe(403);
  }
  expect(
    (
      await request.post(`${wfs}?request=GetCapabilities&REQUEST=GetFeature`, {
        headers: keyHeaders,
        data: features,
      })
    ).status(),
  ).toBe(400);
  expect(
    (
      await request.post(wfs, {
        headers: keyHeaders,
        data: xml("GetCapabilities"),
      })
    ).status(),
  ).toBe(200);
  await grant("GetFeature", "read");
  const allowed = await request.post(`${wfs}?request=GetCapabilities`, {
    headers: keyHeaders,
    data: features,
  });
  expect(allowed.status(), await allowed.text()).toBe(200);
  expect(await allowed.text()).toContain("FeatureCollection");
  await grant("Transaction", "write");
  const transaction = await request.post(`${wfs}?request=GetCapabilities`, {
    headers: keyHeaders,
    data: xml(
      "Transaction",
      '<wfs:Update typeName="areas"><wfs:Property><wfs:ValueReference>description</wfs:ValueReference><wfs:Value>unchanged</wfs:Value></wfs:Property><fes:Filter><fes:ResourceId rid="areas.999999"/></fes:Filter></wfs:Update>',
    ),
  });
  expect(transaction.status(), await transaction.text()).toBe(200);
  expect(await transaction.text()).toContain("TransactionResponse");
  const storedQuery = xml(
    "CreateStoredQuery",
    '<wfs:StoredQueryDefinition id="csrf-probe"><wfs:Title>CSRF probe</wfs:Title><wfs:QueryExpressionText returnFeatureTypes="areas" language="urn:ogc:def:queryLanguage:OGC-WFS::WFS_QueryExpression"><wfs:Query typeNames="areas"/></wfs:QueryExpressionText></wfs:StoredQueryDefinition>',
  );
  expect(
    (
      await request.post(wfs, { headers: keyHeaders, data: storedQuery })
    ).status(),
  ).toBe(403);
  await grant("CreateStoredQuery", "manage");
  const created = await request.post(wfs, {
    headers: keyHeaders,
    data: storedQuery,
  });
  expect(created.status(), await created.text()).toBe(200);
  expect(await created.text()).toContain("CreateStoredQueryResponse");

  const browserKey = await (
    await request.post(apiURL(`${api}/apikeys`), {
      headers: admin,
      data: { name: "browser-no-owner", role_id: "admin" },
    })
  ).json();
  await signInWithToken(page, browserKey.key, `/workspaces/${name}`);
  const dropURL = `${wfs}?service=WFS&version=2.0.0&request=DropStoredQuery&id=csrf-probe`;
  await page.route("http://attacker.test/", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: `<a href="${dropURL.replaceAll("&", "&amp;")}">Continue</a>`,
    }),
  );
  await page.goto("http://attacker.test/");
  const navigation = page.waitForResponse(
    (response) => response.url() === dropURL,
  );
  await page.getByRole("link", { name: "Continue" }).click();
  expect((await navigation).status()).toBe(403);
  const listed = await request.get(
    `${wfs}?service=WFS&request=ListStoredQueries`,
    { headers: admin },
  );
  expect(await listed.text()).toContain("csrf-probe");
  const csrf = (await page.context().cookies()).find(
    (cookie) => cookie.name === "neosrv_csrf",
  )!.value;
  const dropped = await page.request.get(dropURL, {
    headers: { "X-CSRF-Token": csrf },
  });
  expect(dropped.status(), await dropped.text()).toBe(200);
  expect(await dropped.text()).toContain("DropStoredQueryResponse");
  const remaining = await request.get(
    `${wfs}?service=WFS&request=ListStoredQueries`,
    { headers: admin },
  );
  expect(await remaining.text()).not.toContain("csrf-probe");
  for (const [credential, operation] of [
    [key.id, "TRANSACTION"],
    [browserKey.id, "DROPSTOREDQUERY"],
  ]) {
    const audit = await request.get(
      apiURL(`/api/v1/audit?credential_id=${credential}`),
      { headers: admin },
    );
    expect(audit.status(), await audit.text()).toBe(200);
    const { events } = await audit.json();
    expect(events).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          credential_id: credential,
          operation,
          action: "change",
          status: 200,
        }),
      ]),
    );
    expect(
      events.every(
        (event: { credential_id: string }) =>
          event.credential_id === credential,
      ),
    ).toBe(true);
    expect(
      events.some(
        (event: { operation: string; status: number }) =>
          event.operation === "GETFEATURE" && event.status === 200,
      ),
    ).toBe(false);
  }
});

test("passive dashboard polling and a second tab do not extend idle expiry; explicit refresh does", async ({
  page,
  context,
}) => {
  test.setTimeout(90000);
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  const readMe = async () =>
    (await page.request.get(apiURL("/api/v1/auth/me"))).json();
  const before = await readMe();
  const second = await context.newPage();
  await gotoConsole(second, "/");
  // Cross the old one-minute auto-touch threshold while the real dashboard polls.
  await page.waitForTimeout(65000);
  const passive = await readMe();
  expect(passive.session_idle_expires_at).toBe(before.session_idle_expires_at);
  expect(passive.session_expires_at).toBe(before.session_expires_at);
  const csrf = (await context.cookies()).find(
    (cookie) => cookie.name === "neosrv_csrf",
  )!.value;
  const refreshed = await page.request.post(apiURL("/api/v1/auth/refresh"), {
    headers: { "X-CSRF-Token": csrf },
  });
  expect(refreshed.status(), await refreshed.text()).toBe(200);
  const active = await refreshed.json();
  expect(Date.parse(active.session_idle_expires_at)).toBeGreaterThan(
    Date.parse(before.session_idle_expires_at),
  );
  expect(active.session_expires_at).toBe(before.session_expires_at);
  expect((await second.request.get(apiURL("/api/v1/auth/me"))).status()).toBe(
    200,
  );
});
