// Module: extension (VS Code client for the Novel language server)
//
// Purpose:
//   Activates the Novel extension and launches the novel-lsp binary as a
//   language server over stdio, wiring diagnostics into VS Code for .nv files.
//
// Key Components:
//   - activate(): resolves the server binary then starts the LanguageClient
//   - resolveServer(): setting -> workspace bin/novel-lsp -> PATH
//   - deactivate(): stops the client on shutdown
//
// Dependencies:
//   - vscode-languageclient/node: LSP client transport
//
// Note:
//   novel-lsp publishes compile diagnostics on open and on edit. Build it with
//   `make build` so bin/novel-lsp exists, or set `novel.server.path`.

import * as fs from "fs";
import * as path from "path";
import { workspace, ExtensionContext, window, commands } from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

// resolveServer picks the novel-lsp binary: an explicit setting wins, then a
// `bin/novel-lsp` (or `bin/novel-lsp.exe` on Windows) under any workspace folder,
// then the bare name so the OS resolves it on PATH.
function resolveServer(): string {
  const configured = workspace
    .getConfiguration("novel")
    .get<string>("server.path", "");
  if (configured) {
    return configured;
  }
  const isWindows = process.platform === "win32";
  const binaryName = isWindows ? "novel-lsp.exe" : "novel-lsp";
  for (const folder of workspace.workspaceFolders ?? []) {
    const candidate = path.join(folder.uri.fsPath, "bin", binaryName);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return binaryName;
}

export function activate(_context: ExtensionContext): void {
  const command = resolveServer();

  // novel-lsp speaks LSP over stdio (see cmd/novel-lsp).
  const serverOptions: ServerOptions = {
    run: { command, transport: TransportKind.stdio },
    debug: { command, transport: TransportKind.stdio },
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "novel" }],
    synchronize: {
      fileEvents: workspace.createFileSystemWatcher("**/*.nv"),
    },
  };

  client = new LanguageClient(
    "novel",
    "Novel Language Server",
    serverOptions,
    clientOptions
  );

  // novel.showEnvironment asks the server (via the custom novel/environment
  // request) where modules resolve: the NVLPATH store, the active runtime, the
  // LuaJIT version, and the importable global modules.
  _context.subscriptions.push(
    commands.registerCommand("novel.showEnvironment", () =>
      showEnvironment()
    )
  );

  client.start().catch((err) => {
    window.showErrorMessage(
      `Novel: failed to start language server (${command}). ` +
        `Run "make build" so bin/novel-lsp exists, or set "novel.server.path". ${err}`
    );
  });
}

// NovelEnvironment mirrors compiler.Environment returned by novel/environment.
interface NovelEnvironment {
  novelVersion: string;
  storeRoot: string;
  runtimeVersion: string;
  runtimeDir: string;
  novelModuleDir: string;
  luaModuleDir: string;
  novelModules: string[] | null;
  luaModules: string[] | null;
  luajitVersion: string;
  luajitRequired: string;
  luajitOk: boolean;
  luaPath: string;
  projectName: string;
  projectDir: string;
}

// showEnvironment requests the resolved environment from the server and renders
// it in an output channel, so users can see where imports resolve from.
async function showEnvironment(): Promise<void> {
  if (!client) {
    window.showErrorMessage("Novel: language server is not running.");
    return;
  }
  try {
    const env = await client.sendRequest<NovelEnvironment>(
      "novel/environment",
      {}
    );
    const lines: string[] = [
      `Novel ${env.novelVersion}`,
      `NVLPATH:          ${env.storeRoot || "(unset; using fallback runtime)"}`,
      `runtime version:  ${env.runtimeVersion}`,
      `runtime dir:      ${env.runtimeDir || "(not found)"}`,
    ];
    if (env.novelModuleDir) lines.push(`novel modules:    ${env.novelModuleDir}`);
    if (env.luaModuleDir) lines.push(`lua modules:      ${env.luaModuleDir}`);
    const luajit = !env.luajitVersion
      ? "not found"
      : env.luajitRequired
        ? `${env.luajitVersion} (${env.luajitOk ? "satisfies" : "does NOT satisfy"} pin ${env.luajitRequired})`
        : env.luajitVersion;
    lines.push(`luajit:           ${luajit}`);
    if (env.projectName) {
      lines.push(`project:          ${env.projectName} (${env.projectDir})`);
    }
    const novelMods = env.novelModules ?? [];
    if (novelMods.length > 0) {
      lines.push("global .nv modules:");
      for (const m of novelMods) lines.push(`  ${m}`);
    }
    const luaMods = env.luaModules ?? [];
    if (luaMods.length > 0) {
      lines.push("global .lua modules:");
      for (const m of luaMods) lines.push(`  ${m}`);
    }

    const channel = window.createOutputChannel("Novel Environment");
    channel.clear();
    channel.appendLine(lines.join("\n"));
    channel.show(true);
  } catch (err) {
    window.showErrorMessage(`Novel: failed to fetch environment. ${err}`);
  }
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
