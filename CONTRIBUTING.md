# Contributing

## Setup

You need Go 1.27 or newer. Node.js is only required for the VS Code extension.

```bash
make build
make test
make lint
```

`make lint` installs golangci-lint into `.bin` on first use and does not modify
your global Go toolchain.

## Before opening a pull request

- `gofmt -w .`
- `go test ./...`
- `make lint`
- `make cross` when touching platform-dependent code
- Update `testdata/` and golden files only for intended output changes
- Add a test that fails without the change

Keep changes focused. This project intentionally has a small surface: a CLI,
MCP server, GitHub Action, and VS Code extension. New formats, flags, services,
or dependencies need a clear reason.

## Adding a parser

1. Implement `parser.Parser` (and `parser.DirParser` if files reference each
   other) in `internal/parser/<format>`.
2. Register it in `parser.DefaultRegistry`.
3. Use stable, path-independent node IDs and redact secret metadata values.
4. Add fixtures under `testdata/<format>/`, table-driven tests, and golden
   output when the graph shape is new.
5. Document the supported kinds or resources in the README.

## Changing the JSON shape

The schema block at the end of `docs/json-schema.md` is extracted and used to
validate real output in the test suite. Change the schema, the renderer, the
goldens, and `model.SchemaVersion` together, with a test that proves old output
is no longer claimed to be valid.

## VS Code extension

```bash
cd vscode
npm ci
npm run typecheck
npm run build
npm run package
```

The extension invokes the CLI; it must not grow its own parser. Do not add
runtime npm dependencies unless they are bundled and reviewed.

## GitHub Action

`scripts/github-action.sh` is the testable entry point. Run it locally with the
`INPUT_*`, `GITHUB_ACTION_PATH`, `GITHUB_WORKSPACE`, and `RUNNER_TEMP`
environment variables, for both generate and diff modes. The workflow examples
in `docs/github-action.md` are documentation, not executed by CI.

## Commit messages

Use a short subject in the imperative mood and a body explaining why when the
reason is not obvious. Do not include AI attribution or co-author trailers.
