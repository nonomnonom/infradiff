# infradiff for VS Code

Preview Docker Compose, Terraform, and Kubernetes infrastructure as an
interactive diagram, or diff the working tree against a git revision.

The extension is a thin wrapper around the `infradiff` CLI. It does not parse
files itself, does not download anything, and sends nothing over the network.

## Requirements

Install the CLI first:

```bash
go install github.com/infradiff/infradiff/cmd/infradiff@latest
```

If the binary is not on `PATH`, set `infradiff.executable` to its absolute path
in Settings.

## Commands

- **infradiff: Preview Infrastructure** (`infradiff.preview`) renders the active
  file, the selected file or folder, or the workspace folder as a self-contained
  HTML diagram in a webview. The HTML includes the Mermaid runtime, so the
  preview works offline.
- **infradiff: Diff Infrastructure Against Git Revision** (`infradiff.diff`)
  asks for a revision (default `HEAD`), compares it with the working tree, and
  opens a diagram where added, removed, and changed nodes are highlighted.

The preview command is also available from the editor title and the Explorer
context menu for `.yaml`, `.yml`, `.tf`, and `.json` files, and for folders.

Kubernetes and Terraform inputs are parsed per directory because their files
reference each other. Select the directory to get the complete graph.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `infradiff.executable` | `infradiff` | Path to the CLI. Use an absolute path when it is not on `PATH`. |

## Limitations

- The extension runs the CLI on the local machine and is declared as a
  workspace extension.
- Preview reads the saved file. Unsaved changes prompt for a save first.
- Only the HTML renderer is used for previews; the CLI remains the source of
  Mermaid and JSON output.
- The extension has not been tested in a remote extension host.
