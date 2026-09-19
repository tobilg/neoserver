import { describe, expect, it } from "vitest";
import { apiKeyState, expiryISO } from "./expiry";
describe("API key expiration", () => {
  it("derives expired state at the boundary and gives revocation precedence", () => {
    const now = Date.parse("2030-01-01T00:00:00Z");
    expect(apiKeyState({}, now)).toBe("active");
    expect(
      apiKeyState({ expires_at: new Date(now + 1).toISOString() }, now),
    ).toBe("active");
    expect(apiKeyState({ expires_at: new Date(now).toISOString() }, now)).toBe(
      "expired",
    );
    expect(
      apiKeyState({ expires_at: new Date(now - 1).toISOString() }, now),
    ).toBe("expired");
    expect(
      apiKeyState(
        { revoked: true, expires_at: new Date(now - 1).toISOString() },
        now,
      ),
    ).toBe("revoked");
  });
  it("serializes local time as an RFC3339 timestamp", () => {
    expect(expiryISO("2030-12-01T12:30", 0)).toBe(
      new Date(2030, 11, 1, 12, 30).toISOString(),
    );
  });
  it("permits no expiration and rejects invalid or past dates", () => {
    expect(expiryISO("")).toBeUndefined();
    expect(() => expiryISO("invalid")).toThrow("valid");
    expect(() => expiryISO("2000-01-01T00:00")).toThrow("future");
  });
});
