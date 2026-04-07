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
exports.Commit0CodeLensProvider = void 0;
const vscode = __importStar(require("vscode"));
// Regex patterns to detect function/method definitions by language
const functionPatterns = {
    go: /^\s*func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/,
    python: /^\s*(?:async\s+)?def\s+(\w+)\s*\(/,
    typescript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
    javascript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
    typescriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
    javascriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
};
class Commit0CodeLensProvider {
    api;
    _onDidChangeCodeLenses = new vscode.EventEmitter();
    onDidChangeCodeLenses = this._onDidChangeCodeLenses.event;
    cachedSlug;
    constructor(api) {
        this.api = api;
    }
    async getRepoSlug() {
        if (this.cachedSlug) {
            return this.cachedSlug;
        }
        try {
            const repos = await this.api.listRepos();
            if (repos.length === 0) {
                return undefined;
            }
            const wsPath = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
            const match = repos.find((r) => wsPath.startsWith(r.Path) || r.Path.startsWith(wsPath));
            if (match) {
                this.cachedSlug = match.Slug;
                return match.Slug;
            }
            // Fallback: first repo with indexed data
            const indexed = repos.find((r) => r.LastIndexedAt);
            if (indexed) {
                this.cachedSlug = indexed.Slug;
                return indexed.Slug;
            }
            this.cachedSlug = repos[0].Slug;
            return repos[0].Slug;
        }
        catch {
            return undefined;
        }
    }
    refresh() {
        this._onDidChangeCodeLenses.fire();
    }
    async provideCodeLenses(document) {
        const pattern = functionPatterns[document.languageId];
        if (!pattern) {
            return [];
        }
        const lenses = [];
        for (let i = 0; i < document.lineCount; i++) {
            const line = document.lineAt(i);
            const match = pattern.exec(line.text);
            if (!match) {
                continue;
            }
            const funcName = match[1];
            const range = new vscode.Range(i, 0, i, line.text.length);
            // We create placeholder lenses that will be resolved with real data
            const lens = new vscode.CodeLens(range, {
                title: `$(graph) ${funcName}`,
                command: '',
                arguments: [],
            });
            lens._funcName = funcName;
            lens._filePath = document.uri.fsPath;
            lens._line = i + 1;
            lenses.push(lens);
        }
        return lenses;
    }
    async resolveCodeLens(lens) {
        const funcName = lens._funcName;
        if (!funcName) {
            return lens;
        }
        try {
            const slug = await this.getRepoSlug();
            if (!slug) {
                return lens;
            }
            // Look up the node
            const node = await this.api.nodeLookup(slug, funcName);
            if (!node?.ID) {
                return lens;
            }
            // Fetch neighborhood
            const hood = await this.api.neighborhood(node.ID);
            const callers = hood.Callers?.length ?? 0;
            const callees = hood.Callees?.length ?? 0;
            lens.command = {
                title: `${callers} callers │ ${callees} callees │ Trace │ Blast`,
                command: 'commit0.traceSymbol',
                arguments: [funcName, slug],
                tooltip: `${funcName}: ${callers} callers, ${callees} callees`,
            };
        }
        catch {
            // API not available or node not indexed — show minimal lens
            lens.command = {
                title: `$(graph) ${funcName} — not indexed`,
                command: '',
                arguments: [],
            };
        }
        return lens;
    }
}
exports.Commit0CodeLensProvider = Commit0CodeLensProvider;
//# sourceMappingURL=codeLens.js.map