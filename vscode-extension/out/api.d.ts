/** Typed API client for the commit0 HTTP server. */
export declare class Commit0Api {
    private baseUrl;
    constructor();
    private request;
    listRepos(): Promise<Repo[]>;
    createRepo(slug: string, path: string, languages: string[]): Promise<Repo>;
    deleteRepo(slug: string): Promise<void>;
    query(question: string, repoSlug: string, topK?: number): Promise<QueryResult>;
    trace(symbol: string, repoSlug: string, direction?: string, depth?: number): Promise<TraceResult>;
    blast(symbol: string, repoSlug: string, maxDepth?: number): Promise<BlastResult>;
    nodeLookup(repo: string, qualified: string): Promise<CodeNode>;
    nodesByFile(repo: string, path: string): Promise<CodeNode[]>;
    neighborhood(nodeId: string): Promise<Neighborhood>;
    indexProject(repoSlug: string, repoPath: string): Promise<{
        job_id: string;
    }>;
    indexJobStatus(jobId: string): Promise<IndexJob>;
    health(): Promise<boolean>;
}
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
