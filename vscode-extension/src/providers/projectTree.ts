import * as vscode from 'vscode';
import { Commit0Api, type Repo } from '../api.js';

type TreeNode = StatusNode | ActionNode;

export class ProjectTreeProvider implements vscode.TreeDataProvider<TreeNode> {
  private _onDidChangeTreeData = new vscode.EventEmitter<TreeNode | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;
  private cachedRepo: Repo | null = null;

  constructor(private api: Commit0Api) {}

  refresh(): void {
    this.cachedRepo = null;
    this._onDidChangeTreeData.fire(undefined);
  }

  async getWorkspaceRepo(): Promise<Repo | null> {
    if (this.cachedRepo) { return this.cachedRepo; }

    const wsPath = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    if (!wsPath) { return null; }

    try {
      const repos = await this.api.listRepos();
      this.cachedRepo = repos.find((r) =>
        wsPath.startsWith(r.Path) || r.Path.startsWith(wsPath)
      ) ?? null;
      return this.cachedRepo;
    } catch {
      return null;
    }
  }

  getTreeItem(element: TreeNode): vscode.TreeItem {
    return element;
  }

  async getChildren(): Promise<TreeNode[]> {
    const wsFolder = vscode.workspace.workspaceFolders?.[0];
    if (!wsFolder) {
      return [StatusNode.create('Open a folder to get started', 'folder', 'Open a workspace folder first')];
    }

    const healthy = await this.api.health();
    if (!healthy) {
      return [
        StatusNode.create('Server offline', 'error', 'commit0 serve is not running'),
        ActionNode.create('Run: commit0 serve', 'terminal',
          { command: 'commit0.showHelp', title: 'Help' },
          'Start the commit0 server to enable code intelligence'),
      ];
    }

    const repo = await this.getWorkspaceRepo();

    if (!repo) {
      return [
        StatusNode.create(wsFolder.name, 'folder', `${wsFolder.uri.fsPath}\nNot registered with commit0`),
        ActionNode.create('Register & index this workspace', 'add',
          { command: 'commit0.indexWorkspace', title: 'Index' },
          'Register the current workspace and index it'),
      ];
    }

    const items: TreeNode[] = [];

    if (repo.LastIndexedAt) {
      const ago = timeAgo(repo.LastIndexedAt);

      items.push(StatusNode.create(
        repo.Slug,
        'pass-filled',
        `Indexed ${ago}\n${repo.Path}${repo.Languages?.length ? '\nLanguages: ' + repo.Languages.join(', ') : ''}`,
        `indexed ${ago}`,
        'testing.iconPassed',
      ));

      items.push(ActionNode.create('Search code', 'search',
        { command: 'commit0.query', title: 'Search' },
        'Ask a natural language question about this codebase'));

      items.push(ActionNode.create('Trace a symbol', 'type-hierarchy',
        { command: 'commit0.traceSymbol', title: 'Trace' },
        'Follow call chains forward or backward'));

      items.push(ActionNode.create('Blast radius', 'pulse',
        { command: 'commit0.blastSymbol', title: 'Blast' },
        'See what breaks if a function changes'));

      items.push(ActionNode.create('Re-index', 'sync',
        { command: 'commit0.indexWorkspace', title: 'Re-index' },
        'Re-index to pick up code changes'));

    } else {
      items.push(StatusNode.create(
        repo.Slug,
        'circle-large-outline',
        `Registered but not yet indexed\n${repo.Path}`,
        'not indexed',
      ));

      items.push(ActionNode.create('Index this workspace', 'play',
        { command: 'commit0.indexWorkspace', title: 'Index' },
        'Parse source files, extract functions, generate embeddings'));
    }

    return items;
  }
}

class StatusNode extends vscode.TreeItem {
  static create(label: string, icon: string, tooltip: string, description?: string, color?: string): StatusNode {
    const node = new StatusNode(label);
    node.iconPath = new vscode.ThemeIcon(icon, color ? new vscode.ThemeColor(color) : undefined);
    node.tooltip = tooltip;
    if (description) { node.description = description; }
    node.collapsibleState = vscode.TreeItemCollapsibleState.None;
    return node;
  }
}

class ActionNode extends vscode.TreeItem {
  static create(label: string, icon: string, command: vscode.Command, tooltip: string): ActionNode {
    const node = new ActionNode(label);
    node.iconPath = new vscode.ThemeIcon(icon);
    node.command = command;
    node.tooltip = tooltip;
    node.collapsibleState = vscode.TreeItemCollapsibleState.None;
    return node;
  }
}

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) { return 'just now'; }
  if (mins < 60) { return `${mins}m ago`; }
  const hours = Math.floor(mins / 60);
  if (hours < 24) { return `${hours}h ago`; }
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
