"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const vscode = __importStar(require("vscode"));
const api_js_1 = require("./api.js");
const projectTree_js_1 = require("./providers/projectTree.js");
const graphLinks_js_1 = require("./providers/graphLinks.js");
const codeLens_js_1 = require("./providers/codeLens.js");
const hover_js_1 = require("./providers/hover.js");
const chatPanel_js_1 = require("./panels/chatPanel.js");
const index_js_1 = require("./commands/index.js");
let api;
async function activate(context) {
    api = new api_js_1.Commit0Api();
    const healthy = await api.health();
    if (!healthy) {
        vscode.window.showWarningMessage('commit0: Cannot reach server. Start it with `commit0 serve` and reload.');
    }
    // --- Sidebar: Project tree ---
    const projectTree = new projectTree_js_1.ProjectTreeProvider(api);
    vscode.window.registerTreeDataProvider('commit0.projects', projectTree);
    // --- Explorer sidebar: Graph Links (cursor-tracking) ---
    const graphLinks = new graphLinks_js_1.GraphLinksProvider(api, projectTree);
    vscode.window.registerTreeDataProvider('commit0.graphLinks', graphLinks);
    graphLinks.startTracking(context);
    // --- Chat: WebviewViewProvider in its own activity bar container ---
    // User can drag the chat icon to the secondary side bar (right) to match Copilot/Claude layout
    const chatProvider = new chatPanel_js_1.ChatPanelProvider(context.extensionUri, api);
    context.subscriptions.push(vscode.window.registerWebviewViewProvider('commit0.chatView', chatProvider, {
        webviewOptions: { retainContextWhenHidden: true },
    }));
    // Command to open/focus chat
    context.subscriptions.push(vscode.commands.registerCommand('commit0.openChat', () => {
        chatProvider.reveal();
    }));
    // --- CodeLens: inline callers/callees ---
    const codeLensProvider = new codeLens_js_1.Commit0CodeLensProvider(api);
    const codeLensLanguages = ['go', 'python', 'typescript', 'javascript', 'typescriptreact', 'javascriptreact'];
    for (const lang of codeLensLanguages) {
        context.subscriptions.push(vscode.languages.registerCodeLensProvider({ language: lang }, codeLensProvider));
    }
    // --- Hover: function info ---
    const hoverProvider = new hover_js_1.Commit0HoverProvider(api);
    for (const lang of codeLensLanguages) {
        context.subscriptions.push(vscode.languages.registerHoverProvider({ language: lang }, hoverProvider));
    }
    // --- Commands ---
    (0, index_js_1.registerCommands)(context, api, projectTree);
    // --- Go to location command ---
    context.subscriptions.push(vscode.commands.registerCommand('commit0.goToLocation', async (filePath, line) => {
        let uri;
        if (filePath.startsWith('/')) {
            uri = vscode.Uri.file(filePath);
        }
        else {
            const wsFolder = vscode.workspace.workspaceFolders?.[0];
            if (!wsFolder) {
                return;
            }
            uri = vscode.Uri.joinPath(wsFolder.uri, filePath);
        }
        const doc = await vscode.workspace.openTextDocument(uri);
        const editor = await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
        const pos = new vscode.Position(Math.max(0, line - 1), 0);
        editor.selection = new vscode.Selection(pos, pos);
        editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
    }));
    // --- Status bar ---
    const statusItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 0);
    statusItem.text = healthy ? '$(graph) commit0' : '$(graph) commit0 (offline)';
    statusItem.tooltip = 'commit0 Chat';
    statusItem.command = 'commit0.openChat';
    statusItem.show();
    context.subscriptions.push(statusItem);
}
function deactivate() { }
//# sourceMappingURL=extension.js.map