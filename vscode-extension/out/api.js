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
exports.Commit0Api = void 0;
const vscode = __importStar(require("vscode"));
/** Typed API client for the commit0 HTTP server. */
class Commit0Api {
    baseUrl;
    constructor() {
        this.baseUrl = vscode.workspace.getConfiguration('commit0').get('serverUrl', 'http://localhost:8080');
    }
    async request(method, path, body) {
        const url = `${this.baseUrl}${path}`;
        const opts = {
            method,
            headers: { 'Content-Type': 'application/json' },
        };
        if (body) {
            opts.body = JSON.stringify(body);
        }
        const res = await fetch(url, opts);
        if (!res.ok) {
            const text = await res.text().catch(() => '');
            throw new Error(`commit0 API ${method} ${path}: ${res.status} ${text}`);
        }
        return res.json();
    }
    // ---- Repos ----
    async listRepos() {
        return this.request('GET', '/api/v1/repos');
    }
    async createRepo(slug, path, languages) {
        return this.request('POST', '/api/v1/repos', { slug, path, languages });
    }
    async deleteRepo(slug) {
        await this.request('DELETE', `/api/v1/repos/${slug}`);
    }
    // ---- Query ----
    async query(question, repoSlug, topK = 10) {
        return this.request('POST', '/api/v1/query', {
            question, repo_slug: repoSlug, top_k: topK,
        });
    }
    // ---- Trace ----
    async trace(symbol, repoSlug, direction = 'forward', depth = 5) {
        return this.request('POST', '/api/v1/trace/json', {
            symbol, repo_slug: repoSlug, direction, depth,
        });
    }
    // ---- Blast ----
    async blast(symbol, repoSlug, maxDepth = 3) {
        return this.request('POST', '/api/v1/blast', {
            symbol, repo_slug: repoSlug, max_depth: maxDepth,
        });
    }
    // ---- Nodes ----
    async nodeLookup(repo, qualified) {
        return this.request('GET', `/api/v1/nodes/lookup?repo=${encodeURIComponent(repo)}&qualified=${encodeURIComponent(qualified)}`);
    }
    async nodesByFile(repo, path) {
        return this.request('GET', `/api/v1/nodes/by-file?repo=${encodeURIComponent(repo)}&path=${encodeURIComponent(path)}`);
    }
    async neighborhood(nodeId) {
        return this.request('GET', `/api/v1/nodes/${encodeURIComponent(nodeId)}/neighborhood`);
    }
    // ---- Index ----
    async indexProject(repoSlug, repoPath) {
        return this.request('POST', '/api/v1/index', {
            repo_slug: repoSlug, repo_path: repoPath,
        });
    }
    async indexJobStatus(jobId) {
        return this.request('GET', `/api/v1/index/${encodeURIComponent(jobId)}`);
    }
    // ---- Health ----
    // ---- Field Flow ----
    async flowTrace(symbol, repoSlug, fieldPath = '', direction = 'both') {
        return this.request('POST', '/api/v1/flow', {
            symbol, repo_slug: repoSlug, field_path: fieldPath, direction, depth: 10, show_mutations: true,
        });
    }
    // ---- Temporal History ----
    async history(symbol, repoSlug) {
        return this.request('POST', '/api/v1/history', {
            symbol, repo_slug: repoSlug,
        });
    }
    // ---- Find Root Cause ----
    async findRoot(description, repoSlug) {
        return this.request('POST', '/api/v1/find-root', {
            description, repo_slug: repoSlug,
        });
    }
    // ---- Health ----
    async health() {
        try {
            await this.request('GET', '/health');
            return true;
        }
        catch {
            return false;
        }
    }
}
exports.Commit0Api = Commit0Api;
//# sourceMappingURL=api.js.map