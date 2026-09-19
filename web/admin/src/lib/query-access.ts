export function isAccessError(error: unknown) {
  return Boolean(
    error &&
    typeof error === "object" &&
    "status" in error &&
    [401, 403].includes(Number(error.status)),
  );
}
