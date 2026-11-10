# MCP server

`infradiff mcp` runs a Model Context Protocol server over stdio. It exposes the
same offline parser, renderer, and diff engine as the CLI to MCP clients such as
Claude Desktop, Claude Code, and Cursor.

```
infradiff mcp [workspace]
```

`workspace` is the directory every tool path is resolved against. It defaults
to the current directory. Paths outside the workspace, including paths that
escape through `..`, a symbolic link, or a directory junction, are rejected
before any file is read. A broken link or junction gets an explicit resolution
error rather than being reported as an ordinary missing file.

The server makes no network calls and writes nothing to disk. It prints only
newline-delimited JSON-RPC messages to stdout; logs, if any, go to stderr.

## Tools

| Tool | Arguments | Returns |
| --- | --- | --- |
| `generate_diagram` | `path` (required), `format` (`mermaid`, `json`, or `html`; default `mermaid`) | The rendered graph as text. |
| `diff_infrastructure` | `old_path`, `new_path` (required), `format` (default `mermaid`) | The diff graph with added, removed, and changed elements. |
| `explain_graph` | `path` (required) | Structured JSON: counts, groups, nodes by type, dependencies, and risks. |

`generate_diagram` and `diff_infrastructure` return the same deterministic
bytes as `infradiff generate` and `infradiff diff`. Environment values and
metadata keys that look like passwords, secrets, tokens, keys, or credentials
are redacted by the parsers.

`explain_graph` returns:

```json
{
  "schema_version": 1,
  "summary": "+0 added, -0 removed, ~0 changed",
  "node_count": 2,
  "edge_count": 1,
  "groups": [
    { "id": "group:project:risky", "name": "risky", "kind": "project", "nodes": 2 }
  ],
  "nodes_by_type": { "database": 1, "network": 1 },
  "dependencies": [
    { "from": "compose:service:db", "to": "compose:network:default", "kind": "port" }
  ],
  "risks": [
    {
      "severity": "high",
      "node": "compose:service:db",
      "message": "publishes host ports; keep data stores on a private network"
    }
  ]
}
```

Risk severities are `high`, `medium`, and `low`, sorted by severity and then by
node ID. Current rules flag data stores with published ports or public access,
security groups or firewalls that allow `0.0.0.0/0` or `::/0`, cluster services
exposed through `LoadBalancer` or `NodePort`, and images that float on `latest`.
The CLI diff also reports removed data stores as a data-loss risk.

## Build the binary

The configuration examples use an absolute path because MCP clients start the
server from an arbitrary directory.

```bash
go build -o bin/infradiff ./cmd/infradiff
```

Resolve the absolute path with `realpath bin/infradiff` (macOS/Linux) or
`Resolve-Path bin\infradiff.exe` (Windows). Use that path below, and pass the
project directory you want the agent to inspect as the workspace argument.

## Claude Desktop

Configuration file:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "infradiff": {
      "command": "/absolute/path/to/infradiff",
      "args": ["mcp", "/absolute/path/to/project"]
    }
  }
}
```

On Windows use the `.exe` path with escaped backslashes:

```json
{
  "mcpServers": {
    "infradiff": {
      "command": "C:\\Tools\\infradiff.exe",
      "args": ["mcp", "C:\\src\\project"]
    }
  }
}
```

Restart Claude Desktop after saving the file. The tools appear under the
connectors menu. MCP logs are in `~/Library/Logs/Claude` (macOS),
`%APPDATA%\Claude\logs` (Windows), or `~/.config/Claude/logs` (Linux).

## Claude Code

Add a stdio server with the CLI. Everything after `--` is the command to run:

```bash
claude mcp add --transport stdio infradiff -- /absolute/path/to/infradiff mcp /absolute/path/to/project
```

Check the connection and list the tools:

```bash
claude mcp list
```

The same server can be declared in a project-scoped `.mcp.json`:

```json
{
  "mcpServers": {
    "infradiff": {
      "type": "stdio",
      "command": "/absolute/path/to/infradiff",
      "args": ["mcp", "/absolute/path/to/project"]
    }
  }
}
```

## Cursor

Create `.cursor/mcp.json` in the project for project-scoped tools, or
`~/.cursor/mcp.json` for every project. The `type` field is required for stdio
servers.

```json
{
  "mcpServers": {
    "infradiff": {
      "type": "stdio",
      "command": "/absolute/path/to/infradiff",
      "args": ["mcp", "/absolute/path/to/project"]
    }
  }
}
```

Restart Cursor, then enable the server under Customize > MCPs. Connection errors
appear in the Output panel under MCP Logs.

If your client starts the server in the project directory, you can omit the
workspace argument and use `["mcp"]`.

## Example prompts

- "Generate a Mermaid diagram of `deploy/compose.yaml` and add it to the README."
- "Diff `environments/prod` against `environments/staging` and explain what
  changed."
- "Explain the Kubernetes manifests in `k8s/` and list the risky parts."

## Protocol details

- Transport: stdio, newline-delimited JSON-RPC 2.0.
- Tools are registered with JSON Schema input validation.
- Tool failures are returned as tool errors (`isError: true`) so the model can
  correct its input, not as protocol errors.
- `generate_diagram` and `diff_infrastructure` return text content;
  `explain_graph` also returns `structuredContent` matching the JSON above.

Verified against the Model Context Protocol Go SDK v1.8.0. Client configuration
shapes were checked against the official MCP quickstart (Claude Desktop),
Claude Code MCP documentation, and Cursor MCP documentation.
