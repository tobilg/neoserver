/** datetime-local is local wall time; Go's time.Time expects RFC 3339. */
export function expiryISO(value: string, now = Date.now()) {
  if (!value) return undefined;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime()))
    throw new Error("Enter a valid expiration date and time.");
  if (date.getTime() <= now)
    throw new Error("Expiration must be in the future.");
  return date.toISOString();
}

export function apiKeyState(
  key: { revoked?: boolean; expires_at?: string | null },
  now = Date.now(),
) {
  if (key.revoked) return "revoked";
  return key.expires_at && Date.parse(key.expires_at) <= now
    ? "expired"
    : "active";
}
