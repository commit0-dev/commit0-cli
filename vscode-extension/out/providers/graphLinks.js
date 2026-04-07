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
exports.GraphLinksProvider = void 0;
const vscode = __importStar(require("vscode"));
/**
 * GraphLinksProvider shows the relationships of the function under the cursor.
 * Updates automatically as the user moves their cursor in the editor.
 */
class GraphLinksProvider {
    api;
    projectTree;
    _onDidChangeTreeData = new vscode.EventEmitter();
    onDidChangeTreeData = this._onDidChangeTreeData.event;
    currentSymbol = '';
    currentNodeId = '';
    callers = [];
    callees = [];
    dataSinks = [];
    dataSources = [];
    reads = [];
    writes = [];
    loading = false;
    debounceTimer;
    constructor(api, projectTree) {
        this.api = api;
        this.projectTree = projectTree;
    }
    /** Call this from extension.ts to start tracking cursor. */
    startTracking(context) {
        context.subscriptions.push(vscode.window.onDidChangeTextEditorSelection((e) => {
            this.onCursorMove(e.textEditor);
        }), vscode.window.onDidChangeActiveTextEditor((editor) => {
            if (editor) {
                this.onCursorMove(editor);
            }
        }));
        // Initial load for current editor
        if (vscode.window.activeTextEditor) {
            this.onCursorMove(vscode.window.activeTextEditor);
        }
    }
    onCursorMove(editor) {
        clearTimeout(this.debounceTimer);
        this.debounceTimer = setTimeout(() => this.resolveAtCursor(editor), 250);
    }
    async resolveAtCursor(editor) {
        const doc = editor.document;
        const pos = editor.selection.active;
        // Find the function name at or near the cursor
        const funcName = this.findFunctionAtLine(doc, pos.line);
        if (!funcName || funcName === this.currentSymbol) {
            return;
        }
        this.currentSymbol = funcName;
        this.loading = true;
        this._onDidChangeTreeData.fire(undefined);
        try {
            const repo = await this.projectTree.getWorkspaceRepo();
            if (!repo) {
                this.clearData();
                return;
            }
            // Look up the node
            const node = await this.api.nodeLookup(repo.Slug, funcName);
            if (!node?.ID) {
                this.clearData();
                return;
            }
            this.currentNodeId = node.ID;
            // Fetch neighborhood
            const hood = await this.api.neighborhood(node.ID);
            this.callers = hood.Callers ?? [];
            this.callees = hood.Callees ?? [];
            this.dataSinks = hood.DataSinks ?? [];
            this.dataSources = hood.DataSources ?? [];
            this.reads = hood.Reads ?? [];
            this.writes = hood.Writes ?? [];
        }
        catch {
            this.clearData();
        }
        finally {
            this.loading = false;
            this._onDidChangeTreeData.fire(undefined);
        }
    }
    clearData() {
        this.callers = [];
        this.callees = [];
        this.dataSinks = [];
        this.dataSources = [];
        this.reads = [];
        this.writes = [];
        this.currentNodeId = '';
    }
    findFunctionAtLine(doc, line) {
        // Search upward from cursor to find the enclosing function definition
        const patterns = {
            go: /^\s*func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/,
            python: /^\s*(?:async\s+)?def\s+(\w+)\s*\(/,
            typescript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
            javascript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
            typescriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
            javascriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
        };
        const pattern = patterns[doc.languageId];
        if (!pattern) {
            return null;
        }
        // Search from current line upward
        for (let i = line; i >= Math.max(0, line - 50); i--) {
            const match = pattern.exec(doc.lineAt(i).text);
            if (match) {
                return match[1];
            }
        }
        return null;
    }
    getTreeItem(element) {
        return element;
    }
    async getChildren(element) {
        // Children of a section
        if (element instanceof SectionNode) {
            return element.children;
        }
        // Root level
        if (!this.currentSymbol) {
            const hint = new SectionNode('Place cursor on a function', [], false, 'info');
            hint.tooltip = 'Move your cursor to a function definition to see its relationships';
            return [hint];
        }
        if (this.loading) {
            return [new SectionNode('Loading...', [], false, 'loading~spin')];
        }
        const total = this.callers.length + this.callees.length +
            this.dataSinks.length + this.dataSources.length +
            this.reads.length + this.writes.length;
        if (total === 0 && this.currentNodeId) {
            return [
                new SectionNode(this.currentSymbol, [], false, 'symbol-function'),
                new SectionNode('No relationships found', [], false, 'info'),
            ];
        }
        if (total === 0) {
            return [new SectionNode(`${this.currentSymbol} — not indexed`, [], false, 'warning')];
        }
        const sections = [];
        sections.push(new SectionNode(this.currentSymbol, [], false, 'symbol-function'));
        if (this.callers.length > 0) {
            const items = this.callers.map((n) => new LinkItem(n, 'caller'));
            sections.push(new SectionNode(`Callers (${this.callers.length})`, items, true, 'arrow-left'));
        }
        if (this.callees.length > 0) {
            const items = this.callees.map((n) => new LinkItem(n, 'callee'));
            sections.push(new SectionNode(`Callees (${this.callees.length})`, items, true, 'arrow-right'));
        }
        if (this.dataSinks.length > 0) {
            const items = this.dataSinks.map((n) => new LinkItem(n, 'data-sink'));
            sections.push(new SectionNode(`Data sinks (${this.dataSinks.length})`, items, false, 'arrow-down'));
        }
        if (this.dataSources.length > 0) {
            const items = this.dataSources.map((n) => new LinkItem(n, 'data-source'));
            sections.push(new SectionNode(`Data sources (${this.dataSources.length})`, items, false, 'arrow-up'));
        }
        if (this.reads.length > 0) {
            const items = this.reads.map((field) => {
                const item = new vscode.TreeItem(field, vscode.TreeItemCollapsibleState.None);
                item.iconPath = new vscode.ThemeIcon('eye');
                return item;
            });
            sections.push(new SectionNode(`Reads (${this.reads.length})`, items, false, 'book'));
        }
        if (this.writes.length > 0) {
            const items = this.writes.map((field) => {
                const item = new vscode.TreeItem(field, vscode.TreeItemCollapsibleState.None);
                item.iconPath = new vscode.ThemeIcon('edit');
                return item;
            });
            sections.push(new SectionNode(`Writes (${this.writes.length})`, items, false, 'pencil'));
        }
        return sections;
    }
}
exports.GraphLinksProvider = GraphLinksProvider;
class SectionNode extends vscode.TreeItem {
    children;
    constructor(label, children, defaultExpanded = false, icon) {
        super(label, children.length > 0
            ? (defaultExpanded ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.Collapsed)
            : vscode.TreeItemCollapsibleState.None);
        this.children = children;
        if (icon) {
            this.iconPath = new vscode.ThemeIcon(icon);
        }
    }
}
class LinkItem extends vscode.TreeItem {
    constructor(neighbor, kind) {
        const shortName = neighbor.Qualified.includes('.')
            ? neighbor.Qualified.split('.').pop()
            : neighbor.Qualified;
        super(shortName, vscode.TreeItemCollapsibleState.None);
        this.description = neighbor.FilePath
            ? `${neighbor.FilePath}:${neighbor.StartLine}`
            : '';
        this.tooltip = [
            neighbor.Qualified,
            neighbor.Signature || '',
            neighbor.FilePath ? `${neighbor.FilePath}:${neighbor.StartLine}` : '',
        ].filter(Boolean).join('\n');
        // Click to navigate to the file:line
        if (neighbor.FilePath && neighbor.StartLine > 0) {
            this.command = {
                command: 'commit0.goToLocation',
                title: 'Go to',
                arguments: [neighbor.FilePath, neighbor.StartLine],
            };
        }
        // Icon by relationship kind
        switch (kind) {
            case 'caller':
                this.iconPath = new vscode.ThemeIcon('arrow-left', new vscode.ThemeColor('charts.blue'));
                break;
            case 'callee':
                this.iconPath = new vscode.ThemeIcon('arrow-right', new vscode.ThemeColor('charts.green'));
                break;
            case 'data-sink':
                this.iconPath = new vscode.ThemeIcon('arrow-down', new vscode.ThemeColor('charts.orange'));
                break;
            case 'data-source':
                this.iconPath = new vscode.ThemeIcon('arrow-up', new vscode.ThemeColor('charts.purple'));
                break;
        }
    }
}
//# sourceMappingURL=graphLinks.js.map