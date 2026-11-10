---
name: infradiff
description: Inspect, diagram, and diff Docker Compose, Terraform, and Kubernetes infrastructure with the infradiff CLI or MCP server. Use when asked what infrastructure a repository defines, how two versions differ, or to produce a Mermaid, JSON, or HTML architecture diagram.
---

# infradiff

infradiff turns infrastructure files into one graph and can compare two graphs.
It is offline, deterministic, and redacts secret values.

## When to use it

Use infradiff when the user asks to:

- visualize Docker Compose services, Terraform resources, or Kubernetes objects;
- explain how services, databases, volumes, networks, and references connect;
- compare infrastructure between two directories, commits, or environments;
- add an up-to-date Mermaid architecture diagram to documentation;
- gate a change with a machine-readable diff.

Do not use it for cloud APIs, live clusters, or state inspection. It reads files
only; it does not run Terraform, Docker, or kubectl.

## Commands

```bash
infradiff generate <file-or-dir>
infradiff generate <file-or-dir> -f json -o graph.json
infradiff generate <file-or-dir> -f html -o diagram.html
infradiff diff <old> <new>
infradiff diff --git <revision> [path]
infradiff diff ... --fail-on-change
infradiff mcp [workspace]
```

- Default format is Mermaid on stdout.
- Compose is parsed per file. Terraform and Kubernetes are parsed per directory
  so references across files resolve. Select the directory, not one file, when
  the user wants the whole environment.
- Terraform plan JSON is also accepted as a single file.
- `--fail-on-change` exits 1 when anything changed; read the rendered output and
  the stderr summary before treating that as an execution failure.

## Output rules

- Mermaid is best for chat, Markdown, and pull request comments.
- JSON is best for scripts and for counting changes. The schema is in
  `docs/json-schema.md`.
- HTML is a self-contained page for humans; do not paste a 5 MB HTML file into a
  conversation. Write it to disk and link it.
- Never edit generated Mermaid, JSON, or HTML by hand. Change the input files and
  regenerate.

## MCP tools

When the infradiff MCP server is connected:

- `generate_diagram` with `path` and optional `format`.
- `diff_infrastructure` with `old_path`, `new_path`, and optional `format`.
- `explain_graph` with `path` for counts, groups, dependencies, and risks.

Paths are relative to the MCP workspace and cannot escape it. Use
`explain_graph` first when the user asks for an overview or risks.

## Reading a diff

- `+` added, `-` removed, `~` changed.
- A node is changed when its name, type, group, or metadata changes. A moved
  source line is not a change.
- A node pair has at most one edge; the most specific relationship wins.
- The summary line and per-element change lines are printed to stderr. The
  rendered diagram or JSON is on stdout.

## Verification

After generating a diagram for documentation, re-run the same command and
confirm the output is byte-identical. If the user asks whether a change is safe,
report the concrete added, removed, and changed nodes; do not infer runtime
health from the graph.
