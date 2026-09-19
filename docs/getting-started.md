# Getting started: API-first

This tutorial starts neoserver with Docker Compose, connects it to the repository's seeded PostGIS database, publishes two layers, enables WMS and WFS, and verifies the resulting services.

Prefer the browser? The separate [UI-first tutorial](getting-started-ui.md) covers sign-in, workspace creation, PostGIS connection or file upload, publication, preview and client access without management API commands. For contributing code, use [make dev](development.md#one-command-contributor-startup).

## Prerequisites

- Docker with Compose support
- curl
- jq
- OpenSSL
- Ports 9000 and 5432 available

Run all commands from the repository root.

## 1. Initialize the encrypted store

Set the local development encryption key and public server URL:

~~~bash
export NEOSRV_STORE_KEY=abc123 # Local tutorial only; generate a strong key for deployment.
export NEOSRV_SERVER_URLBASE="http://localhost:9000"
~~~

Keep NEOSRV_STORE_KEY safe. The same value is required whenever the store is opened, and it cannot be changed after initialization. For any non-local deployment, generate a key with `openssl rand -hex 32`; never use the example key. The key encrypts the catalog, it is not a sign-in token.

Initialize the store:

~~~bash
docker compose build server
docker compose run --rm --no-deps server init --store-path /data/neoserver.db
~~~

The command prints a super_admin bootstrap token that expires after 24 hours. Copy it into your shell:

~~~bash
export TOKEN="paste-bootstrap-token-here"
~~~

If the token expires, stop the server (the catalog has a single writer) and create another one with the same store and encryption key, then restart:

~~~bash
docker compose stop server
docker compose run --rm --no-deps server create-token \
  --store-path /data/neoserver.db \
  --role super_admin \
  --expires 24h
docker compose --profile postgis up -d
~~~

## 2. Start neoserver and PostGIS

The PostGIS service uses a Compose profile, so include it explicitly:

~~~bash
docker compose --profile postgis up --build
~~~

Run the remaining commands in another terminal with NEOSRV_STORE_KEY, NEOSRV_SERVER_URLBASE, and TOKEN exported.

Check readiness:

~~~bash
curl -fsS http://localhost:9000/ready | jq
~~~

Expected response:

~~~json
{"status":"ready","checks":{"catalog":"ok","catalog_lifecycle":"ok","mosaic_catalog":"disabled","tile_cache":"disabled","tile_cache_lease":"disabled","tile_jobs":"disabled"}}
~~~

## 3. Create a workspace

Workspaces isolate data connections, published layers, styles, service settings, and access control.

~~~bash
curl -fsS -X POST http://localhost:9000/api/v1/workspaces \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "demo",
    "description": "Getting started workspace"
  }' | jq
~~~

Management routes accept a workspace name or UUID. The public service URLs use the workspace name.

## 4. Connect the seeded PostGIS database

From the server container, the database host is db rather than localhost. The connection field is named user.

~~~bash
curl -fsS -X POST \
  http://localhost:9000/api/v1/workspaces/demo/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "sample-postgis",
    "type": "postgis",
    "connection_info": {
      "host": "db",
      "port": 5432,
      "database": "postgis",
      "user": "postgres",
      "password": "postgres",
      "sslmode": "disable",
      "schemas": ["public"]
    }
  }' | jq
~~~

Credentials in this example are for local development only.

## 5. Discover and publish layers

Discover the source tables:

~~~bash
curl -fsS -X POST \
  http://localhost:9000/api/v1/workspaces/demo/services/sample-postgis/discover \
  -H "Authorization: Bearer $TOKEN" | jq
~~~

Publish the seeded point and polygon tables:

~~~bash
curl -fsS -X POST \
  http://localhost:9000/api/v1/workspaces/demo/services/sample-postgis/layers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_layer": "public.places",
    "public_id": "places",
    "title": "Places"
  }' | jq

curl -fsS -X POST \
  http://localhost:9000/api/v1/workspaces/demo/services/sample-postgis/layers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_layer": "public.areas",
    "public_id": "areas",
    "title": "Areas"
  }' | jq
~~~

OGC API - Features is enabled for new workspaces. WMS and WFS must be enabled per workspace as well as globally; the Compose service enables them globally.

## 6. Enable WMS and WFS

~~~bash
curl -fsS -X PUT \
  http://localhost:9000/api/v1/workspaces/demo/settings/wms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "title": "Demo WMS",
    "max_width": 4096,
    "max_height": 4096
  }' | jq

curl -fsS -X PUT \
  http://localhost:9000/api/v1/workspaces/demo/settings/wfs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "title": "Demo WFS",
    "max_features": 10000
  }' | jq
~~~

## 7. Query the services

List collections and fetch features:

~~~bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/workspaces/demo/ogc/collections | jq

curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/demo/ogc/collections/places/items?limit=2" | jq
~~~

Request WMS and WFS capabilities:

~~~bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/demo/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities"

curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/demo/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetCapabilities"
~~~

Open the administration console at http://localhost:9000/admin/ and choose the `demo` workspace. Its Preview page provides the interactive map.

## 8. Create an application API key

Self-signed JWTs are intended primarily for bootstrap and recovery. Create a workspace API key for a client:

~~~bash
curl -fsS -X POST \
  http://localhost:9000/api/v1/workspaces/demo/apikeys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "demo-client",
    "owner_name": "Getting Started",
    "role_id": "viewer"
  }' | jq
~~~

The full key is returned only once. Use it with X-API-Key:

~~~bash
# Bash: paste the viewer secret without recording it in shell history.
read -r -s NEOSRV_API_KEY
export NEOSRV_API_KEY
printf '\n'
curl -fsS -H "X-API-Key: $NEOSRV_API_KEY" \
  http://localhost:9000/workspaces/demo/ogc/collections
~~~

## Stop and restart

Stop services without deleting persistent data:

~~~bash
docker compose --profile postgis down
~~~

On the next start, export the same NEOSRV_STORE_KEY and NEOSRV_SERVER_URLBASE, then run the compose up command. Do not run init again for an existing store.

Both the encrypted catalog (`serverdata`) and PostGIS (`pgdata`) use named Docker volumes. `docker compose --profile postgis down -v` deletes **both**, including imported files stored with the catalog. This is destructive, not a restart step; back up first and use it only if you explicitly want to discard the whole local environment.

## Next steps

- [Deployment](deployment.md)
- [Data sources](data-sources.md)
- [Management API](management-api.md)
- [Authentication and authorization](authentication.md)
- [OGC API - Features](ogc-api-features.md)
