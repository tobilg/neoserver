# Authentication and authorization

neoserver separates identity validation from authorization. Credentials identify a principal; global/workspace roles and layer rules determine what it may access.

Browser sign-out completes only after server-side session revocation succeeds. A session-store write failure returns HTTP 503 and leaves the browser cookie available for retry; the console reports **Sign-out failed** and stays on the current page. Retry **End session** when storage is available. Refresh also returns 503 for persistence failures; HTTP 409 is reserved for an actual concurrent rotation, which the browser recovers from by loading its current identity.

## Credential types

Requests may use:

- Stored API keys in X-API-Key
- Self-signed JWTs in Authorization: Bearer
- OIDC bearer tokens
- A server-configured static API key
- Server-configured HTTP Basic credentials
- An encrypted browser session created by the administration console

Health and generated API documentation are public. Management handlers and non-public workspace services enforce their own access requirements.

An invalid, expired, or unverifiable credential returns 401 even when the requested service is public. Omit credentials entirely for anonymous access.

## Bootstrap and recovery tokens

The init command generates an ECDSA signing key inside the encrypted store and prints a super_admin JWT valid for 24 hours.

Create a replacement:

~~~bash
export NEOSRV_STORE_KEY="the-original-store-key"
neoserver create-token --store-path ./data/neoserver.db \
  --role super_admin --subject recovery --expires 24h
~~~

Rotate the signing key only when all existing self-signed tokens should be invalidated:

~~~bash
neoserver rotate-signing-key --store-path ./data/neoserver.db
~~~

Stored API keys and OIDC tokens are not signed by this internal key.

## Browser sessions

The administration console at `/admin/` exchanges a password, API key, self-signed JWT, or OIDC ID token through `POST /api/v1/auth/login`. The original credential is not stored in the browser. The server persists only session and CSRF hashes in the encrypted catalog and returns an `HttpOnly`, `SameSite=Lax` session cookie plus a readable CSRF cookie. Unsafe HTTP methods and protocol mutations must echo the latter as `X-CSRF-Token`. This includes GET requests for WFS transactions, locks, and stored-query changes; a cookie alone never authorizes those mutations. Explicit header credentials do not require CSRF tokens.

`Auth.Session.TTLSec` bounds absolute lifetime, `IdleTimeoutSec` bounds inactivity, and `CleanupIntervalSec` controls expired-session cleanup. The current session is rotated with `/auth/refresh` and revoked with `/auth/logout`. Super administrators can list and revoke sessions under `/auth/sessions`.

Ordinary authenticated requests do not extend idle expiry. Only a CSRF-protected `POST /api/v1/auth/refresh` does, capped at the original absolute expiry. The console refreshes near the idle deadline only after recent pointer/keyboard activity in a visible tab. Dashboard polling, mounting or reloading a tab does not count as activity. An expired session cannot be refreshed; sign in again.

Refresh rotation is conditional on the authenticated token generation. The preceding session/CSRF hashes remain usable for at most 30 seconds to cover requests already in flight; accepting them never extends that window. Reusing the preceding token to refresh returns **409 Conflict**, not a successful response with an invalid cookie. Refreshing the current token inside that window is coalesced without another rotation. Revocation and absolute/idle expiry apply to both generations. The console shares in-flight refreshes within a tab, serializes them across tabs using Web Locks where available, and reloads the current identity after a refresh conflict.

Explicit authorization credentials that no configured validator accepts return **401**, including unsupported schemes and unknown JWT issuers. They never fall back to an existing browser session or anonymous public access. Requests without credentials may still access public services.

Header credentials take precedence over cookies. An invalid explicit credential therefore fails closed instead of silently falling back to an existing browser session.

## Stored API keys

Create workspace-scoped keys through the management API. The full secret is returned once and only its hash is stored.

~~~bash
curl -X POST \
  http://localhost:9000/api/v1/workspaces/acme/apikeys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "map-client",
    "owner_name": "Web map",
    "role_id": "viewer"
  }'
~~~

Revoke a key with DELETE on its management URL. A revoked key can no longer authenticate but stays listed, so its prefix can still be traced to a name and owner. Once that record is no longer needed, remove it permanently with DELETE on `/api/v1/workspaces/{workspace}/apikeys/{keyId}/permanent`; this also deletes browser sessions created from the key. Active keys are refused with 409 and must be revoked first. Prefer separate keys per application and use expires_at for temporary access.

## OIDC

~~~toml
[Auth]
Enabled = true
Method = "oidc"
RequireHTTPS = true

[Auth.OIDC]
IssuerURL = "https://identity.example.com/realms/geo"
ClientID = "neoserver"
RequiredScopes = ["openid"]
SkipIssuerCheck = false
BrowserLoginEnabled = true
BrowserScopes = ["openid", "profile", "email"]
GroupClaims = ["groups", "cognito:groups", "realm_access.roles", "roles"]
~~~

The server validates signature, issuer, audience/client, and expiry. API bearer tokens must also contain every configured `RequiredScopes` value in a recognized string or string-array scope claim; missing, empty, malformed, or insufficient scopes are rejected when requirements are configured. Keep issuer checking enabled in production.

`BrowserScopes` are requested during browser Authorization Code + PKCE login. That login verifies the ID token separately and does not require access-token scope claims on the ID token. `RequiredScopes` governs resource/API bearer authentication, not ID-token login. Local role mappings still govern what a logged-in principal can access.

For console login, register the callback URL reported by `/api/v1/console/config` and configure a public Authorization Code client with PKCE S256. The provider's discovery and token endpoints must permit browser access. The console never uses or exposes a client secret. See [Administration console](admin-console.md) for provider-specific claim conventions.

Map claims to roles:

~~~bash
neoserver add-claim-mapping \
  --store-path ./data/neoserver.db \
  --workspace acme \
  --claim groups \
  --value acme-editors \
  --role editor
~~~

Use workspace * for a global mapping. Higher priority mappings are checked first.

OIDC sessions retain the safe claims presented at login and re-evaluate local mappings from those values. Changes at the identity provider are visible only after a new login or session expiry; revoke the browser session for immediate termination.

### Authorizing with identity-provider groups

Group values match exactly. When several mappings match the same workspace, neoserver chooses priority descending, then built-in role strength (`super_admin`, `admin`, `editor`, `viewer`), then role ID lexicographically. A workspace-specific and a global `*` mapping may both apply. The console Identity page can build a mapping from claims captured in the current session; use the `init` bootstrap JWT when the first OIDC administrator has not been mapped yet.

Every provider uses the callback shown by `/api/v1/console/config`, normally `https://host[/base-path]/admin/auth/callback`, on a public PKCE S256 client:

| Provider | Group claim | Provider setup | Example mapping |
| --- | --- | --- | --- |
| AWS Cognito | `cognito:groups` | Use a public user-pool app client, enable authorization code + PKCE, and allow the callback and browser token request | `cognito:groups = gis-admins` → workspace `admin` |
| Clerk | `groups` or a namespaced template claim | Add organization/group values to the JWT template used by the OIDC application | `groups = org:acme:gis` → workspace `admin` |
| Keycloak | `realm_access.roles` | Add realm roles to the ID token through the client scope; group mappers may instead emit `groups` | `realm_access.roles = neoserver-admin` → workspace `admin` |
| Auth0 | A namespaced claim such as `https://geo.example/roles` | Add the claim to the ID token in an Action and include that exact name in `GroupClaims` | `https://geo.example/roles = publisher` → workspace `admin` |
| Okta | `groups` | Add a groups claim to the ID token and filter it to the groups neoserver needs | `groups = GIS Operators` → workspace `admin` |
| Microsoft Entra ID | `groups` or `roles` | Configure group object IDs or app roles in the ID token | `groups = <group-object-id>` → workspace `admin` |

Entra's group-overage form (`_claim_names`/`_claim_sources`) is detected and rejected because neoserver does not call Microsoft Graph to expand it. Prefer app roles or a filtered group claim for users with more than the token limit. Literal claim names are checked before dotted traversal, so both namespaced claims and nested `realm_access.roles` work. Identity-provider membership changes propagate on the next login; local mapping changes take effect during the existing session.

## Static API key and Basic auth

~~~toml
[Auth]
Enabled = true
Method = "apikey"
ApiKey = "shared-secret"
DefaultRole = "viewer"
RequireHTTPS = true
AllowAPIKeyInQuery = false
~~~

For Basic auth, set Method to basic and define Auth.Users or AUTH_USERS. Static and Basic principals receive Auth.DefaultRole globally. Setting it to super_admin gives every holder full management access and is strongly discouraged.

Browser sessions for these credentials re-evaluate `Auth.DefaultRole` on every authenticated request, just like direct Basic/static-key authentication. Restart with the updated configuration to apply a policy change; existing cookies cannot retain the old elevated role. Removing or changing the credential still invalidates its sessions.

Query-string API keys are disabled by default because URLs leak through logs, caches, browser history, and referrers.

## Roles

| Role | Scope | Typical access | Admin console |
| --- | --- | --- | --- |
| super_admin | Global | All workspaces and global administration | Yes, including server administration |
| admin | Workspace | Manage services, layers, credentials, settings, and stored queries | Yes, for the assigned workspace |
| editor | Workspace | Modify layers/styles via the management API and perform WFS write/lock operations | No |
| viewer | Workspace | Read visible OGC resources and the management API | No |

The admin console requires `admin` for a workspace or `super_admin`. Editor and viewer credentials still work for OGC clients and the management API; signing in to the console with them shows a page that explains this and lets the user copy an access request for an administrator.

The management API can create additional role records, but access policy is based on the permissions configured by the server's RBAC model.

Use **Roles & policies** or `POST /api/v1/roles/{roleId}/policies` to add an operation grant. `workspace` accepts an existing UUID or name and is stored as a UUID; `*` explicitly grants across all workspaces. Blank or `*` operations grant all operations of that service with the chosen action. Grants are additive, not deny rules, and do not bypass service access or layer visibility.

| WFS operations | Required action |
| --- | --- |
| Capabilities, feature/schema/property reads, stored-query listings/descriptions | `read` |
| Transaction, LockFeature, GetFeatureWithLock | `write` |
| CreateStoredQuery, DropStoredQuery | `manage` |

Other supported protocols currently expose read operations. Unsupported service/operation/action combinations and unknown workspaces return 422 instead of creating ineffective policies. XML POST authorization uses the XML root operation, just like dispatch; conflicting duplicate routing parameters and malformed XML are rejected. Equivalent repeated parameters remain compatible with existing clients. Legacy unmatched workspace policies are shown with a warning and remain removable using their exact stored scope; replace them with an existing workspace selection.

## Public services and layer rules

Each workspace service has a public flag:

- false requires a workspace role
- true permits anonymous access

Layer access is then evaluated:

| Layer setting | Result |
| --- | --- |
| public: true | Visible to every caller that can reach the service |
| allowed_roles non-empty | Visible only to those roles and super_admin |
| allowed_roles empty | Visible to any caller with service access |

Listings, capabilities, feature reads, maps, feature info, and tiles all apply the same filtering. A disallowed direct layer request returns not found.

## Transport security

With Auth.RequireHTTPS enabled, authenticated requests must arrive over TLS or through a trusted proxy that reports X-Forwarded-Proto: https. Configure Server.TrustedProxyCIDRs so untrusted clients cannot spoof forwarding headers.

Security checklist:

- Keep the store encryption key and credentials out of TOML committed to source control
- Prefer OIDC or scoped stored API keys
- Use viewer by default and grant editor/admin narrowly
- Rotate and revoke application credentials independently
- Restrict CORS and datasource network/path access
- Protect bootstrap and recovery token output

Related: [Deployment](deployment.md) · [Management API](management-api.md)
