import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';

export class Commit0HoverProvider implements vscode.HoverProvider {
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
      const indexed = repos.find((r) => r.LastIndexedAt);
      if (indexed) { this.cachedSlug = indexed.Slug; return indexed.Slug; }
      this.cachedSlug = repos[0].Slug;
      return repos[0].Slug;
    } catch { return undefined; }
  }

  async provideHover(
    document: vscode.TextDocument,
    position: vscode.Position,
  ): Promise<vscode.Hover | null> {
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
      if (!slug) { return null; }

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
    } catch {
      return null;
    }
  }
}
