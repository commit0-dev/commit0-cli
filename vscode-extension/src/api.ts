import * as vscode from 'vscode';

/** Typed API client for the commit0 HTTP server. */
export class Commit0Api {
  private baseUrl: string;

  constructor() {
    this.baseUrl = vscode.workspace.getConfiguration('commit0').get('serverUrl', 'http://localhost:8080');
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const opts: RequestInit = {
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
    return res.json() as Promise<T>;
  }

  // ---- Repos ----

  async listRepos(): Promise<Repo[]> {
    return this.request<Repo[]>('GET', '/api/v1/repos');
  }

  async createRepo(slug: string, path: string, languages: string[]): Promise<Repo> {
    return this.request<Repo>('POST', '/api/v1/repos', { slug, path, languages });
  }

  async deleteRepo(slug: string): Promise<void> {
    await this.request('DELETE', `/api/v1/repos/${slug}`);
  }

  // ---- Query ----

  async query(question: string, repoSlug: string, topK = 10): Promise<QueryResult> {
    return this.request<QueryResult>('POST', '/api/v1/query', {
      question, repo_slug: repoSlug, top_k: topK,
    });
  }

  // ---- Trace ----

  async trace(symbol: string, repoSlug: string, direction = 'forward', depth = 5): Promise<TraceResult> {
    return this.request<TraceResult>('POST', '/api/v1/trace/json', {
      symbol, repo_slug: repoSlug, direction, depth,
    });
  }

  // ---- Blast ----

  async blast(symbol: string, repoSlug: string, maxDepth = 3): Promise<BlastResult> {
    return this.request<BlastResult>('POST', '/api/v1/blast', {
      symbol, repo_slug: repoSlug, max_depth: maxDepth,
    });
  }

  // ---- Nodes ----

  async nodeLookup(repo: string, qualified: string): Promise<CodeNode> {
    return this.request<CodeNode>('GET', `/api/v1/nodes/lookup?repo=${encodeURIComponent(repo)}&qualified=${encodeURIComponent(qualified)}`);
  }

  async nodesByFile(repo: string, path: string): Promise<CodeNode[]> {
    return this.request<CodeNode[]>('GET', `/api/v1/nodes/by-file?repo=${encodeURIComponent(repo)}&path=${encodeURIComponent(path)}`);
  }

  async neighborhood(nodeId: string): Promise<Neighborhood> {
    return this.request<Neighborhood>('GET', `/api/v1/nodes/${encodeURIComponent(nodeId)}/neighborhood`);
  }

  // ---- Index ----

  async indexProject(repoSlug: string, repoPath: string): Promise<{ job_id: string }> {
    return this.request<{ job_id: string }>('POST', '/api/v1/index', {
      repo_slug: repoSlug, repo_path: repoPath,
    });
  }

  async indexJobStatus(jobId: string): Promise<IndexJob> {
    return this.request<IndexJob>('GET', `/api/v1/index/${encodeURIComponent(jobId)}`);
  }

  // ---- Health ----

  async health(): Promise<boolean> {
    try {
      await this.request<{ status: string }>('GET', '/health');
      return true;
    } catch {
      return false;
    }
  }
}

// ---- Types matching Go structs ----

export interface Repo {
  Slug: string;
  Path: string;
  RemoteURL: string;
  Languages: string[];
  CreatedAt: string;
  LastIndexedAt: string | null;
}

export interface CodeNode {
  ID: string;
  Name: string;
  Qualified: string;
  Kind: 'function' | 'class' | 'file' | 'module';
  FilePath: string;
  RepoSlug: string;
  Signature: string;
  StartLine: number;
  EndLine: number;
  Visibility: string;
  Language: string;
}

export interface ScoredNode {
  Node: CodeNode;
  VectorScore: number;
  FTSScore: number;
  FusedScore: number;
  Centrality: number;
}

export interface QueryResult {
  Explanation: string;
  StructuredExplanation?: SearchExplanation;
  Query: string;
  RepoSlug: string;
  Nodes: ScoredNode[];
  Timing: TimingInfo;
}

export interface SearchExplanation {
  overview: string;
  evidence: EvidenceItem[];
  insights: string[];
}

export interface EvidenceItem {
  function: string;
  file: string;
  lines: string;
  description: string;
  relevance: string;
}

export interface TraceHop {
  Node: CodeNode;
  Edge: CodeEdge;
  Children: TraceHop[];
  Depth: number;
}

export interface TraceResult {
  Direction: string;
  Explanation: string;
  StructuredExplanation?: TraceExplanation;
  Tree: TraceHop[];
  Root: CodeNode;
  Timing: TimingInfo;
}

export interface TraceExplanation {
  overview: string;
  flow_steps: FlowStep[];
  key_insights: string[];
}

export interface FlowStep {
  hop: number;
  function: string;
  action: string;
  data_changes: string;
}

export interface BlastResult {
  Summary: string;
  StructuredSummary?: BlastExplanation;
  Affected: AffectedNode[];
  Target: CodeNode;
  Timing: TimingInfo;
}

export interface BlastExplanation {
  overview: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  risk_areas: RiskArea[];
  migration_steps: string[];
}

export interface RiskArea {
  function: string;
  file: string;
  risk: string;
  mitigation: string;
}

export interface AffectedNode {
  Module: string;
  Path: string;
  Node: CodeNode;
  HopCount: number;
}

export interface CodeEdge {
  Kind: string;
  FromID: string;
  ToID: string;
  CallSite: string;
  CallType: string;
}

export interface NeighborNode {
  Qualified: string;
  Signature: string;
  Docstring: string;
  FilePath: string;
  StartLine: number;
}

export interface Neighborhood {
  Callers: NeighborNode[];
  Callees: NeighborNode[];
  DataSinks: NeighborNode[];
  DataSources: NeighborNode[];
  Reads: string[];
  Writes: string[];
}

export interface IndexJob {
  id: string;
  status: 'indexing' | 'completed' | 'failed';
  repo_slug: string;
  files_indexed: number;
  nodes_created: number;
  errors: number;
  error?: string;
  started_at: string;
  finished_at?: string;
}

export interface TimingInfo {
  EmbedMS: number;
  SearchMS: number;
  GraphMS: number;
  ExplainMS: number;
  TotalMS: number;
}
