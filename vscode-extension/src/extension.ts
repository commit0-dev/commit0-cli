import * as vscode from 'vscode';
import { Commit0Api } from './api.js';
import { ProjectTreeProvider } from './providers/projectTree.js';
import { GraphLinksProvider } from './providers/graphLinks.js';
import { Commit0CodeLensProvider } from './providers/codeLens.js';
import { Commit0HoverProvider } from './providers/hover.js';
import { ChatPanelProvider } from './panels/chatPanel.js';
import { registerCommands } from './commands/index.js';

let api: Commit0Api;

export async function activate(context: vscode.ExtensionContext) {
  api = new Commit0Api();

  const healthy = await api.health();
  if (!healthy) {
    vscode.window.showWarningMessage(
      'commit0: Cannot reach server. Start it with `commit0 serve` and reload.',
    );
  }

  // --- Sidebar: Project tree ---
  const projectTree = new ProjectTreeProvider(api);
  vscode.window.registerTreeDataProvider('commit0.projects', projectTree);

  // --- Explorer sidebar: Graph Links (cursor-tracking) ---
  const graphLinks = new GraphLinksProvider(api, projectTree);
  vscode.window.registerTreeDataProvider('commit0.graphLinks', graphLinks);
  graphLinks.startTracking(context);

  // --- Chat: WebviewViewProvider in its own activity bar container ---
  // User can drag the chat icon to the secondary side bar (right) to match Copilot/Claude layout
  const chatProvider = new ChatPanelProvider(context.extensionUri, api);
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider('commit0.chatView', chatProvider, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
  );

  // Command to open/focus chat
  context.subscriptions.push(
    vscode.commands.registerCommand('commit0.openChat', () => {
      chatProvider.reveal();
    }),
  );


  // --- CodeLens: inline callers/callees ---
  const codeLensProvider = new Commit0CodeLensProvider(api);
  const codeLensLanguages = ['go', 'python', 'typescript', 'javascript', 'typescriptreact', 'javascriptreact'];
  for (const lang of codeLensLanguages) {
    context.subscriptions.push(
      vscode.languages.registerCodeLensProvider({ language: lang }, codeLensProvider),
    );
  }

  // --- Hover: function info ---
  const hoverProvider = new Commit0HoverProvider(api);
  for (const lang of codeLensLanguages) {
    context.subscriptions.push(
      vscode.languages.registerHoverProvider({ language: lang }, hoverProvider),
    );
  }

  // --- Commands ---
  registerCommands(context, api, projectTree);

  // --- Go to location command ---
  context.subscriptions.push(
    vscode.commands.registerCommand('commit0.goToLocation', async (filePath: string, line: number) => {
      let uri: vscode.Uri;
      if (filePath.startsWith('/')) {
        uri = vscode.Uri.file(filePath);
      } else {
        const wsFolder = vscode.workspace.workspaceFolders?.[0];
        if (!wsFolder) { return; }
        uri = vscode.Uri.joinPath(wsFolder.uri, filePath);
      }
      const doc = await vscode.workspace.openTextDocument(uri);
      const editor = await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
      const pos = new vscode.Position(Math.max(0, line - 1), 0);
      editor.selection = new vscode.Selection(pos, pos);
      editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
    }),
  );

  // --- Status bar ---
  const statusItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 0);
  statusItem.text = healthy ? '$(graph) commit0' : '$(graph) commit0 (offline)';
  statusItem.tooltip = 'commit0 Chat';
  statusItem.command = 'commit0.openChat';
  statusItem.show();
  context.subscriptions.push(statusItem);
}

export function deactivate() {}
