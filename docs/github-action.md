# GitHub Action

The composite action builds infradiff from this repository, then either renders
the current infrastructure or diffs it against a git revision. Results are
written to a file, added to the job summary as a Mermaid diagram, and optionally
posted as a pull request comment.

The action needs Go on the runner; it installs the requested version with
`actions/setup-go`.

## Diff a pull request

```yaml
name: infrastructure

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write

jobs:
  diagram:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: nonomnonom/infradiff@main
        with:
          path: deploy
          base-ref: ${{ github.event.pull_request.base.sha }}
          comment: true
```

`fetch-depth: 0` makes the base commit available for `infradiff diff --git`.
Without it, the diff fails because the base revision is not in the clone.

## Generate a diagram

```yaml
name: infrastructure

on:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  diagram:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: nonomnonom/infradiff@main
        id: diagram
        with:
          path: deploy
          format: html
      - uses: actions/upload-artifact@v4
        with:
          name: infradiff
          path: ${{ steps.diagram.outputs.output-path }}
```

## Inputs

| Input | Default | Description |
| --- | --- | --- |
| `path` | `.` | File or directory to parse, relative to the repository root. |
| `base-ref` | empty | Git revision to diff against. When empty, the current graph is generated. |
| `format` | `mermaid` | `mermaid`, `json`, or `html`. |
| `output` | runner temp | Output file. Relative paths resolve against the repository root. |
| `comment` | `false` | Post the result as a pull request comment. Requires `pull-requests: write`. |
| `fail-on-change` | `false` | Exit with status 1 when the diff finds changes. |
| `github-token` | `${{ github.token }}` | Token used by `gh pr comment`. |
| `go-version` | `1.27.x` | Go toolchain used to build infradiff. |

## Outputs

| Output | Description |
| --- | --- |
| `output-path` | Absolute path of the generated file. |

## Behavior

- A generated or diffed Mermaid graph is written to the job summary inside a
  collapsible block, so GitHub renders it without extra tooling.
- JSON output is included in the summary as a JSON code block.
- HTML output is not included in the summary or in pull request comments
  because it is a self-contained page; upload it as an artifact instead.
- Pull request comments are skipped for HTML output with a warning.
- `fail-on-change` only applies when `base-ref` is set. The summary and comment
  are still written before the action exits with status 1.
- The action makes no network calls except the Go module download needed to
  build the CLI and the GitHub API call used for comments.

The action script is tested locally on Windows Git Bash in both generate and
diff modes, including the `fail-on-change` exit status. The workflow files above
have not been executed on GitHub-hosted runners in this repository.
