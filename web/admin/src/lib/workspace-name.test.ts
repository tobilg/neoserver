import { describe, expect, it } from "vitest";
import { suggestWorkspaceName, workspaceNameProblem } from "./workspace-name";

describe("workspace names", () => {
  it("accepts URL-safe names", () => {
    for (const name of ["demo", "Demo_2026", "a.b-c"])
      expect(workspaceNameProblem(name)).toBeUndefined();
  });

  it("explains invalid names", () => {
    expect(workspaceNameProblem("My Demo")).toMatch(/no spaces/);
    expect(workspaceNameProblem("-demo")).toMatch(/Start with/);
    expect(workspaceNameProblem("x".repeat(65))).toMatch(/64/);
  });

  it("suggests a slug", () => {
    expect(suggestWorkspaceName("My Demo")).toBe("my-demo");
    expect(suggestWorkspaceName("  Café / Straße ")).toBe("cafe-stra-e");
    expect(suggestWorkspaceName("--x--")).toBe("x");
  });
});
