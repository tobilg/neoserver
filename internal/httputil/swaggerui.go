package httputil

import "html"

// SwaggerUIPage returns a Swagger UI page whose every asset is same-origin.
//
// The server sends a restrictive Content-Security-Policy (`default-src 'self';
// script-src 'self'`), so a CDN-hosted bundle or an inline initialiser is
// blocked and the page renders blank. Swagger UI's runtime is copied into the
// embedded console build by web/admin/scripts/copy-swagger-assets.mjs and
// served below basePath+"/admin/vendor/swagger"; initScript is the URL of a
// same-origin script serving SwaggerUIInitJS. The initialiser loads the
// OpenAPI document from "./api", relative to the page.
func SwaggerUIPage(title, basePath, initScript string) string {
	vendor := html.EscapeString(basePath + "/admin/vendor/swagger")
	return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>` + html.EscapeString(title) + `</title>
    <link rel="stylesheet" href="` + vendor + `/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="` + vendor + `/swagger-ui-bundle.js"></script>
    <script src="` + html.EscapeString(initScript) + `"></script>
  </body>
</html>`
}

// SwaggerUIInitJS boots Swagger UI, or explains itself when its runtime is missing.
//
// The runtime ships with the embedded console, so it is absent from a binary
// built without Node and unreachable when the console is disabled. Both cases
// would otherwise leave an empty page with only a console error.
const SwaggerUIInitJS = `(function () {
  var mount = document.getElementById("swagger-ui");
  if (typeof SwaggerUIBundle === "undefined") {
    mount.innerHTML =
      '<div style="font:14px system-ui;max-width:44rem;margin:3rem auto;padding:0 1rem">' +
      "<h1>API documentation is unavailable</h1>" +
      "<p>The Swagger UI runtime ships with the administration console. It is " +
      "missing when the server was built without the console, or when the " +
      "console is disabled with <code>Server.AdminUI=false</code>.</p>" +
      '<p>Build it with <code>make ui-build</code>, or use the OpenAPI document ' +
      'directly at <a href="./api">./api</a>.</p></div>';
    return;
  }
  window.ui = SwaggerUIBundle({ url: "./api", dom_id: "#swagger-ui" });
})();
`
