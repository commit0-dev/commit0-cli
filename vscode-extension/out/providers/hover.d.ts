import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';
export declare class Commit0HoverProvider implements vscode.HoverProvider {
    private api;
    private cachedSlug;
    constructor(api: Commit0Api);
    private getRepoSlug;
    provideHover(document: vscode.TextDocument, position: vscode.Position): Promise<vscode.Hover | null>;
}
