/** Extracts the first "line N" reference from a server parse error. */
export function errorLine(detail?: string): number | undefined {
  const match = /\bline (\d+)\b/.exec(detail ?? "");
  return match ? Number(match[1]) : undefined;
}
