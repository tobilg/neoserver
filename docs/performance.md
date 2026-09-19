# Performance and observability

neoserver applies bounded paging, database pools, renderer queues, datasource download limits, and memory-limited response caches. Tune these together for the workload and available CPU/memory.

For reproducible workload measurements and the named-client evidence boundary,
run them against your own clients and data. Configuration limits
are controls, not measured capacity or a comparative GeoServer benchmark.

## Response caches

Five in-memory caches are available:

| Cache | Typical content |
| --- | --- |
| capabilities | WMS/WFS capabilities |
| collections | OGC collection listings and metadata |
| features | OGC items and WFS feature results |
| counts | Count-only queries |
| tiles | WMS maps and OGC tiles |

Entries are ephemeral and disappear on restart. Relevant workspace, service, layer, style, and settings updates invalidate entries automatically.

## Profiles and budgets

~~~toml
[Cache]
Enabled = true
MaxMemoryMB = 512
Profile = "balanced"
MetricsEnabled = true
FillCoalescingEnabled = true
FillTimeoutSec = 60
~~~

Profiles are balanced, feature-heavy, and map-heavy. They allocate default sub-cache budgets; an explicit nonzero MaxMemoryMB in a subsection wins. Enabled sub-cache budgets must not exceed the total.

~~~toml
[Cache.Features]
Enabled = true
TTLSec = 300
MaxEntries = 1000
MaxEntrySize = 10485760
MaxMemoryMB = 208

[Cache.Tiles]
Enabled = true
TTLSec = 900
MaxEntrySize = 10485760
MaxMemoryMB = 256
~~~

Maximum entry sizes prevent one large response from consuming a cache. Fill coalescing shares concurrent loads for the same key.

## Per-service policy

Services may override feature/tile cache enablement and TTL. Workspace Tiles settings may also disable caching. The most specific policy is applied without exceeding global memory controls.

## Metrics and administration

GET /api/v1/cache/stats returns hit/miss and memory statistics to super_admin. POST /api/v1/cache/clear, /cache/clear/{type}, or a workspace cache-clear endpoint invalidates entries.

When OpenTelemetry is enabled, cache instruments include used/max bytes, dropped/rejected sets, evictions, oversized entries, shared loads, load errors, TTL expirations, and invalidation-key counts labeled by cache type.

## Paging and query limits

- Paging.LimitDefault and LimitMax bound OGC API pages.
- Paging.MaxOffset and workspace max_offset can reject deep offset scans.
- Paging.CountTimeoutMS bounds exact OGC API `numberMatched` counts; a timed-out count is omitted instead of failing the page.
- WFS MaxFeatures, DefaultCount, MaxOffset, and CountTimeoutMS bound WFS work.
- Database pool size and connection lifetime should match the upstream database and process concurrency.
- Properties selection and spatial filters reduce transfer and rendering work.

Use indexes on PostGIS geometry, filter, sort, and identifier columns used by clients.

## Rendering and tiles

WMS controls image dimensions, total pixels, rendered features/vertices, concurrent renders, queue wait, and geometry simplification.

Tiles controls zoom range, feature/vertex/output size, concurrent renders, queue wait, and SQL statement timeout. Workspace settings can lower feature, vertex, and byte limits.

Zero global render concurrency derives a conservative value from GOMAXPROCS. Requests that cannot enter the render queue or exceed ceilings fail instead of consuming unbounded resources.

## Remote datasource limits

Remote file reads are restricted by URL allowlists, maximum bytes, timeout, and a local cache directory. Size the cache volume and use narrow URL patterns. Local and downloaded files must remain readable by the server's runtime user.

## OpenTelemetry

~~~toml
[Observability.OTel]
Enabled = true
ServiceName = "neoserver"
ShutdownTimeoutSec = 5
~~~

The exporter uses standard OTLP/HTTP environment variables:

~~~bash
export OTEL_EXPORTER_OTLP_ENDPOINT=https://otel.example.com
export OTEL_EXPORTER_OTLP_HEADERS="authorization=Bearer token"
~~~

HTTP server traces, cache metrics, catalog-lifecycle metrics, and audit-delivery metrics are exported. Audit instruments are `neoserver.audit.write_failures` (with `stage=direct|retry`), `neoserver.audit.outbox_enqueue_failures`, `neoserver.audit.retry_deliveries`, `neoserver.audit.retention_failures`, `neoserver.audit.events_lost`, `neoserver.audit.pending_events`, and `neoserver.audit.degraded`. Treat any `events_lost` increment as critical. Treat sustained degradation, backlog, write failures, or retention failures as an operator alert; the exact expression belongs in the deployment's metrics backend. If OpenTelemetry initialization fails, neoserver logs a warning and continues without export, but audit readiness and durable retry remain active.

## pprof

~~~toml
[Observability.Pprof]
Enabled = true
~~~

Profiles are exposed at /api/v1/debug/pprof/. They require a super_admin identity and secure transport. Do not expose them anonymously or leave continuous CPU/trace captures running unnecessarily.

## Tuning workflow

1. Set hard paging, render, tile, and remote-file ceilings first.
2. Size database pools below upstream limits.
3. Allocate cache memory within the container/process memory budget.
4. Observe hit rates, eviction pressure, queue failures, and latency.
5. Adjust profiles, TTLs, and concurrency using production traffic evidence.

Related: [Configuration](configuration.md) · [Deployment](deployment.md)
