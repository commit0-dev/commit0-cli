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
exports.registerCommands = registerCommands;
const vscode = __importStar(require("vscode"));
function registerCommands(context, api, projectTree) {
    let cachedSlug;
    async function getWorkspaceSlug() {
        if (cachedSlug) {
            return cachedSlug;
        }
        // Try workspace match first
        const repo = await projectTree.getWorkspaceRepo();
        if (repo) {
            cachedSlug = repo.Slug;
            return repo.Slug;
        }
        // Fallback: list all repos and let user pick
        try {
            const repos = await api.listRepos();
            if (repos.length === 0) {
                vscode.window.showWarningMessage('commit0: No projects indexed. Run "commit0: Index Project" first.');
                return undefined;
            }
            if (repos.length === 1) {
                cachedSlug = repos[0].Slug;
                return repos[0].Slug;
            }
            const picked = await vscode.window.showQuickPick(repos.map((r) => ({ label: r.Slug, description: r.Path })), { placeHolder: 'Select a commit0 project' });
            if (picked) {
                cachedSlug = picked.label;
                return picked.label;
            }
        }
        catch { /* ignore */ }
        return undefined;
    }
    context.subscriptions.push(
    // ---- Refresh ----
    vscode.commands.registerCommand('commit0.refreshProjects', () => {
        projectTree.refresh();
    }), 
    // ---- Index current workspace ----
    vscode.commands.registerCommand('commit0.indexWorkspace', async () => {
        const wsFolder = vscode.workspace.workspaceFolders?.[0];
        if (!wsFolder) {
            vscode.window.showWarningMessage('commit0: Open a folder first.');
            return;
        }
        const wsPath = wsFolder.uri.fsPath;
        const slug = wsFolder.name.toLowerCase().replace(/[^a-z0-9-]/g, '-');
        try {
            let repo = await projectTree.getWorkspaceRepo();
            if (!repo) {
                await api.createRepo(slug, wsPath, []);
                projectTree.refresh();
            }
            const repoSlug = repo?.Slug ?? slug;
            // Start the job
            const { job_id } = await api.indexProject(repoSlug, wsPath);
            // Poll for progress with real-time UI feedback
            await vscode.window.withProgress({
                location: vscode.ProgressLocation.Notification,
                title: `commit0: Indexing ${repoSlug}`,
                cancellable: false,
            }, async (progress) => {
                progress.report({ message: 'Starting...' });
                let lastFiles = 0;
                while (true) {
                    await new Promise((r) => setTimeout(r, 2000));
                    try {
                        const job = await api.indexJobStatus(job_id);
                        if (job.status === 'completed') {
                            progress.report({ message: `Done — ${job.files_indexed} files, ${job.nodes_created} nodes` });
                            vscode.window.showInformationMessage(`commit0: Indexed "${repoSlug}" — ${job.files_indexed} files, ${job.nodes_created} nodes`);
                            break;
                        }
                        if (job.status === 'failed') {
                            vscode.window.showErrorMessage(`commit0: Indexing failed — ${job.error || 'unknown error'}`);
                            break;
                        }
                        // Show incremental progress
                        const newFiles = job.files_indexed - lastFiles;
                        lastFiles = job.files_indexed;
                        if (job.files_indexed > 0) {
                            progress.report({
                                message: `${job.files_indexed} files indexed, ${job.nodes_created} nodes${newFiles > 0 ? ` (+${newFiles})` : ''}...`,
                            });
                        }
                    }
                    catch {
                        // Job status fetch failed — keep polling
                        progress.report({ message: 'Indexing in progress...' });
                    }
                }
            });
            projectTree.refresh();
            cachedSlug = repoSlug;
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Legacy: Index via picker (kept for command palette) ----
    vscode.commands.registerCommand('commit0.indexProject', async () => {
        vscode.commands.executeCommand('commit0.indexWorkspace');
    }), vscode.commands.registerCommand('commit0.indexProjectDirect', async (slug, path) => {
        try {
            await api.indexProject(slug, path);
            vscode.window.showInformationMessage(`commit0: Indexing "${slug}" started.`);
            setTimeout(() => projectTree.refresh(), 5000);
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Add Project (legacy, redirects to indexWorkspace) ----
    vscode.commands.registerCommand('commit0.addProject', () => {
        vscode.commands.executeCommand('commit0.indexWorkspace');
    }), 
    // ---- Query ----
    vscode.commands.registerCommand('commit0.query', async () => {
        const question = await vscode.window.showInputBox({
            prompt: 'Ask about your code',
            placeHolder: 'Where is rate limiting applied?',
        });
        if (!question) {
            return;
        }
        const slug = await getWorkspaceSlug();
        try {
            const result = await api.query(question, slug ?? '');
            const channel = vscode.window.createOutputChannel('commit0: Query');
            channel.show();
            channel.appendLine(`Query: "${question}"`);
            channel.appendLine(`Results: ${result.Nodes?.length ?? 0} (${result.Timing?.TotalMS}ms)\n`);
            if (result.Explanation) {
                channel.appendLine(result.Explanation);
                channel.appendLine('');
            }
            result.Nodes?.forEach((n, i) => {
                channel.appendLine(`  #${i + 1}  [${n.Node.Kind}] ${n.Node.Qualified}  (score: ${n.FusedScore.toFixed(2)})`);
                channel.appendLine(`       ${n.Node.FilePath}:${n.Node.StartLine}`);
            });
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Trace ----
    vscode.commands.registerCommand('commit0.traceSymbol', async (symbol) => {
        if (!symbol) {
            const editor = vscode.window.activeTextEditor;
            if (editor) {
                const wordRange = editor.document.getWordRangeAtPosition(editor.selection.active);
                symbol = wordRange ? editor.document.getText(wordRange) : undefined;
            }
            if (!symbol) {
                symbol = await vscode.window.showInputBox({ prompt: 'Symbol to trace' });
            }
        }
        if (!symbol) {
            return;
        }
        const slug = await getWorkspaceSlug();
        try {
            const result = await api.trace(symbol, slug ?? '');
            const channel = vscode.window.createOutputChannel('commit0: Trace');
            channel.show();
            channel.appendLine(`Trace: ${symbol} (${result.Direction})`);
            channel.appendLine(`Root: ${result.Root?.Qualified ?? symbol}`);
            channel.appendLine(`Hops: ${result.Tree?.length ?? 0}\n`);
            if (result.Explanation) {
                channel.appendLine(result.Explanation);
            }
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Blast ----
    vscode.commands.registerCommand('commit0.blastSymbol', async (symbol) => {
        if (!symbol) {
            const editor = vscode.window.activeTextEditor;
            if (editor) {
                const wordRange = editor.document.getWordRangeAtPosition(editor.selection.active);
                symbol = wordRange ? editor.document.getText(wordRange) : undefined;
            }
            if (!symbol) {
                symbol = await vscode.window.showInputBox({ prompt: 'Symbol for blast radius' });
            }
        }
        if (!symbol) {
            return;
        }
        const slug = await getWorkspaceSlug();
        try {
            const result = await api.blast(symbol, slug ?? '');
            const channel = vscode.window.createOutputChannel('commit0: Blast');
            channel.show();
            channel.appendLine(`Blast Radius: ${symbol}`);
            channel.appendLine(`Target: ${result.Target?.Qualified ?? symbol}`);
            channel.appendLine(`Affected: ${result.Affected?.length ?? 0} nodes\n`);
            if (result.Summary) {
                channel.appendLine(result.Summary);
                channel.appendLine('');
            }
            result.Affected?.forEach((a) => {
                channel.appendLine(`  [depth ${a.HopCount}] ${a.Node.Qualified} — ${a.Path}`);
            });
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Remove project ----
    vscode.commands.registerCommand('commit0.removeProject', async () => {
        const repo = await projectTree.getWorkspaceRepo();
        if (!repo) {
            return;
        }
        const confirm = await vscode.window.showWarningMessage(`Remove "${repo.Slug}" and all its indexed data?`, { modal: true }, 'Remove');
        if (confirm !== 'Remove') {
            return;
        }
        try {
            await api.deleteRepo(repo.Slug);
            projectTree.refresh();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            vscode.window.showErrorMessage(`commit0: ${message}`);
        }
    }), 
    // ---- Help ----
    vscode.commands.registerCommand('commit0.showHelp', () => {
        const channel = vscode.window.createOutputChannel('commit0: Getting Started');
        channel.show();
        channel.appendLine('commit0 — Graph-based code intelligence\n');
        channel.appendLine('Setup:');
        channel.appendLine('  1. Start the server:  commit0 serve');
        channel.appendLine('  2. Click "Index this workspace" in the commit0 sidebar');
        channel.appendLine('  3. Wait for indexing to complete (parses all source files)\n');
        channel.appendLine('Features:');
        channel.appendLine('  • CodeLens: callers/callees shown above each function');
        channel.appendLine('  • Hover: function info tooltip with graph context');
        channel.appendLine('  • Search: Cmd+Shift+P → "commit0: Search Code"');
        channel.appendLine('  • Trace: right-click a function → "commit0: Trace Call Chain"');
        channel.appendLine('  • Blast: right-click a function → "commit0: Blast Radius"');
        channel.appendLine('  • Chat: use the Chat panel in the commit0 sidebar');
    }), 
    // ---- Reveal in Finder ----
    vscode.commands.registerCommand('commit0.revealInFinder', (path) => {
        if (path) {
            vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(path));
        }
    }));
}
//# sourceMappingURL=index.js.map