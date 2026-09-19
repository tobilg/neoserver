// Values originate in the catalog, not in source code. Quote separately for
// each target language; URL-encoding alone does not escape shell apostrophes.
export function shellQuote(value: string) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

export function featureURL(root: string, publicID: string) {
  return `${root}/ogc/collections/${encodeURIComponent(publicID)}/items?limit=10`;
}

export function connectionExamples(
  url: string,
  root: string,
  publicID: string,
) {
  return {
    curl: [
      "# Set NEOSRV_API_KEY to a workspace viewer key, not your admin token.",
      "# Bash: read it without putting the secret in shell history.",
      "# Run these lines in bash (or save as features.sh and run bash features.sh).",
      "printf 'Paste viewer API key, then press Enter: ' >&2",
      "read -r -s NEOSRV_API_KEY",
      "export NEOSRV_API_KEY",
      "printf '\\n'",
      'curl --fail-with-body --silent --show-error -H "X-API-Key: $NEOSRV_API_KEY" ' +
        shellQuote(url),
    ].join("\n"),
    javascript: `// Save as features.mjs; run: node features.mjs (Node 22+).\n// Set NEOSRV_API_KEY in the process environment first. Server-side only.\nconst key = process.env.NEOSRV_API_KEY;\nif (!key) throw new Error("Set NEOSRV_API_KEY to a workspace viewer key");\nconst response = await fetch(${JSON.stringify(url)}, {\n  headers: { "X-API-Key": key },\n});\nif (!response.ok) throw new Error(\`HTTP \${response.status}: \${await response.text()}\`);\nconsole.log(JSON.stringify(await response.json(), null, 2));`,
    python: `# Save as features.py; run: python3 features.py. No packages required.\n# Set NEOSRV_API_KEY in the process environment first.\nimport json\nimport os\nimport urllib.request\n\nrequest = urllib.request.Request(\n    ${JSON.stringify(url)},\n    headers={"X-API-Key": os.environ["NEOSRV_API_KEY"]},\n)\nwith urllib.request.urlopen(request, timeout=30) as response:\n    print(json.dumps(json.load(response), indent=2))`,
    qgis: `QGIS → Data Source Manager → WFS / OGC API - Features → New\nURL: ${root}/ogc/\nAuthentication: store the viewer key in QGIS authentication storage;\nconfigure an HTTP header named X-API-Key with the full key value.\nConnect, select ${publicID}, then Add.\n\nDo not append keys to URLs. This is a read/query connection.\nWFS-T editing from QGIS is not a supported client workflow.`,
    maplibre: `// Integration snippet for an existing MapLibre map (maplibre-gl).\n// Run after the map's load event. Private data MUST come through your\n// authenticated application backend. Do not bundle workspace API keys.\n// Implement /api/map-data on that backend, fetching:\n// ${url}\n// with its scoped viewer key. Enforce your application's user permissions.\nconst response = await fetch("/api/map-data", { credentials: "same-origin" });\nif (!response.ok) throw new Error(\`HTTP \${response.status}\`);\nconst data = await response.json();\nmap.addSource("neoserver", { type: "geojson", data });\nmap.addLayer({ id: "neoserver-polygons", type: "fill", source: "neoserver",\n  filter: ["==", ["geometry-type"], "Polygon"],\n  paint: { "fill-color": "#2563eb", "fill-opacity": 0.4 } });\nmap.addLayer({ id: "neoserver-lines", type: "line", source: "neoserver",\n  filter: ["!=", ["geometry-type"], "Point"],\n  paint: { "line-color": "#2563eb", "line-width": 2 } });\nmap.addLayer({ id: "neoserver-points", type: "circle", source: "neoserver",\n  filter: ["==", ["geometry-type"], "Point"],\n  paint: { "circle-color": "#2563eb", "circle-radius": 5 } });\n// Fit the map to your dataset's extent. The sample is limited to 10 features.`,
  };
}
