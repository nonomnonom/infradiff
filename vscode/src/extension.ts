import { execFile } from "node:child_process";
import * as path from "node:path";
import { promisify } from "node:util";
import * as vscode from "vscode";

const execFileAsync = promisify(execFile);
const maxBuffer = 64 * 1024 * 1024;

let output: vscode.OutputChannel;

export function activate(context: vscode.ExtensionContext): void {
  output = vscode.window.createOutputChannel("infradiff");
  context.subscriptions.push(
    output,
    vscode.commands.registerCommand("infradiff.preview", (uri?: vscode.Uri) =>
      preview(uri)
    ),
    vscode.commands.registerCommand("infradiff.diff", (uri?: vscode.Uri) =>
      diff(uri)
    )
  );
}

export function deactivate(): void {
  // The output channel is disposed with the extension context.
}

async function preview(uri?: vscode.Uri): Promise<void> {
  const target = resolveTarget(uri);
  if (!target) {
    return;
  }
  if (!(await confirmSaved(target))) {
    return;
  }
  await withProgress("infradiff: rendering", async () => {
    const html = await runCLI(
      ["generate", target.fsPath, "-f", "html"],
      target
    );
    showPreview(`infradiff: ${path.basename(target.fsPath)}`, html);
  });
}

async function diff(uri?: vscode.Uri): Promise<void> {
  const target = resolveTarget(uri);
  if (!target) {
    return;
  }
  if (!(await confirmSaved(target))) {
    return;
  }
  const revision = await vscode.window.showInputBox({
    prompt: "Git revision to compare the working tree against",
    value: "HEAD",
    ignoreFocusOut: true
  });
  if (!revision) {
    return;
  }
  await withProgress("infradiff: diffing", async () => {
    const html = await runCLI(
      ["diff", "--git", revision, target.fsPath, "-f", "html"],
      target
    );
    showPreview(
      `infradiff diff: ${path.basename(target.fsPath)} @ ${revision}`,
      html
    );
  });
}

function resolveTarget(uri?: vscode.Uri): vscode.Uri | undefined {
  if (uri) {
    return uri;
  }
  const editor = vscode.window.activeTextEditor;
  if (editor) {
    return editor.document.uri;
  }
  const folder = vscode.workspace.workspaceFolders?.[0];
  if (!folder) {
    void vscode.window.showErrorMessage(
      "infradiff: open a file or folder in the workspace first"
    );
    return undefined;
  }
  return folder.uri;
}

async function confirmSaved(target: vscode.Uri): Promise<boolean> {
  const dirty = vscode.workspace.textDocuments.some(
    (document) =>
      document.isDirty && document.uri.toString() === target.toString()
  );
  if (!dirty) {
    return true;
  }
  const choice = await vscode.window.showWarningMessage(
    "infradiff reads files from disk. Save the file before previewing.",
    "Save"
  );
  if (choice !== "Save") {
    return false;
  }
  const document = vscode.workspace.textDocuments.find(
    (candidate) => candidate.uri.toString() === target.toString()
  );
  return document ? document.save() : false;
}

async function withProgress(
  title: string,
  task: () => Promise<void>
): Promise<void> {
  await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title },
    async () => {
      try {
        await task();
      } catch (error) {
        reportError(error);
      }
    }
  );
}

function reportError(error: unknown): void {
  const message = error instanceof Error ? error.message : String(error);
  const stderr =
    typeof error === "object" && error !== null && "stderr" in error
      ? String((error as { stderr?: unknown }).stderr ?? "")
      : "";
  if (stderr.trim()) {
    output.appendLine(stderr.trim());
  }
  output.appendLine(message);
  void vscode.window
    .showErrorMessage(`infradiff: ${firstLine(message)}`, "Show Output")
    .then((choice) => {
      if (choice === "Show Output") {
        output.show();
      }
    });
}

function firstLine(message: string): string {
  const line = message.split(/\r?\n/, 1)[0].trim();
  return line === "" ? message : line;
}

async function runCLI(args: string[], target: vscode.Uri): Promise<string> {
  const configured = vscode.workspace
    .getConfiguration("infradiff")
    .get<string>("executable", "infradiff")
    .trim();
  const executable = configured === "" ? "infradiff" : configured;
  const cwd =
    vscode.workspace.getWorkspaceFolder(target)?.uri.fsPath ??
    path.dirname(target.fsPath);
  try {
    const { stdout, stderr } = await execFileAsync(executable, args, {
      cwd,
      maxBuffer,
      windowsHide: true
    });
    if (stderr.trim()) {
      output.appendLine(stderr.trim());
    }
    return stdout;
  } catch (error) {
    if (
      typeof error === "object" &&
      error !== null &&
      (error as NodeJS.ErrnoException).code === "ENOENT"
    ) {
      throw new Error(
        `cannot run "${executable}"; install infradiff or set infradiff.executable`
      );
    }
    throw error;
  }
}

function showPreview(title: string, html: string): void {
  const panel = vscode.window.createWebviewPanel(
    "infradiff.preview",
    title,
    vscode.ViewColumn.Beside,
    { enableScripts: true, retainContextWhenHidden: true }
  );
  panel.webview.html = html;
}
