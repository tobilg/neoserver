/** Browser storage can be unavailable (privacy policies, tests, embedded UIs). */
export function readPreference(key: string, fallback: string): string {
  try {
    return window.localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}

export function writePreference(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Persistence is optional; the control still works for this session.
  }
}
