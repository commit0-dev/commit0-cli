import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';
/**
 * ChatPanelProvider is a WebviewViewProvider that renders in a sidebar view container.
 * Register it in the activity bar; user can drag it to the secondary side bar (right).
 */
export declare class ChatPanelProvider implements vscode.WebviewViewProvider {
    private extensionUri;
    private api;
    private view?;
    constructor(extensionUri: vscode.Uri, api: Commit0Api);
    resolveWebviewView(view: vscode.WebviewView): void;
    /** Reveal the chat panel programmatically. */
    reveal(): void;
    private cachedSlug;
    private getRepoSlug;
    private handleInput;
    private handleQuery;
    private handleTrace;
    private handleBlast;
    private getHtml;
}
