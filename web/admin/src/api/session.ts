// Responses from an earlier login must never update the current session's UI.
let generation = 0;
export const sessionGeneration = () => generation;
export function invalidateSessionRequests() {
  return ++generation;
}
export function assertCurrentSession(expected: number) {
  if (expected !== generation) {
    throw new DOMException(
      "The session changed while this request was running.",
      "AbortError",
    );
  }
}
