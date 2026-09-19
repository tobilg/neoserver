/**
 * Turns a store connection or open failure into a first-line explanation.
 * The raw detail stays available separately for diagnosis.
 */
export function connectionMessage(
  detail: string,
  allowedPaths: string[] = [],
): string {
  if (
    /not permitted by the datasource allowlist|escapes the datasource allowlist/i.test(
      detail,
    )
  )
    return allowedPaths.length
      ? `This path is outside the folders the server may read. Use a path under ${allowedPaths.join(", ")}.`
      : "This path is outside the folders the server may read, and no allowed folders are configured.";
  if (/path traversal/i.test(detail))
    return "Paths may not contain “..”. Use a path inside an allowed folder.";
  if (/database .*does not exist/i.test(detail))
    return "Database not found. Check the database name and create it if needed.";
  if (/password authentication|authentication failed/i.test(detail))
    return "Authentication failed. Check the database user and password.";
  if (
    /no such file|file not found|does not exist|cannot open|not recognized as being in a supported file format/i.test(
      detail,
    )
  )
    return "The file or folder could not be opened. Check that the path exists on the server and is a supported format.";
  if (/connection refused|dial tcp|timeout|no such host/i.test(detail))
    return "Could not reach the database. Check the host, port, network, and whether the database is running.";
  if (/ssl|tls|certificate/i.test(detail))
    return "Secure connection failed. Check the SSL mode and database certificate settings.";
  if (/no .*(granules|files) match|empty mosaic|no granules/i.test(detail))
    return "No raster files matched. Check the directory and file pattern.";
  return "Connection failed. Check the connection settings and the diagnostic details below.";
}
