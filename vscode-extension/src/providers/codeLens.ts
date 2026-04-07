import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';

// Regex patterns to detect function/method definitions by language
const functionPatterns: Record<string, RegExp> = {
  go: /^\s*func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/,
  python: /^\s*(?:async\s+)?def\s+(\w+)\s*\(/,
  typescript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
  javascript: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
  typescriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
  javascriptreact: /^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]/,
};

export class Commit0CodeLensProvider implements vscode.CodeLensProvider {
  private _onDidChangeCodeLenses = new vscode.EventEmitter<void>();
  readonly onDidChangeCodeLenses = this._onDidChangeCodeLenses.event;
  private cachedSlug: string | undefined;

  constructor(private api: Commit0Api) {}

  private async getRepoSlug(): Promise<string | undefined> {
    if (this.cachedSlug) { return this.cachedSlug; }
    try {
      const repos = await this.api.listRepos();
      if (repos.length === 0) { return undefined; }
      const wsPath = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
      const match = repos.find((r) => wsPath.startsWith(r.Path) || r.Path.startsWith(wsPath));
      if (match) { this.cachedSlug = match.Slug; return match.Slug; }
      // Fallback: first repo with indexed data
      const indexed = repos.find((r) => r.LastIndexedAt);
      if (indexed) { this.cachedSlug = indexed.Slug; return indexed.Slug; }
      this.cachedSlug = repos[0].Slug;
      return repos[0].Slug;
    } catch { return undefined; }
  }

  refresh(): void {
    this._onDidChangeCodeLenses.fire();
  }

  async provideCodeLenses(document: vscode.TextDocument): Promise<vscode.CodeLens[]> {
    const pattern = functionPatterns[document.languageId];
    if (!pattern) {
      return [];
    }

    const lenses: vscode.CodeLens[] = [];

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
      (lens as any)._funcName = funcName;
      (lens as any)._filePath = document.uri.fsPath;
      (lens as any)._line = i + 1;
      lenses.push(lens);
    }

    return lenses;
  }

  async resolveCodeLens(lens: vscode.CodeLens): Promise<vscode.CodeLens | null> {
    const funcName = (lens as any)._funcName as string;
    if (!funcName) {
      return lens;
    }

    try {
      const slug = await this.getRepoSlug();
      if (!slug) { return lens; }

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
    } catch {
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
