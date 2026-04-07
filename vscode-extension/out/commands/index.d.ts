import * as vscode from 'vscode';
import { Commit0Api } from '../api.js';
import { ProjectTreeProvider } from '../providers/projectTree.js';
export declare function registerCommands(context: vscode.ExtensionContext, api: Commit0Api, projectTree: ProjectTreeProvider): void;
