import { describe, expect, it } from "vitest";
import { serviceState } from "./services";

describe("serviceState", () => {
  it("distinguishes server, workspace and publication state", () => {
    expect(serviceState(false, true, 3)).toBe("off_server");
    expect(serviceState(true, false, 3)).toBe("off_workspace");
    expect(serviceState(true, true, 0)).toBe("on_no_publications");
    expect(serviceState(true, true, 2)).toBe("on");
  });
});
