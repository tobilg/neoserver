# Layer groups

Layer groups are workspace-scoped, ordered map compositions shared by WMS, WMTS, and OGC API - Tiles. A group can contain published feature layers, coverages, or nested groups. It is a map resource only: it does not become an OGC API feature collection or a vector-tile layer.

Create one through the management API:

```bash
curl -X POST http://localhost:9000/api/v1/workspaces/acme/layer-groups \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "public_id":"base-map",
    "title":"Base map",
    "enabled":true,
    "members":[
      {"resource":"terrain","style":"terrain"},
      {"resource":"roads","style":"roads","opacity":0.85,"composite":"multiply"}
    ]
  }'
```

The endpoints support list, create, get, update, and delete at `/api/v1/workspaces/{workspace}/layer-groups`. An identifier must not collide with another workspace resource, including a disabled one. All referenced resources/styles must exist. Updates reject cycles and nesting beyond `WMS.MaxGroupDepth`; renaming or directly deleting a referenced resource/group is rejected. Service deletion reports these references in its default `409` dependency response. Explicit `?recurse=true` service deletion removes the complete transitive closure of groups that would otherwise contain a dangling member.

Group visibility is the intersection of the group's own `public`/`allowed_roles` policy and every member's policy, so a group cannot disclose a restricted child. An alternate group style may contain a NamedLayer for each child. A requested group style is resolved independently for every expanded member.

Member opacity defaults to 1. Blend modes require the server and workspace `compositing` extension. Changes increment the group tile generation, making old persistent cache entries unreachable without destructive cache clearing.
