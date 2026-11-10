# Repository instructions

infradiff is a Go CLI plus an MCP server, a composite GitHub Action, and a VS
Code extension. Keep the product surface exactly that: no web service, database,
telemetry, cloud calls, or extra commands.

## Invariants

- Runtime is fully offline. Do not add network calls to the CLI, parser,
  renderer, or MCP server. The only network use is `go mod download` during a
  build and the GitHub API in the optional Action comment.
- Output must stay deterministic. Sort before rendering, avoid timestamps,
  absolute paths, map iteration, and environment-dependent formatting.
- Secret values must never be rendered. Keys containing `password`, `secret`,
  `token`, `key`, or `credential` are redacted; keep that behavior for every new
  parser and metadata field.
- Node IDs are stable and path-independent: `compose:service:<name>`,
  `k8s:<kind>:<namespace>/<name>`, and `tf:<address>`.
- The product version stays `dev`. Do not add version numbers, changelogs, or
  release tags to source files. The VS Code manifest version is packaging
  metadata, not the CLI version.
- Do not add controls, flags, or configuration that the task did not ask for.

## Checks

Run the smallest relevant checks, and all of these before finishing a change:

```bash
gofmt -w .
go test ./...
make lint
```

For output changes, update golden files deliberately and inspect the diff.
Cross-compile with `make cross` after platform-sensitive changes.

VS Code changes:

```bash
cd vscode
npm ci
npm run typecheck
npm run build
npm run package
```

Action changes: exercise `scripts/github-action.sh` locally with `INPUT_*`,
`GITHUB_ACTION_PATH`, and `GITHUB_WORKSPACE` set; test both generate and
`base-ref` diff modes.

## Layout

- `cmd/infradiff`: CLI wiring only.
- `internal/model`: graph types, statuses, sorting, validation.
- `internal/parser`: registry and per-format parsers.
- `internal/infer`: leaf package with image/resource/kind inference.
- `internal/render`: Mermaid, JSON, HTML, and embedded assets.
- `internal/diff` and `internal/gitdiff`: graph diff and git extraction.
- `internal/watch`: debounced file watching.
- `internal/mcp`: MCP server and tools.
- `testdata`: fixtures and goldens.

## Commits

Write one or two concise sentences explaining why the change was made. Do not
add AI attribution, co-author trailers, or "generated with" lines.
