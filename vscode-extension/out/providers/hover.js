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
exports.Commit0HoverProvider = void 0;
const vscode = __importStar(require("vscode"));
class Commit0HoverProvider {
    api;
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
    async provideHover(document, position) {
        const wordRange = document.getWordRangeAtPosition(position, /\w+/);
        if (!wordRange) {
            return null;
        }
        const word = document.getText(wordRange);
        if (word.length < 2) {
            return null;
        }
        try {
            const slug = await this.getRepoSlug();
            if (!slug) {
                return null;
            }
            const node = await this.api.nodeLookup(slug, word);
            if (!node?.ID) {
                return null;
            }
            const hood = await this.api.neighborhood(node.ID);
            const callers = hood.Callers ?? [];
            const callees = hood.Callees ?? [];
            const md = new vscode.MarkdownString();
            md.isTrusted = true;
            md.supportThemeIcons = true;
            md.appendMarkdown(`**${node.Kind}** \`${node.Qualified}\`\n\n`);
            if (node.Signature) {
                md.appendCodeblock(node.Signature, node.Language || 'go');
            }
            md.appendMarkdown(`\n\n---\n\n`);
            md.appendMarkdown(`$(symbol-reference) **${callers.length} callers** · $(symbol-method) **${callees.length} callees**\n\n`);
            if (callers.length > 0) {
                md.appendMarkdown(`**Callers:** ${callers.slice(0, 5).map((c) => `\`${c.Qualified}\``).join(', ')}${callers.length > 5 ? ` +${callers.length - 5} more` : ''}\n\n`);
            }
            if (callees.length > 0) {
                md.appendMarkdown(`**Callees:** ${callees.slice(0, 5).map((c) => `\`${c.Qualified}\``).join(', ')}${callees.length > 5 ? ` +${callees.length - 5} more` : ''}\n\n`);
            }
            md.appendMarkdown(`\`${node.FilePath}:${node.StartLine}\``);
            return new vscode.Hover(md, wordRange);
        }
        catch {
            return null;
        }
    }
}
exports.Commit0HoverProvider = Commit0HoverProvider;
//# sourceMappingURL=hover.js.map