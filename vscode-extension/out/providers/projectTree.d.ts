import * as vscode from 'vscode';
import { Commit0Api, type Repo } from '../api.js';
type TreeNode = StatusNode | ActionNode;
export declare class ProjectTreeProvider implements vscode.TreeDataProvider<TreeNode> {
    private api;
    private _onDidChangeTreeData;
    readonly onDidChangeTreeData: vscode.Event<TreeNode | undefined>;
    private cachedRepo;
    constructor(api: Commit0Api);
    refresh(): void;
    getWorkspaceRepo(): Promise<Repo | null>;
    getTreeItem(element: TreeNode): vscode.TreeItem;
    getChildren(): Promise<TreeNode[]>;
}
declare class StatusNode extends vscode.TreeItem {
    static create(label: string, icon: string, tooltip: string, description?: string, color?: string): StatusNode;
}
declare class ActionNode extends vscode.TreeItem {
    static create(label: string, icon: string, command: vscode.Command, tooltip: string): ActionNode;
}
export {};
