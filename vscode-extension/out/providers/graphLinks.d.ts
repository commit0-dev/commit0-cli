import * as vscode from 'vscode';
import { Commit0Api, type NeighborNode } from '../api.js';
import { ProjectTreeProvider } from './projectTree.js';
type LinkNode = SectionNode | LinkItem;
/**
 * GraphLinksProvider shows the relationships of the function under the cursor.
 * Updates automatically as the user moves their cursor in the editor.
 */
export declare class GraphLinksProvider implements vscode.TreeDataProvider<LinkNode> {
    private api;
    private projectTree;
    private _onDidChangeTreeData;
    readonly onDidChangeTreeData: vscode.Event<LinkNode | undefined>;
    private currentSymbol;
    private currentNodeId;
    private callers;
    private callees;
    private dataSinks;
    private dataSources;
    private reads;
    private writes;
    private loading;
    private debounceTimer;
    constructor(api: Commit0Api, projectTree: ProjectTreeProvider);
    /** Call this from extension.ts to start tracking cursor. */
    startTracking(context: vscode.ExtensionContext): void;
    private onCursorMove;
    private resolveAtCursor;
    private clearData;
    private findFunctionAtLine;
    getTreeItem(element: LinkNode): vscode.TreeItem;
    getChildren(element?: LinkNode): Promise<LinkNode[]>;
}
declare class SectionNode extends vscode.TreeItem {
    readonly children: LinkNode[];
    constructor(label: string, children: LinkNode[], defaultExpanded?: boolean, icon?: string);
}
declare class LinkItem extends vscode.TreeItem {
    constructor(neighbor: NeighborNode, kind: string);
}
export {};
