/** Mirrors the server rule: names are public URL path segments. */
export const WORKSPACE_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

export function workspaceNameProblem(name: string): string | undefined {
  if (!name) return undefined;
  if (WORKSPACE_NAME_PATTERN.test(name)) return undefined;
  if (name.length > 64) return "Use at most 64 characters.";
  if (!/^[A-Za-z0-9]/.test(name)) return "Start with a letter or digit.";
  return "Use only letters, digits, “.”, “_” or “-” (no spaces or slashes).";
}

/** Suggests a lowercase, hyphenated name for free text such as "My Demo". */
export function suggestWorkspaceName(name: string): string {
  return name
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[^a-z0-9]+/, "")
    .replace(/-+$/, "")
    .slice(0, 64);
}
