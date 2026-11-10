# Graph JSON schema

`infradiff generate -f json` and `infradiff diff -f json` emit the same
document shape. `schema_version` is the version of this document, not the
product version. It is currently `1` and only changes when the JSON shape
changes.

The test suite extracts the JSON Schema block at the end of this file and
validates real renderer output with it, so this document cannot drift from the
implementation.

## Top level

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | integer | Always `1` for this shape. |
| `nodes` | array | Infrastructure components, sorted by `id`. |
| `edges` | array | Directed relationships, sorted by `from`, then `to`. |
| `groups` | array | Projects, namespaces, and modules, sorted by `id`. |
| `summary` | object | Counts per status plus one human-readable line. |

## node

```json
{
  "id": "compose:service:api",
  "name": "api",
  "type": "service",
  "group": "group:project:shop",
  "source": { "file": "compose.yaml", "line": 12 },
  "metadata": { "image": "shop/api:latest", "ports": "8080:8080" },
  "status": "unchanged"
}
```

- `id` is stable and independent of absolute paths, so moving a repository does
  not make every node look changed.
- `type` is one of `service`, `database`, `cache`, `queue`, `storage`,
  `loadbalancer`, `network`, `ingress`, `compute`, `function`, `other`.
- `group` is a group `id` or an empty string.
- `source.file` is relative to the scanned root and uses forward slashes.
  `source.line` is `0` when the format has no line information, as with
  Terraform plan JSON.
- `metadata` values are always strings. Keys are sorted by the JSON encoder.
  Values of keys that look secret (`password`, `secret`, `token`, `key`,
  `credential`) are replaced with `[redacted]` before rendering.
- `status` is one of `unchanged`, `added`, `removed`, `changed`. Node
  attributes that trigger `changed` are `name`, `type`, `group`, and
  `metadata`; a moved source line does not.

## edge

```json
{
  "from": "compose:service:api",
  "to": "compose:service:db",
  "kind": "depends_on",
  "status": "unchanged"
}
```

`kind` is one of `depends_on`, `network`, `volume`, `port`, `reference`,
`routes_to`. A pair of nodes has at most one edge; when several relationships
apply, the most specific kind is kept (`routes_to` > `reference` >
`depends_on` > `port` > `volume` > `network`). Edges are `changed` when the same
pair keeps its endpoints but changes kind.

## group

```json
{
  "id": "group:project:shop",
  "name": "shop",
  "kind": "project",
  "status": "unchanged"
}
```

`kind` is one of `project` (Compose), `namespace` (Kubernetes), or `module`
(Terraform). `metadata` is optional and currently carries Terraform module
`count` or `for_each` information.

## summary

```json
{
  "nodes": { "added": 0, "removed": 0, "changed": 0, "unchanged": 3 },
  "edges": { "added": 0, "removed": 0, "changed": 0, "unchanged": 2 },
  "text": "+0 added, -0 removed, ~0 changed"
}
```

`text` is the plain-text line printed by the diff command. Counts cover nodes
and edges separately; `text` totals nodes, edges, and changed groups. Group
statuses remain in the top-level `groups` array.

## Determinism

The same inputs always produce byte-identical JSON: nodes, edges, and groups
are sorted, map keys are emitted in sorted order, source paths are relative and
slash-separated, and no timestamps or absolute paths are included.

## Schema

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://infradiff.dev/schema/graph-1.json",
  "title": "infradiff graph",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "nodes", "edges", "groups", "summary"],
  "properties": {
    "schema_version": { "const": 1 },
    "nodes": { "type": "array", "items": { "$ref": "#/$defs/node" } },
    "edges": { "type": "array", "items": { "$ref": "#/$defs/edge" } },
    "groups": { "type": "array", "items": { "$ref": "#/$defs/group" } },
    "summary": { "$ref": "#/$defs/summary" }
  },
  "$defs": {
    "status": {
      "type": "string",
      "enum": ["unchanged", "added", "removed", "changed"]
    },
    "nodeType": {
      "type": "string",
      "enum": [
        "service", "database", "cache", "queue", "storage", "loadbalancer",
        "network", "ingress", "compute", "function", "other"
      ]
    },
    "edgeKind": {
      "type": "string",
      "enum": ["depends_on", "network", "volume", "port", "reference", "routes_to"]
    },
    "source": {
      "type": "object",
      "additionalProperties": false,
      "required": ["file", "line"],
      "properties": {
        "file": { "type": "string", "minLength": 1 },
        "line": { "type": "integer", "minimum": 0 }
      }
    },
    "metadata": {
      "type": "object",
      "additionalProperties": { "type": "string" }
    },
    "node": {
      "type": "object",
      "additionalProperties": false,
      "required": ["id", "name", "type", "group", "source", "metadata", "status"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "name": { "type": "string", "minLength": 1 },
        "type": { "$ref": "#/$defs/nodeType" },
        "group": { "type": "string" },
        "source": { "$ref": "#/$defs/source" },
        "metadata": { "$ref": "#/$defs/metadata" },
        "status": { "$ref": "#/$defs/status" }
      }
    },
    "edge": {
      "type": "object",
      "additionalProperties": false,
      "required": ["from", "to", "kind", "status"],
      "properties": {
        "from": { "type": "string", "minLength": 1 },
        "to": { "type": "string", "minLength": 1 },
        "kind": { "$ref": "#/$defs/edgeKind" },
        "status": { "$ref": "#/$defs/status" }
      }
    },
    "group": {
      "type": "object",
      "additionalProperties": false,
      "required": ["id", "name", "kind", "status"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "name": { "type": "string", "minLength": 1 },
        "kind": { "type": "string", "enum": ["project", "namespace", "module"] },
        "metadata": { "type": "object", "additionalProperties": { "type": "string" } },
        "status": { "$ref": "#/$defs/status" }
      }
    },
    "counts": {
      "type": "object",
      "additionalProperties": false,
      "required": ["added", "removed", "changed", "unchanged"],
      "properties": {
        "added": { "type": "integer", "minimum": 0 },
        "removed": { "type": "integer", "minimum": 0 },
        "changed": { "type": "integer", "minimum": 0 },
        "unchanged": { "type": "integer", "minimum": 0 }
      }
    },
    "summary": {
      "type": "object",
      "additionalProperties": false,
      "required": ["nodes", "edges", "text"],
      "properties": {
        "nodes": { "$ref": "#/$defs/counts" },
        "edges": { "$ref": "#/$defs/counts" },
        "text": { "type": "string" }
      }
    }
  }
}
```
