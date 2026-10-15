import axios from "axios";
import type {ContractError} from './contracts';
import type { Account, LoginSession, OperationRecord, QueueOverview, QueueSettings, AttemptResult, Dashboard, Finding, FindingSummary, Project, ScanCompare, ScanAttempt, ScanLogEvent, ScanTask, User } from "./types";

export interface SnapshotInfo {
  id: number;
  originalFilename: string;
  archiveSha256: string;
  contentHash: string;
  snapshotPath: string;
  fileCount: number;
  totalSize: number;
  projectId: number;
  scanCount: number;
  createdAt: string;
}

export interface ModelConfig {
  id: number;
  name: string;
  provider: string;
  baseUrl: string;
  model: string;
  status: string;
  compatibilityStatus: string;
  enabled: boolean;
  remark: string;
  websiteUrl: string;
  /** 上游协议覆盖；空 = 用预设默认值 */
  wireApi: string;
  /** 已存 Key 的掩码提示（sk-…尾4位）；空 = 未配置 Key。仅列表接口瞬态返回 */
  apiKeyHint?: string;
  createdAt: string;
  updatedAt: string;
}

/** 模型供应商预设（注册表驱动，见 GET /model-providers；字段仅作表单提示，实际值用户填写） */
export interface ProviderPreset {
  key: string;
  label: string;
  baseUrl: string;
  credentialEnv: string;
  wireApi: string;
  icon: string;
  iconColor: string;
  /** 官网 / 获取 Key / 拉取模型列表 端点 */
  website: string;
  keyUrl: string;
  modelsUrl: string;
  defaultModel: string;
  /** 模型字段示例文案（如 "qwen-plus、qwen-turbo"），仅作输入提示 */
  modelExamples?: string;
  models: string[];
}

/** 模型配置的写载荷：API Key 留空 = 不修改（仅 update） */
export interface ModelConfigPayload {
  name: string;
  provider: string;
  baseUrl: string;
  model: string;
  apiKey?: string;
  remark: string;
  websiteUrl: string;
  wireApi: string;
}

export const TOKEN_KEY = "axiom_token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
  window.dispatchEvent(new Event('axiom:auth-changed'));
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem('axiom_user');
  window.dispatchEvent(new Event('axiom:auth-changed'));
}

export class ApiError extends Error {
  constructor(message: string, readonly status = 0, readonly retryAfter = 0) { super(message); }
}

const http = axios.create({
  baseURL: "/api/v1",
  timeout: 20_000,
});

http.interceptors.request.use((config) => {
  const token = getToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

http.interceptors.response.use(
  (res) => res.data?.data ?? res.data,
  async (err) => {
    let payload=err.response?.data;
    if(payload instanceof Blob && payload.type.includes('json')){try{payload=JSON.parse(await payload.text())}catch{}}
    const message = payload?.message ?? err.message ?? "请求失败";
    const status = err.response?.status ?? 0;
    // A late response from a previous session must not log out a newer session.
    if (status === 401 && !err.config?.url?.includes('/auth/login') && err.config?.headers?.Authorization === `Bearer ${getToken()}`) {
      clearToken();
      window.dispatchEvent(new Event("axiom:session-expired"));
    }
    return Promise.reject(new ApiError(message, status, Number(err.response?.headers?.['retry-after']) || 0));
  },
);

export interface CreateScanInput {
    deepMaxDiscoveryRuns?: number;
    deepStopAfterNoNew?: number;
    sourcePath?: string;
    sourceSnapshotId?: number;
    modelConfigId?: number;
    projectId?: number;
    provider?: string;
    model?: string;
    baseUrl?: string;
    scanMode?: string;
    includePaths?: string[];
    scanFocus?: string[];
    validationMode?: "default" | "exploitability";
    businessContext?: string;
    timeoutMinutes?: number;

    maxCostUsd?: number | null;
    maxTotalCostUsd?: number | null;
    workers?: number | null;

}

export const api = {
 async storage():Promise<import('./contracts').StorageSnapshot>{return http.get('/storage') as Promise<import('./contracts').StorageSnapshot>},
 async updateStoragePolicy(policy:import('./contracts').StoragePolicy):Promise<import('./contracts').StorageSnapshot>{return http.put('/storage/policy',policy) as Promise<import('./contracts').StorageSnapshot>},
 async cleanupJobs():Promise<import('./contracts').CleanupJob[]>{return http.get('/storage/cleanup') as Promise<import('./contracts').CleanupJob[]>},
 async cleanupJob(id:string):Promise<import('./contracts').CleanupJob>{return http.get(`/storage/cleanup/${encodeURIComponent(id)}`) as Promise<import('./contracts').CleanupJob>},
 async previewCleanup(kind:'orphans'|'scan'|'snapshot',resourceId?:number):Promise<import('./contracts').CleanupJob>{return http.post('/storage/cleanup',{kind,...(resourceId==null?{}:{resourceId})}) as Promise<import('./contracts').CleanupJob>},
 async cancelCleanup(id:string,revision:number):Promise<import('./contracts').CleanupJob>{return http.post(`/storage/cleanup/${encodeURIComponent(id)}/cancel`,{revision}) as Promise<import('./contracts').CleanupJob>},
 async executeCleanup(id:string,revision:number):Promise<import('./contracts').CleanupJob>{return http.post(`/storage/cleanup/${encodeURIComponent(id)}/execute`,{revision}) as Promise<import('./contracts').CleanupJob>},
  async taskBudget(id:number):Promise<import('./contracts').TaskBudgetSnapshot>{return http.get(`/scans/${id}/budget`) as Promise<import('./contracts').TaskBudgetSnapshot>;},
  async updateTaskBudget(id:number,maxTotalCostUsd:number,revision:number):Promise<import('./contracts').TaskBudgetSnapshot>{return http.put(`/scans/${id}/budget`,{maxTotalCostUsd,revision}) as Promise<import('./contracts').TaskBudgetSnapshot>;},
  async reconcileTaskCost(id:number,attempt:number,estimatedUsd:number,note:string,revision:number):Promise<import('./contracts').TaskBudgetSnapshot>{return http.put(`/scans/${id}/budget/attempts/${attempt}`,{estimatedUsd,note,revision}) as Promise<import('./contracts').TaskBudgetSnapshot>;},
  async login(username: string, password: string): Promise<{ token: string; user: User }> {
    const data = (await http.post("/auth/login", { username, password })) as {
      token: string;
      user: User;
    };
    localStorage.setItem("axiom_user", data.user.username);
    return data;
  },
  async me(): Promise<User> {
    return http.get("/auth/me") as Promise<User>;
  },
  async logout(): Promise<{revoked: boolean}> { return http.post('/auth/logout') as Promise<{revoked: boolean}>; },
  async listSessions(): Promise<LoginSession[]> { return http.get('/auth/sessions') as Promise<LoginSession[]>; },
  async revokeSession(id: string): Promise<{revoked: boolean}> { return http.delete(`/auth/sessions/${encodeURIComponent(id)}`) as Promise<{revoked: boolean}>; },
  async listAccounts(): Promise<Account[]> { return http.get('/users') as Promise<Account[]>; },
  async createAccount(username: string, password: string, role: User['role']): Promise<User> { return http.post('/users', {username, password, role}) as Promise<User>; },
  async updateAccount(id: number, role: User['role'], active: boolean): Promise<User> { return http.put(`/users/${id}`, {role, active}) as Promise<User>; },
  async listOperations(before = ''): Promise<{items: OperationRecord[]; nextCursor: string}> { return http.get('/operations', {params: {before}}) as Promise<{items: OperationRecord[]; nextCursor: string}>; },
  async listProjects(): Promise<Project[]> {
    return http.get("/projects") as Promise<Project[]>;
  },
  async createProject(name: string): Promise<Project> {
    return http.post("/projects", { name }) as Promise<Project>;
  },
  async deleteProject(id: number): Promise<{ id: number }> {
    return http.delete(`/projects/${id}`) as Promise<{ id: number }>;
  },
  async getProject(id: number): Promise<Project> {
    return http.get(`/projects/${id}`) as Promise<Project>;
  },
  async scanCheckpoints(id:number,attempt:number):Promise<Blob> { return http.get(`/scans/${id}/checkpoints`,{params:{attempt},responseType:'blob'}) as Promise<Blob>; },
  async scanCoverage(id:number,attempt:number):Promise<import("./types").CoverageResponse> { return http.get(`/scans/${id}/coverage`,{params:{attempt}}) as Promise<import("./types").CoverageResponse>; },
  async createScan(payload: CreateScanInput): Promise<ScanTask> {
    return http.post("/scans", payload) as Promise<ScanTask>;
  },
  async listScans(projectId?: number): Promise<ScanTask[]> {
    const q = projectId ? `?projectId=${projectId}` : "";
    return http.get(`/scans${q}`) as Promise<ScanTask[]>;
  },
  async scanPage(page: number, keyword: string, status: string): Promise<{items: ScanTask[];total:number;page:number;counts:Record<string,number>}> {
    return http.get('/scans', {params:{page,pageSize:50,keyword,status}}) as Promise<{items:ScanTask[];total:number;page:number;counts:Record<string,number>}>;
  },
  async getScan(id: number): Promise<ScanTask> {
    return http.get(`/scans/${id}`) as Promise<ScanTask>;
  },
  async listFindings(scanId: number): Promise<Finding[]> {
    return http.get(`/scans/${scanId}/findings`) as Promise<Finding[]>;
  },
  async queueOverview(taskId?: number): Promise<QueueOverview> { return http.get('/executions/overview', { params: { taskId } }) as Promise<QueueOverview>; },
  async executionSettings(): Promise<{ settings: QueueSettings; canEdit: boolean; history: QueueSettings[] }> { return http.get('/settings/execution') as Promise<{ settings: QueueSettings; canEdit: boolean; history: QueueSettings[] }>; },
  async saveExecutionSettings(v: QueueSettings): Promise<QueueSettings> { return http.put('/settings/execution', {global:v.global,perModel:v.perModel,perProject:v.perProject,revision:v.revision}) as Promise<QueueSettings>; },
  async resultArtifact(id:number,attempt:number,path:string):Promise<Blob>{return http.get(`/scans/${id}/attempts/${attempt}/artifacts`,{params:{path},responseType:'blob'}) as Promise<Blob>;},
  async attemptResult(id: number, attempt: number): Promise<AttemptResult> { return http.get(`/scans/${id}/attempts/${attempt}/result`) as Promise<AttemptResult>; },
  async scanAttempts(scanId: number): Promise<ScanAttempt[]> { return http.get(`/scans/${scanId}/attempts`) as Promise<ScanAttempt[]>; },
  async scanLogs(scanId: number, after: number, attempt?: number): Promise<{ events: ScanLogEvent[]; nextAfter: number; total: number; hasMore?: boolean;format?:string;invalidRecords?:number;diagnostic?:ContractError;recordCount?:number }> {
    return http.get(`/scans/${scanId}/logs?after=${after}${attempt ? `&attempt=${attempt}` : ""}`) as Promise<{
      events: ScanLogEvent[];
      nextAfter: number;
      total: number;
    }>;
  },
  async exportReport(scanId: number): Promise<Blob> {
    return http.get(`/scans/${scanId}/report`, { responseType: "blob" }) as Promise<Blob>;
  },
  async exportReportPdf(scanId: number): Promise<Blob> {
    return http.get(`/scans/${scanId}/report.pdf`, { responseType: "blob", timeout: 120_000 }) as Promise<Blob>;
  },
  async cancelScan(scanId: number): Promise<ScanTask> {
    return http.post(`/scans/${scanId}/cancel`) as Promise<ScanTask>;
  },
  async retryScan(scanId: number): Promise<ScanTask> {
    return http.post(`/scans/${scanId}/retry`) as Promise<ScanTask>;
  },
  async deleteScan(scanId: number): Promise<{ id: number }> {
    return http.delete(`/scans/${scanId}`) as Promise<{ id: number }>;
  },
  async uploadSnapshot(file: File, projectId?: number): Promise<SnapshotInfo> {
    const form = new FormData();
    form.append("file", file);
    if (projectId) form.append("projectId", String(projectId));
    return http.post("/uploads", form, { timeout: 600_000 }) as Promise<SnapshotInfo>;
  },
  async listSnapshots(projectId?: number): Promise<SnapshotInfo[]> {
    const q = projectId ? `?projectId=${projectId}` : "";
    return http.get(`/snapshots${q}`) as Promise<SnapshotInfo[]>;
  },
  async deleteSnapshot(id: number): Promise<{ id: number }> {
    return http.delete(`/snapshots/${id}`) as Promise<{ id: number }>;
  },
  async assignSnapshotProject(id: number, projectId: number): Promise<{ id: number; projectId: number }> {
    return http.put(`/snapshots/${id}/project`, { projectId }) as Promise<{ id: number; projectId: number }>;
  },
  async listModelConfigs(): Promise<ModelConfig[]> {
    return http.get("/model-configs") as Promise<ModelConfig[]>;
  },
  async listModelProviders(): Promise<ProviderPreset[]> {
    return http.get("/model-providers") as Promise<ProviderPreset[]>;
  },
  async enableModelConfig(id: number, enabled: boolean): Promise<ModelConfig> {
    return http.put(`/model-configs/${id}/enable`, { enabled }) as Promise<ModelConfig>;
  },
  async createModelConfig(payload: ModelConfigPayload): Promise<ModelConfig> {
    return http.post("/model-configs", payload) as Promise<ModelConfig>;
  },
  async updateModelConfig(id: number, payload: ModelConfigPayload): Promise<ModelConfig> {
    return http.put(`/model-configs/${id}`, payload) as Promise<ModelConfig>;
  },
  async deleteModelConfig(id: number): Promise<{ id: number }> {
    return http.delete(`/model-configs/${id}`) as Promise<{ id: number }>;
  },
  async testModelConfig(id: number): Promise<{ ok: boolean; status?: number; error?: string }> {
    return http.post(`/model-configs/${id}/test`) as Promise<{ ok: boolean; status?: number; error?: string }>;
  },
  /** 从供应商拉取模型清单（cc-switch「获取模型列表」）；编辑场景可传 modelConfigId 用已存 key */
  async fetchProviderModels(payload: {
    provider: string;
    baseUrl: string;
    apiKey?: string;
    modelConfigId?: number;
  }): Promise<{ ok: boolean; models?: string[]; status?: number; error?: string }> {
    return http.post("/model-configs/fetch-models", payload, { timeout: 30_000 }) as Promise<{
      ok: boolean;
      models?: string[];
      status?: number;
      error?: string;
    }>;
  },
  async listFindingsPool(filters: { severity?: string; status?: string; q?: string }): Promise<FindingSummary[]> {
    const params = new URLSearchParams();
    if (filters.severity) params.set("severity", filters.severity);
    if (filters.status) params.set("status", filters.status);
    if (filters.q) params.set("q", filters.q);
    return http.get(`/findings?${params.toString()}`) as Promise<FindingSummary[]>;
  },
  async setFindingStatus(findingId: number, status: string): Promise<{ id: number; status: string }> {
    return http.put(`/findings/${findingId}/status`, { status }) as Promise<{
      id: number;
      status: string;
    }>;
  },
  async compareScan(scanId: number): Promise<ScanCompare> {
    return http.get(`/scans/${scanId}/compare`) as Promise<ScanCompare>;
  },
  async dashboard(): Promise<Dashboard> {
    return http.get("/dashboard") as Promise<Dashboard>;
  },
  async changePassword(oldPassword: string, newPassword: string): Promise<{ id: number }> {
    return http.put("/user/password", { oldPassword, newPassword }) as Promise<{ id: number }>;
  },
};
