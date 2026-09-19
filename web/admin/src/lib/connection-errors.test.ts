import { describe, expect, it } from "vitest";
import { connectionMessage } from "./connection-errors";

describe("connectionMessage", () => {
  it("names the allowed folders for paths outside the allowlist", () => {
    expect(
      connectionMessage(
        'path "/Users/me/data/x.tif" is not permitted by the datasource allowlist',
        ["./data/**"],
      ),
    ).toBe(
      "This path is outside the folders the server may read. Use a path under ./data/**.",
    );
  });

  it("explains open failures from the server's create response", () => {
    expect(
      connectionMessage(
        'the data source could not be opened: resolve mosaic granule: path ".cache/x.tif" is not permitted by the datasource allowlist',
        [],
      ),
    ).toMatch(/no allowed folders are configured/);
    expect(
      connectionMessage(
        "open raster: /data/missing.tif: No such file or directory",
      ),
    ).toMatch(/could not be opened/);
  });

  it("keeps the existing database explanations", () => {
    expect(
      connectionMessage(
        'FATAL: password authentication failed for user "postgres"',
      ),
    ).toMatch(/^Authentication failed/);
    expect(
      connectionMessage("dial tcp 127.0.0.1:5432: connect: connection refused"),
    ).toMatch(/^Could not reach the database/);
    expect(connectionMessage("something odd")).toMatch(/^Connection failed/);
  });
});
