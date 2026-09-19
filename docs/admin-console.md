# Administration console

neoserver includes an embedded operator console at `/admin/`. It uses the same management API as automation clients and covers workspace catalog management, datasource discovery and imports, layers and coverages, styles, protocol settings, credentials, caches, operational state, and an interactive MapLibre preview.

Console sign-in requires the `admin` role for a workspace or `super_admin`; `editor` and `viewer` credentials are for OGC clients and the management API (see [Roles](authentication.md#roles)).

The console is enabled by default. Set `Server.AdminUI=false` to omit its routes, or `Server.DisableUI=true` to disable all browser UI. `Server.BasePath` is honored automatically; for example, a base path of `/geo` serves the console at `/geo/admin/`. The Vite build uses relative assets and does not require runtime variable injection.

## Sign in

The console exchanges a credential for a server-side browser session. The credential is not retained in browser storage. Available methods are:

- Token: a stored `nsk_` API key or a neoserver self-signed JWT. The configured static API key is also accepted when that authentication method is enabled.
- Password: enabled only when `Auth.Enabled=true`, `Auth.Method="basic"`, and `Auth.Users` is non-empty.
- OIDC: a public Authorization Code flow with PKCE, enabled when issuer, client ID, and `BrowserLoginEnabled` are configured.

The session cookie is `HttpOnly`, `SameSite=Lax`, and secure when HTTPS is required. Mutating requests also require the separate CSRF cookie value in `X-CSRF-Token`. Absolute and idle lifetimes come from `Auth.Session`; active-session cleanup is periodic. Revoking an API key, rotating the signing key, or changing a Basic/static credential invalidates sessions created from that credential. On the API keys page, a revoked key stays listed as "revoked" until you delete it permanently with its trash-can action. Super administrators can inspect and revoke sessions from the Identity page.

## OIDC setup

Register the exact callback shown by `GET /api/v1/console/config`, normally `https://host[/base-path]/admin/auth/callback`. The browser client must be public, require PKCE S256, allow that redirect URI, and allow browser access to its discovery and token endpoints. No client secret is sent or exposed.

~~~toml
[Auth]
Enabled = true
Method = "oidc"
RequireHTTPS = true

[Auth.OIDC]
IssuerURL = "https://identity.example.com/realms/geo"
ClientID = "neoserver-console"
BrowserLoginEnabled = true
BrowserScopes = ["openid", "profile", "email"]
GroupClaims = ["groups", "cognito:groups", "realm_access.roles", "roles"]
~~~

Common provider claim choices are `cognito:groups` for Cognito, `groups` for Clerk/Auth0/Okta and many Keycloak configurations, `realm_access.roles` for Keycloak realm roles, and `roles` for Entra application roles. Entra group-overage responses are rejected because the console does not fetch the Microsoft Graph continuation. Claim names are checked exactly before dotted traversal, so names containing a literal dot remain usable.

The session keeps the safe claims captured at login. Claim-to-role mappings are re-evaluated against those captured values on requests, but membership changes at the identity provider require a new login or session expiry. Revoke a session when access must end immediately.

## Operator workflow

New here? Follow the [UI-first tutorial](getting-started-ui.md); automation users can follow the independent [API-first tutorial](getting-started.md). Workspace overview now includes a dismissible guide from store connection or upload through publication, preview and client access. Dismissing it does not change data or permissions.

After sign-in, super administrators can choose or create a workspace. Workspace administrators land directly in an administrable workspace. A typical publication flow is:

1. Create a service and test its connection, or start a managed upload/URL import.
2. Discover source layers, review the import plan and bounded preview, then publish.
3. Configure layers, coverages, styles, and WMS/WFS/WCS/tiles settings.
4. Use Endpoints to choose a real feature layer, inspect its store/layer/protocol/access status, perform a read-only session check, and copy curl, server-side JavaScript, Python or QGIS connection examples. MapLibre guidance explicitly requires an authenticated application backend for private data. Preview checks map rendering separately and offers per-layer Fit controls.

Tables switch between a card layout and a detailed layout based on the width available to the table (not the screen): below about 670px of table width rows become cards. The actions column stays pinned to the right while the rest of a wide table scrolls. Layers and Coverages show **Preview** plus an actions menu (Edit, Preview, Connect a client for feature layers, Unpublish); Stores show their discovery action plus a menu with Test connection, Add SQL view, mosaic management and Delete. On cards, a single **⋯** button lists every action of the row. Title and Generation columns are hidden by default (the title is shown under the public ID); use **Columns** to show them. Below 1100px the sidebar starts collapsed unless you have chosen otherwise, and **⌘K / Ctrl+K** (or the search button on phones) opens a quick jump to pages, workspaces and publications; text typed while it opens goes into its search field. Publication editing opens with everyday fields; expand **Advanced publication settings** for additional styles, spatial/raster configuration and cache policy. Collapsed fields retain their values, and save errors reveal advanced settings.

Preview pins the publications currently on the map under **On the map**, with their source, opacity, style and legend controls and a **Fit all** button; the searchable catalog below lists everything else, grouped by store or by type. A layer group shows the legends of its member layers. Without a configured basemap (`Website.BasemapUrl`), a short hint explains how to add one. Preview automatically frames the initial selection when geographic extents are available. Explicit `bbox` view links take precedence; refresh, opacity/style edits, and late metadata do not reset a view you have moved. Use **Fit** to deliberately reframe. An unavailable extent is explained beside the disabled Fit button.
5. Create a scoped API key for the consuming application and save its one-time secret.

The connection page never displays a key ID as a secret and never embeds credentials in examples. A successful session check proves only that the current console session can fetch a small sample through the local origin, not that the configured public URL, a different user's key or map rendering works. Inactive endpoints remain grey with disabled Copy actions and a link to the relevant service settings. Failed reads expose Retry and recovery guidance instead of empty-success states.

Server administration exposes Operations (readiness, catalog integrity, global response cache and a summary of deletions that need attention), Deletions (all workspaces, including deleted ones), Audit, Roles & policies, Identity and custom tile matrix sets according to the signed-in principal's capabilities. In a workspace, **Operate › Caching** combines the durable tile cache, seeding jobs and the workspace's in-memory response cache (the former Tile cache and Cache pages; their URLs redirect).

SQL-view validation has a 10-second server deadline. While it runs, **Cancel validation** aborts the request and preserves the SQL and layer-ID draft. Validate again before publishing. Managed imported DuckDB stores support this workflow even with their one-connection pool.

After SQL validation, select a **Feature ID column**: a stable, unique, non-null value for each row. Publication checks this constraint and keeps the draft available if validation fails.

**Add layer** locks the store and dialog controls during discovery and publication. A batch uses the store and names captured when it starts; failed rows remain selected for retry without republishing successful rows. Navigation warns while a batch or edited draft is active. Leaving the page or ending the session stops queued publications, but a request already sent may still finish.

Discovery lists new sources first; already-published sources are behind **Show N published**. When every source is published, the dialog says so and links to Layers and Preview. **Refresh discovery** keeps existing draft IDs, titles, and selections, including after a failed refresh. Discovery starts with nothing selected. Missing sources are deselected with a notice; their names are retained if they reappear during that dialog session.

The role-policy editor selects workspaces by name and submits their IDs. Operation and action choices come from the server's OpenAPI contract; stored-query administration uses `manage`, while transactions and locks use `write`. Legacy unmatched workspace scopes are marked for removal and replacement.

The Audit page is an event log: each line shows the time (with seconds), the status (failures in red), who did what, and the request. Consecutive identical requests collapse into one line with a count ("×24"). Outcome (All / Failed / Succeeded), time range and search filters, plus exact principal, workspace and API-key filters under **More filters**, run against all retained history, not just the displayed page (`outcome` and `since` are API query parameters). "Older events" follows the server cursor; "Newer events" returns to the beginning. Filters and the current cursor remain shareable in the URL, and browser Back returns to the preceding page. Historical events may have no credential ID.

Resource selectors include enabled published feature layers, coverages (including PostGIS raster), and groups. Coverages/groups use WMS in Preview; GeoJSON and vector-tile controls are disabled for them. Enable WMS in workspace Service settings and in server configuration first. Vector seed jobs list only feature resources, while map seed jobs also offer coverages/groups.

The style editor reads and saves the canonical `body`. **New style** offers SLD 1.0, SLD/SE 1.1, CSS, YSLD and Mapbox formats with point, line, polygon and raster starters. Creation failures retain the chosen name/format/starter. All use the same server compiler used for binding validation. CSS/YSLD/Mapbox rendering requires `dynamic-style` in both server `WMS.Extensions` and workspace WMS settings; creation alone does not enable the gate. Unsaved draft portrayal is limited to SLD/SE; save an alternate-format style and use its named WMS preview. Bind the saved style through a publication's **Default style** field.

Below 1024 pixels, **Editor** and **Preview** tabs replace the stacked style panels. Arrow keys move between tabs; Apply preview selects the Preview tab. Unsaved source stays mounted across tab, theme and viewport changes. Wider screens show both panels side by side.

For live local verification, run `make ui-e2e` with Docker running. It starts disposable PostGIS/Keycloak/server services and tests the embedded production assets. Chromium maps the fixture's Keycloak hostname to loopback without editing `/etc/hosts`; issuer verification stays enabled. `CONSOLE_PROJECT_NAME` selects a separate test project, whose volumes are removed after the run unless `KEEP_CONSOLE_ENVIRONMENT=true`.

The fixture rebuilds by default. For test-only reruns, `CONSOLE_SKIP_BUILD=true`
reuses the project's local images without refreshing base-image metadata. Use
it only after those images have been built from the current backend/UI code.

### Following a deletion

Deleting a store opens **Deletions** in its workspace with a bookmarkable operation detail (`/admin/workspaces/{workspace}/deletions?operation={id}`). HTTP 202 means accepted, not completed. The detail follows pending/running cleanup automatically, displays failures, and offers **Retry deletion**. Confirmation controls remain locked while acceptance is pending. History remains available after the store leaves the catalog; workspace administrators do not need access to the server Deletions page. Workspace deletions hand off to **Server administration › Deletions** (`/admin/deletions?operation={id}`) because the workspace itself will disappear.

History filters and pagination run on the server, after authorization. Select **Failed** or **Needs attention** to find older unfinished work. **Next page** fetches older records, **First page** returns to the latest, and browser Back restores previous page URLs. Polling pauses when the tab is hidden and stops when work finishes. Reloading an operation link resumes tracking.

API clients can call `GET /api/v1/deletions?workspace={workspace-uuid}&status=failed&limit=50`. The optional workspace filter uses the UUID, including for a deleted workspace. Omit it to list all workspaces the caller administers. Follow `next_cursor` with the same filters; restart pagination if permissions or filters change. Limits are 1–1000 (default 200); `status` accepts `pending`, `running`, `failed`, `completed`, or `actionable`. Invalid filters/cursors return 400 and inaccessible workspace scopes return 403.

## Security and deployment

Use HTTPS, configure `Server.UrlBase` to the externally visible origin, and restrict `Server.TrustedProxyCIDRs`. OIDC issuer and configured basemap origins are added narrowly to the console Content Security Policy. The console never exposes datasource passwords or server secrets through its bootstrap configuration endpoint.

Related: [Authentication](authentication.md) · [Management API](management-api.md) · [Configuration](configuration.md)
