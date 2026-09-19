const publicIDPattern = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

export const isValidPublicID = (value: string) => publicIDPattern.test(value);

/** Suggest a public layer ID from the first table the query reads. */
export function suggestPublicID(sql: string) {
  const match =
    /\bfrom\s+((?:"[^"]+"|[\w$]+)(?:\s*\.\s*(?:"[^"]+"|[\w$]+))*)(?![\w$"]|\s*[.(])/i.exec(
      sql,
    );
  if (!match) return "";
  const table = match[1].split(".").at(-1)!.trim().replaceAll('"', "");
  const id = table
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, "_")
    .replace(/^[^a-z_]+/, "");
  return isValidPublicID(id) ? id : "";
}

/** Explain the first missing step that keeps publication disabled. */
export function publishBlocker({
  validated,
  idColumn,
  publicID,
}: {
  validated: boolean;
  idColumn: string;
  publicID: string;
}) {
  if (!validated) return "Validate the SQL to publish it.";
  if (!idColumn) return "Choose a feature ID column to publish.";
  if (!publicID) return "Add a public layer ID to publish.";
  if (!isValidPublicID(publicID)) return "Fix the public layer ID to publish.";
  return "";
}
