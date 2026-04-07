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
exports.ChatPanelProvider = void 0;
const vscode = __importStar(require("vscode"));
/**
 * ChatPanelProvider is a WebviewViewProvider that renders in a sidebar view container.
 * Register it in the activity bar; user can drag it to the secondary side bar (right).
 */
class ChatPanelProvider {
    extensionUri;
    api;
    view;
    constructor(extensionUri, api) {
        this.extensionUri = extensionUri;
        this.api = api;
    }
    resolveWebviewView(view) {
        this.view = view;
        view.webview.options = {
            enableScripts: true,
            localResourceRoots: [this.extensionUri],
        };
        view.webview.html = this.getHtml();
        view.webview.onDidReceiveMessage(async (msg) => {
            if (msg.type === 'query') {
                await this.handleInput(msg.text);
            }
            if (msg.type === 'openFile') {
                vscode.commands.executeCommand('commit0.goToLocation', msg.path, msg.line);
            }
        });
    }
    /** Reveal the chat panel programmatically. */
    reveal() {
        if (this.view) {
            this.view.show(true);
        }
        else {
            // Focus the view to trigger resolveWebviewView if not yet created
            vscode.commands.executeCommand('commit0.chatView.focus');
        }
    }
    cachedSlug;
    async getRepoSlug() {
        if (this.cachedSlug) {
            return this.cachedSlug;
        }
        try {
            const repos = await this.api.listRepos();
            if (repos.length === 0) {
                return '';
            }
            // Try matching workspace path
            const wsPath = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
            const match = repos.find((r) => wsPath.startsWith(r.Path) || r.Path.startsWith(wsPath));
            if (match) {
                this.cachedSlug = match.Slug;
                return match.Slug;
            }
            // No path match — if only one repo, use it
            if (repos.length === 1) {
                this.cachedSlug = repos[0].Slug;
                return repos[0].Slug;
            }
            // Multiple repos, no match — let user pick
            const picked = await vscode.window.showQuickPick(repos.map((r) => ({ label: r.Slug, description: r.Path })), { placeHolder: 'Select a commit0 project' });
            if (picked) {
                this.cachedSlug = picked.label;
                return picked.label;
            }
            return '';
        }
        catch {
            return '';
        }
    }
    async handleInput(text) {
        if (!this.view) {
            return;
        }
        try {
            this.view.webview.postMessage({ type: 'loading', value: true });
            if (text.startsWith('/trace ')) {
                await this.handleTrace(text.slice(7).trim());
            }
            else if (text.startsWith('/blast ')) {
                await this.handleBlast(text.slice(7).trim());
            }
            else {
                await this.handleQuery(text);
            }
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            this.view.webview.postMessage({ type: 'error', message });
        }
        finally {
            this.view.webview.postMessage({ type: 'loading', value: false });
        }
    }
    async handleQuery(text) {
        const repoSlug = await this.getRepoSlug();
        const result = await this.api.query(text, repoSlug);
        // Prefer structured explanation if available
        const se = result.StructuredExplanation;
        this.view.webview.postMessage({
            type: 'queryResult',
            structured: se ? {
                overview: se.overview,
                evidence: se.evidence ?? [],
                insights: se.insights ?? [],
            } : null,
            explanation: result.Explanation,
            nodes: result.Nodes?.map((n) => ({
                name: n.Node.Qualified,
                kind: n.Node.Kind,
                filePath: n.Node.FilePath,
                startLine: n.Node.StartLine,
                score: n.FusedScore,
            })) ?? [],
            timing: result.Timing,
        });
    }
    async handleTrace(symbol) {
        const repoSlug = await this.getRepoSlug();
        const result = await this.api.trace(symbol, repoSlug);
        const se = result.StructuredExplanation;
        this.view.webview.postMessage({
            type: 'traceResult',
            symbol,
            structured: se ? {
                overview: se.overview,
                flow_steps: se.flow_steps ?? [],
                key_insights: se.key_insights ?? [],
            } : null,
            explanation: result.Explanation,
            direction: result.Direction,
            root: result.Root ? { name: result.Root.Qualified, kind: result.Root.Kind, filePath: result.Root.FilePath, startLine: result.Root.StartLine } : null,
            hops: result.Tree?.length ?? 0,
            timing: result.Timing,
        });
    }
    async handleBlast(symbol) {
        const repoSlug = await this.getRepoSlug();
        const result = await this.api.blast(symbol, repoSlug);
        const se = result.StructuredSummary;
        this.view.webview.postMessage({
            type: 'blastResult',
            symbol,
            structured: se ? {
                overview: se.overview,
                severity: se.severity,
                risk_areas: se.risk_areas ?? [],
                migration_steps: se.migration_steps ?? [],
            } : null,
            summary: result.Summary,
            target: result.Target ? { name: result.Target.Qualified, kind: result.Target.Kind, filePath: result.Target.FilePath, startLine: result.Target.StartLine } : null,
            affected: result.Affected?.map((a) => ({
                name: a.Node.Qualified,
                kind: a.Node.Kind,
                filePath: a.Path,
                startLine: a.Node.StartLine,
                depth: a.HopCount,
            })) ?? [],
            timing: result.Timing,
        });
    }
    getHtml() {
        return `<!DOCTYPE html>
<html>
<head>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: var(--vscode-font-family);
    font-size: var(--vscode-font-size);
    color: var(--vscode-foreground);
    background: var(--vscode-sideBar-background);
    display: flex;
    flex-direction: column;
    height: 100vh;
  }
  .messages { flex: 1; overflow-y: auto; padding: 12px; }
  .msg { margin-bottom: 14px; animation: fadeIn 0.2s ease; }
  @keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; } }
  .msg-role {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--vscode-descriptionForeground);
    margin-bottom: 3px;
    font-weight: 600;
  }
  .msg-user {
    background: var(--vscode-input-background);
    border: 1px solid var(--vscode-input-border);
    border-radius: 8px;
    padding: 8px 10px;
    font-size: 13px;
    line-height: 1.5;
  }
  .msg-assistant {
    font-size: 13px;
    line-height: 1.6;
    white-space: pre-wrap;
    padding: 2px 0;
  }
  .result-card {
    background: var(--vscode-editor-background);
    border: 1px solid var(--vscode-widget-border);
    border-radius: 6px;
    padding: 6px 8px;
    margin: 4px 0;
    cursor: pointer;
    font-size: 12px;
    transition: background 0.15s;
  }
  .result-card:hover { background: var(--vscode-list-hoverBackground); }
  .result-name { font-weight: 600; }
  .result-path { color: var(--vscode-descriptionForeground); font-size: 11px; margin-top: 1px; }
  .result-score { float: right; color: var(--vscode-focusBorder); font-size: 11px; font-weight: 600; }
  .input-area { padding: 8px 12px; border-top: 1px solid var(--vscode-widget-border); }
  .input-wrap {
    display: flex; gap: 6px; align-items: flex-end;
    background: var(--vscode-input-background);
    border: 1px solid var(--vscode-input-border);
    border-radius: 8px;
    padding: 6px 8px;
    transition: border-color 0.15s;
  }
  .input-wrap:focus-within { border-color: var(--vscode-focusBorder); }
  textarea {
    flex: 1; background: transparent; color: var(--vscode-input-foreground);
    border: none; font-family: inherit; font-size: 13px;
    resize: none; min-height: 20px; max-height: 120px; outline: none;
  }
  button {
    background: var(--vscode-button-background); color: var(--vscode-button-foreground);
    border: none; border-radius: 6px; padding: 5px 10px;
    cursor: pointer; font-size: 12px; font-weight: 500;
  }
  button:hover { background: var(--vscode-button-hoverBackground); }
  button:disabled { opacity: 0.4; cursor: default; }
  .empty {
    color: var(--vscode-descriptionForeground);
    text-align: center; padding: 32px 16px; font-size: 13px; line-height: 1.8;
  }
  .empty code {
    display: inline-block; background: var(--vscode-textCodeBlock-background);
    padding: 1px 5px; border-radius: 3px; font-size: 12px;
  }
  .loading { color: var(--vscode-descriptionForeground); font-size: 12px; }
  .loading::after { content: '...'; animation: dots 1s steps(3) infinite; }
  @keyframes dots { 0% { content: '.'; } 33% { content: '..'; } 66% { content: '...'; } }
  .kind-badge {
    display: inline-block; font-size: 9px; font-weight: 700;
    text-transform: uppercase; padding: 1px 4px; border-radius: 3px; margin-right: 4px;
  }
  .kind-function { background: rgba(56,189,248,0.15); color: #38bdf8; }
  .kind-class { background: rgba(167,139,250,0.15); color: #a78bfa; }
  .kind-file { background: rgba(52,211,153,0.15); color: #34d399; }
  .kind-module { background: rgba(251,146,60,0.15); color: #fb923c; }
  .timing { font-size: 11px; color: var(--vscode-descriptionForeground); margin-top: 6px; }
</style>
</head>
<body>
  <div class="messages" id="messages">
    <div class="empty">
      Ask about your codebase<br><br>
      <code>Where is authentication handled?</code><br>
      <code>What functions call db.Query?</code>
    </div>
  </div>
  <div class="input-area">
    <div class="input-wrap">
      <textarea id="input" placeholder="Ask about your code..." rows="1"></textarea>
      <button id="send">Ask</button>
    </div>
  </div>

  <script>
    const vscode = acquireVsCodeApi();
    const messages = document.getElementById('messages');
    const input = document.getElementById('input');
    const send = document.getElementById('send');
    let hasMessages = false;

    function addMsg(role, html) {
      if (!hasMessages) { messages.innerHTML = ''; hasMessages = true; }
      const div = document.createElement('div');
      div.className = 'msg';
      div.innerHTML = '<div class="msg-role">' + role + '</div><div class="msg-' + role + '">' + html + '</div>';
      messages.appendChild(div);
      messages.scrollTop = messages.scrollHeight;
    }

    function kindBadge(kind) {
      const label = kind === 'function' ? 'fn' : kind === 'class' ? 'cls' : kind;
      return '<span class="kind-badge kind-' + kind + '">' + label + '</span>';
    }

    function esc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

    send.onclick = () => {
      const text = input.value.trim();
      if (!text) return;
      addMsg('user', esc(text));
      vscode.postMessage({ type: 'query', text });
      input.value = '';
      send.disabled = true;
    };

    input.onkeydown = (e) => {
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send.click(); }
    };

    function clearLoading() {
      const last = messages.lastElementChild;
      if (last && last.querySelector('.loading')) { last.remove(); }
    }

    function renderNodes(nodes) {
      let html = '';
      nodes.forEach(function(n) {
        html += '<div class="result-card" onclick="goTo(\\'' + n.filePath.replace(/'/g,"\\\\'") + '\\', ' + n.startLine + ')">';
        if (n.score !== undefined) html += '<span class="result-score">' + n.score.toFixed(2) + '</span>';
        if (n.depth !== undefined) html += '<span class="result-score">depth ' + n.depth + '</span>';
        html += kindBadge(n.kind) + '<span class="result-name">' + esc(n.name) + '</span>';
        html += '<div class="result-path">' + esc(n.filePath) + ':' + n.startLine + '</div>';
        html += '</div>';
      });
      return html;
    }

    function renderStructuredQuery(msg) {
      let html = '';
      const s = msg.structured;
      if (s) {
        html += '<div style="font-weight:600;margin-bottom:8px">' + esc(s.overview) + '</div>';
        if (s.evidence && s.evidence.length) {
          s.evidence.forEach(function(e) {
            html += '<div class="result-card" onclick="goTo(\\'' + (e.file||'').replace(/'/g,"\\\\'") + '\\', 0)">';
            html += '<span class="result-name">' + esc(e.function) + '</span>';
            if (e.file) html += '<div class="result-path">' + esc(e.file) + (e.lines ? ':' + e.lines : '') + '</div>';
            html += '<div style="margin-top:4px;font-size:12px">' + esc(e.description) + '</div>';
            html += '</div>';
          });
        }
        if (s.insights && s.insights.length) {
          html += '<div style="margin-top:8px;font-size:12px;color:var(--vscode-descriptionForeground)">';
          s.insights.forEach(function(i) { html += '• ' + esc(i) + '<br>'; });
          html += '</div>';
        }
      } else if (msg.explanation) {
        html += esc(msg.explanation);
      }
      if (msg.nodes && msg.nodes.length) html += renderNodes(msg.nodes);
      if (msg.timing) html += '<div class="timing">' + msg.timing.TotalMS + 'ms</div>';
      return html;
    }

    function renderStructuredTrace(msg) {
      let html = '';
      const s = msg.structured;
      html += '<div style="font-size:11px;color:var(--vscode-focusBorder);margin-bottom:4px">TRACE ' + esc(msg.direction || 'forward') + '</div>';
      if (s) {
        html += '<div style="font-weight:600;margin-bottom:8px">' + esc(s.overview) + '</div>';
        if (s.flow_steps && s.flow_steps.length) {
          s.flow_steps.forEach(function(step) {
            html += '<div style="display:flex;gap:6px;margin:4px 0;font-size:12px">';
            html += '<span style="color:var(--vscode-focusBorder);font-weight:600;min-width:20px">' + step.hop + '.</span>';
            html += '<div>' + kindBadge('function') + '<strong>' + esc(step.function) + '</strong>';
            html += '<div style="color:var(--vscode-descriptionForeground)">' + esc(step.action) + '</div>';
            if (step.data_changes) html += '<div style="font-size:11px;color:var(--vscode-descriptionForeground);font-style:italic">↳ ' + esc(step.data_changes) + '</div>';
            html += '</div></div>';
          });
        }
        if (s.key_insights && s.key_insights.length) {
          html += '<div style="margin-top:8px;font-size:12px;color:var(--vscode-descriptionForeground)">';
          s.key_insights.forEach(function(i) { html += '• ' + esc(i) + '<br>'; });
          html += '</div>';
        }
      } else if (msg.explanation) {
        html += esc(msg.explanation);
      }
      if (msg.timing) html += '<div class="timing">' + msg.hops + ' hops · ' + msg.timing.TotalMS + 'ms</div>';
      return html;
    }

    function renderStructuredBlast(msg) {
      let html = '';
      const s = msg.structured;
      if (s) {
        const sevColors = { low: '#34d399', medium: '#fcd34d', high: '#f97316', critical: '#ef4444' };
        const sevColor = sevColors[s.severity] || '#888';
        html += '<div style="display:flex;gap:6px;align-items:center;margin-bottom:8px">';
        html += '<span style="background:' + sevColor + ';color:#000;font-size:10px;font-weight:700;padding:2px 6px;border-radius:3px;text-transform:uppercase">' + esc(s.severity) + '</span>';
        html += '<span style="font-weight:600">' + esc(s.overview) + '</span>';
        html += '</div>';
        if (s.risk_areas && s.risk_areas.length) {
          s.risk_areas.forEach(function(r) {
            html += '<div class="result-card" onclick="goTo(\\'' + (r.file||'').replace(/'/g,"\\\\'") + '\\', 0)">';
            html += kindBadge('function') + '<span class="result-name">' + esc(r.function) + '</span>';
            if (r.file) html += '<div class="result-path">' + esc(r.file) + '</div>';
            html += '<div style="margin-top:4px;font-size:12px;color:#f97316">⚠ ' + esc(r.risk) + '</div>';
            if (r.mitigation) html += '<div style="font-size:11px;color:var(--vscode-descriptionForeground)">→ ' + esc(r.mitigation) + '</div>';
            html += '</div>';
          });
        }
        if (s.migration_steps && s.migration_steps.length) {
          html += '<div style="margin-top:8px;font-size:12px"><strong>Migration order:</strong></div>';
          s.migration_steps.forEach(function(step, i) {
            html += '<div style="font-size:12px;color:var(--vscode-descriptionForeground)">' + (i+1) + '. ' + esc(step) + '</div>';
          });
        }
      } else if (msg.summary) {
        html += esc(msg.summary);
      }
      if (msg.affected && msg.affected.length) html += renderNodes(msg.affected);
      if (msg.timing) html += '<div class="timing">' + (msg.affected ? msg.affected.length : 0) + ' affected · ' + msg.timing.TotalMS + 'ms</div>';
      return html;
    }

    window.addEventListener('message', (e) => {
      const msg = e.data;
      if (msg.type === 'loading' && msg.value) {
        addMsg('assistant', '<span class="loading">Analyzing</span>');
      }
      if (msg.type === 'queryResult') {
        clearLoading();
        addMsg('assistant', renderStructuredQuery(msg));
        send.disabled = false;
      }
      if (msg.type === 'traceResult') {
        clearLoading();
        addMsg('assistant', renderStructuredTrace(msg));
        send.disabled = false;
      }
      if (msg.type === 'blastResult') {
        clearLoading();
        addMsg('assistant', renderStructuredBlast(msg));
        send.disabled = false;
      }
      if (msg.type === 'error') {
        clearLoading();
        addMsg('assistant', 'Error: ' + esc(msg.message));
        send.disabled = false;
      }
    });

    window.goTo = function(path, line) {
      vscode.postMessage({ type: 'openFile', path: path, line: line });
    };
  </script>
</body>
</html>`;
    }
}
exports.ChatPanelProvider = ChatPanelProvider;
//# sourceMappingURL=chatPanel.js.map