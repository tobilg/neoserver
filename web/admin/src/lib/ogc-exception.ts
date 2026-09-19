/**
 * Reduce an OGC exception report (WMS ServiceExceptionReport or OWS
 * ExceptionReport) to its messages. Other bodies are returned unchanged.
 */
export function ogcExceptionMessage(body: string) {
  if (!/ExceptionReport/.test(body)) return body;
  const messages = [
    ...body.matchAll(
      /<(?:\w+:)?(?:ServiceException|ExceptionText)\b[^>]*>([\s\S]*?)<\/(?:\w+:)?(?:ServiceException|ExceptionText)>/g,
    ),
  ]
    .map((match) => decodeEntities(match[1].trim()))
    .filter(Boolean);
  return messages.length ? messages.join("\n") : body;
}

const entities: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
};

function decodeEntities(text: string) {
  return text
    .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, "$1")
    .replace(/&(amp|lt|gt|quot|apos);/g, (_, name: string) => entities[name]);
}
