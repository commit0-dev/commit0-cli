import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';
export declare class Commit0CodeLensProvider implements vscode.CodeLensProvider {
    private api;
    private _onDidChangeCodeLenses;
    readonly onDidChangeCodeLenses: vscode.Event<void>;
    private cachedSlug;
    constructor(api: Commit0Api);
    private getRepoSlug;
    refresh(): void;
    provideCodeLenses(document: vscode.TextDocument): Promise<vscode.CodeLens[]>;
    resolveCodeLens(lens: vscode.CodeLens): Promise<vscode.CodeLens | null>;
}
