# WFS 2.0

WFS is exposed at:

~~~text
/workspaces/{workspace}/wfs
~~~

WFS.Enabled must be true globally and the workspace WFS settings must be enabled. The endpoint accepts KVP GET and XML POST requests.

## Supported operations

Read and discovery:

- GetCapabilities
- DescribeFeatureType
- GetFeature
- GetPropertyValue
- ListStoredQueries
- DescribeStoredQueries

Administrative and write operations:

- Transaction
- CreateStoredQuery
- DropStoredQuery
- LockFeature
- GetFeatureWithLock

Transactions and locking require at least editor access. Creating or dropping stored queries requires admin access.

The process-level `WFS.AllowAnonymousMutations` switch is disabled by default.
It is a narrowly scoped escape hatch for isolated conformance/development
fixtures: when enabled, authentication must be globally disabled and anonymous
mutations apply only to public WFS workspaces. It grants request-local write or
admin authority for Transaction, LockFeature, GetFeatureWithLock,
CreateStoredQuery, and DropStoredQuery. Never enable it on a shared or
Internet-facing deployment.

## Enable a workspace

~~~bash
curl -X PUT \
  http://localhost:9000/api/v1/workspaces/acme/settings/wfs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "public": false,
    "title": "ACME WFS",
    "max_features": 10000,
    "default_count": 100
  }'
~~~

Workspace limits cannot exceed the global WFS limits.

## Capabilities and schemas

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetCapabilities"

curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType&TYPENAMES=buildings"
~~~

Capabilities include only layers visible to the caller. DescribeFeatureType returns an application schema for one or more published feature types.

## GetFeature

Request GeoJSON:

~~~bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/workspaces/acme/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=buildings&COUNT=10&OUTPUTFORMAT=application/json"
~~~

The default output is GML 3.2. Common parameters include TYPENAMES, COUNT, STARTINDEX, RESULTTYPE, SRSNAME, BBOX, SORTBY, PROPERTYNAME, RESOURCEID, NAMESPACES, and FILTER.

GetFeature also supports:

- `OUTPUTFORMAT=text/csv`: deterministic property-column order with geometry as compact GeoJSON;
- `OUTPUTFORMAT=application/geopackage+sqlite3` (`gpkg`, `geopackage`, and `application/x-sqlite3` are accepted aliases): one direct GDAL GeoPackage layer with the requested output CRS and a `feature_id` attribute;
- `OUTPUTFORMAT=shape-zip`: a ZIP containing ESRI Shapefile sidecars and a UTF-8 `.cpg` file. `FORMAT_OPTIONS=filename:<safe-basename>.zip` may select the archive name.

GeoPackage and SHAPE-ZIP are advertised only when their linked GDAL output driver is available. Both are written directly through the process-wide Go/GDAL package; request-time DuckDB databases, `INSTALL spatial`, intermediate GeoJSON, and DuckDB `COPY ... FORMAT GDAL` are not part of either export path.

Binary export is intentionally bounded to one `TYPENAMES` value, matching the server's existing no-spatial-join contract. SHAPE-ZIP splits mixed or collection geometry into deterministic `_point`, `_line`, and `_polygon` datasets, promotes single geometries when a family contains multi-geometries, and maps DBF field names to unique case-insensitive ten-character names. Unsupported geometry types and unsafe archive names fail explicitly.

Binary export runs through a bounded worker queue, writes only below a private directory under `WFS.TemporaryDirectory`, enforces both `MaxTemporaryBytes` and `MaxOutputBytes`, and removes the directory after the response. `MaxConcurrentExports`, `ExportQueueTimeoutMS`, and `ExportTimeoutMS` prevent export from monopolizing request workers. Binary/CSV responses bypass the feature response cache.

RESULTTYPE=hits returns counts without feature members. WFS.MaxOffset and CountTimeoutMS protect expensive paging and count operations.

## FES filters

GetFeature and GetPropertyValue support FES 2.0 comparison, logical, resource-ID, and spatial filters. Comparisons against `@gml:id` resolve a qualified feature identifier to the published type and configured ID column rather than treating it as an ordinary application property. XML POST is the clearest encoding for complex filters:

~~~xml
<wfs:GetFeature service="WFS" version="2.0.0"
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:fes="http://www.opengis.net/fes/2.0">
  <wfs:Query typeNames="buildings">
    <fes:Filter>
      <fes:PropertyIsGreaterThan>
        <fes:ValueReference>height</fes:ValueReference>
        <fes:Literal>50</fes:Literal>
      </fes:PropertyIsGreaterThan>
    </fes:Filter>
  </wfs:Query>
</wfs:GetFeature>
~~~

## Transactions

WFS Transaction supports insert, update, replace, and delete for writable sources. Currently PostGIS is the writable datasource. DuckDB, GeoParquet, vector files, and every SQL-view layer are read-only.

Transactions are atomic and may reference layers from only one service. The database account needs the corresponding table privileges.

~~~bash
curl -X POST \
  http://localhost:9000/workspaces/acme/wfs \
  -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/xml" \
  --data-binary @transaction.xml
~~~

Do not grant editor/admin roles to clients that only need feature reads.

## Locks and stored queries

LockFeature and GetFeatureWithLock coordinate write workflows. Global limits cap lock expiry, locks per workspace and principal, features per lock, cleanup interval, versioned features, and versions per feature.

The standard GetFeatureById stored query is available. Admins can create and drop custom stored-query definitions through WFS XML operations; definitions are persisted per workspace.

Feature identifiers use `published-type.local-id`. Built-in lookup and its locking variants resolve the longest matching published type prefix, so dots in publication names and the remaining local ID are preserved. Configured application namespace prefixes are accepted. If publication prefixes overlap, the longest match wins; an explicit type plus ResourceId disambiguates the intended publication. Visibility still applies after resolution. Numeric IDs and scalar values retain their JSON numeric tokens in GML and property responses, including integers above JavaScript's exact-integer range.

`GetPropertyValue` accepts the geometry column (with or without its namespace prefix) and returns actual GML geometry members in the requested `srsName`, rather than scalar text. Null geometry is represented by a nil member. Point, LineString, Polygon and their multi-geometries use the same selection and paging rules as other properties.

LockFeature and GetFeatureWithLock require both a write-operation grant and read visibility of every queried publication. All LockFeature queries are checked before source access. SQL views are read-only and cannot be locked, including older publications still bound to a physical source-layer name. Only the built-in GetFeatureById stored query is supported by locking operations; other stored-query definitions are rejected.

## Lock and version durability

Feature locks are persisted in the encrypted catalog store and survive a server restart: a lockId issued before a restart remains valid until its expiry (bounded by `WFS.MaxLockExpirySec`, one hour by default). A lock is only granted once its persisted record has been written; expired lock rows are pruned by the cleanup ticker. The single-active-node constraint still applies — locks are coordinated by one server process against one catalog file.

Transactions also record feature version *metadata* (version number, state, predecessor reference, principal) that survives restarts. Only metadata is kept — no feature content history — so version navigation cannot be served yet, and GetCapabilities truthfully declares `ImplementsFeatureVersioning` and `ImplementsVersionNav` as FALSE. Both flip to TRUE only when content-history snapshots are implemented.

## Conformance and limits

neoserver implements Simple and Basic WFS behavior, response paging, common FES comparison/logical/spatial predicates, transactions, stored queries, and locking. Not every optional WFS/FES conformance class or spatial predicate is implemented. Use GetCapabilities as the runtime declaration and test required client workflows against the target release.

The repository includes native live WFS integration tests plus a separate
digest-pinned official TEAM Engine suite; see
[Conformance](conformance.md) and [Development](development.md).

Related: [Data sources](data-sources.md) · [Authentication](authentication.md) · [Performance](performance.md)

## Version negotiation

The service supports WFS 2.0.0 and 2.0.2. GetCapabilities without a version
retains the 2.0.0 default. `VERSION=2.0.2` selects 2.0.2; `ACCEPTVERSIONS`
selects the first supported version in the supplied preference list. Responses
and capabilities cache entries retain the selected version. The stock official
conformance profile requests 2.0.0. The separately labelled official-derived
`wfs20/core202` profile exercises 2.0.2 with a narrowly scoped upstream test
correction; see [conformance profiles](conformance.md). Native tests cover both versions.

For 2.0.2, LockFeature requests combining `lockId` with query expressions
return `OperationParsingFailed` without changing the lock.
