import { describe, expect, it } from "vitest";
import {
  connectionExamples,
  featureURL,
  shellQuote,
} from "./connection-examples";

describe("client examples", () => {
  it("encodes resource paths without losing a configured base path", () => {
    expect(
      featureURL(
        "https://maps.test/geo/workspaces/Test%20Space",
        "roads/2026 & rail",
      ),
    ).toBe(
      "https://maps.test/geo/workspaces/Test%20Space/ogc/collections/roads%2F2026%20%26%20rail/items?limit=10",
    );
  });
  it("escapes shell quotes, newlines and command substitution as literal data", () => {
    expect(shellQuote("a'b$(id)\nc")).toBe("'a'\\''b$(id)\nc'");
  });
  it("generates credential-free source for real layer URLs", () => {
    const root = "http://localhost:9000/geo/workspaces/demo";
    const url = featureURL(root, "places");
    const examples = connectionExamples(url, root, "places");
    expect(examples.curl).toContain("$NEOSRV_API_KEY");
    expect(examples.javascript).toContain("process.env.NEOSRV_API_KEY");
    expect(examples.python).toContain('os.environ["NEOSRV_API_KEY"]');
    expect(examples.qgis).toContain(`${root}/ogc/`);
    expect(examples.maplibre).toContain('fetch("/api/map-data"');
    for (const example of Object.values(examples))
      expect(example).not.toContain("nsk_");
    for (const example of [examples.curl, examples.javascript, examples.python])
      expect(example).toContain(url);
  });
});
