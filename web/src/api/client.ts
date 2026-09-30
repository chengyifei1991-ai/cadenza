// 统一 fetch 客户端与类型化 API（契约清单见 docs/web-frontend-prd.md §3）。
import type {
  AuditAction,
  AuditLog,
  AuthMode,
  ChatMessage,
  ChatSession,
  Collector,
  ConfigVersion,
  GitCommit,
  GitStatus,
  Me,
  OpsStats,
  PageEnvelope,
  SessionSummary,
  Stats,
  SystemInfo,
  Task,
  TaskEventsResponse,
  TaskStatus,
  TaskType,
} from "./types";

const BASE = ""; // 同源部署；开发期由 Vite proxy 转发 /api。

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    method: opts.method ?? "GET",
    credentials: "same-origin",
    headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (!resp.ok) {
    let message = `请求失败（${resp.status}）`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* 非 JSON 响应 */
    }
    throw new ApiError(resp.status, message);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

export interface AuditExportParams {
  actor?: string;
  action?: AuditAction;
  subject?: string;
  from?: string;
  to?: string;
  format?: "csv" | "json";
}

/** 审计导出：返回下载文件名 + Blob（超限时抛带中文原因的 ApiError）。 */
export interface AuditExportFile {
  filename: string;
  blob: Blob;
}

async function requestBlob(path: string): Promise<AuditExportFile> {
  const resp = await fetch(`${BASE}${path}`, { method: "GET", credentials: "same-origin" });
  if (!resp.ok) {
    let message = `导出失败（${resp.status}）`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* 非 JSON 响应 */
    }
    throw new ApiError(resp.status, message);
  }
  const cd = resp.headers.get("Content-Disposition") ?? "";
  const m = /filename="([^"]+)"/.exec(cd);
  const ext = path.includes("format=json") ? "json" : "csv";
  return { filename: m?.[1] ?? `cadenza-audit.${ext}`, blob: await resp.blob() };
}

export interface ListParams {
  page?: number;
  page_size?: number;
}

export const api = {
  // 公开/系统
  systemInfo(): Promise<SystemInfo> {
    return request<SystemInfo>("/api/v1/system/info");
  },

  // 认证
  me(): Promise<Me> {
    return request<Me>("/api/v1/auth/me");
  },
  login(username: string, password: string): Promise<Me> {
    return request<Me>("/api/v1/auth/login", { method: "POST", body: { username, password } });
  },
  logout(): Promise<unknown> {
    return request("/api/v1/auth/logout", { method: "POST" });
  },

  // Collector
  listCollectors(params: ListParams = {}): Promise<PageEnvelope<Collector>> {
    const q = new URLSearchParams();
    if (params.page) q.set("page", String(params.page));
    if (params.page_size) q.set("page_size", String(params.page_size));
    return request<PageEnvelope<Collector>>(`/api/v1/collectors?${q}`);
  },
  getCollector(uid: string): Promise<Collector> {
    return request<Collector>(`/api/v1/collectors/${encodeURIComponent(uid)}`);
  },
  listVersions(uid: string, params: ListParams = {}): Promise<PageEnvelope<ConfigVersion>> {
    const q = new URLSearchParams();
    if (params.page) q.set("page", String(params.page));
    if (params.page_size) q.set("page_size", String(params.page_size));
    return request<PageEnvelope<ConfigVersion>>(
      `/api/v1/collectors/${encodeURIComponent(uid)}/versions?${q}`,
    );
  },

  // 任务
  listTasks(
    params: { status?: TaskStatus; type?: TaskType; target?: string; session_id?: string } & ListParams = {},
  ): Promise<PageEnvelope<Task>> {
    const q = new URLSearchParams();
    if (params.status) q.set("status", params.status);
    if (params.type) q.set("type", params.type);
    if (params.target) q.set("target", params.target);
    if (params.session_id) q.set("session_id", params.session_id);
    if (params.page) q.set("page", String(params.page));
    if (params.page_size) q.set("page_size", String(params.page_size));
    return request<PageEnvelope<Task>>(`/api/v1/tasks?${q}`);
  },
  getTask(id: string): Promise<Task> {
    return request<Task>(`/api/v1/tasks/${id}`);
  },
  /** 任务状态流转时间线（回放 task_events，按事件 id 升序）。 */
  taskEvents(id: string): Promise<TaskEventsResponse> {
    return request<TaskEventsResponse>(`/api/v1/tasks/${id}/events`);
  },
  /** 任务级 diff（服务端 unified diff；支持分组目标，无需单实例基准）。 */
  getTaskDiff(id: string): Promise<{
    task_id: string;
    base_yaml: string;
    generated_yaml: string;
    diff: string;
    has_base: boolean;
  }> {
    return request(`/api/v1/tasks/${encodeURIComponent(id)}/diff`);
  },
  approveTask(id: string): Promise<unknown> {
    return request(`/api/v1/tasks/${id}/approve`, { method: "POST", body: {} });
  },
  rejectTask(id: string, reason: string): Promise<unknown> {
    return request(`/api/v1/tasks/${id}/reject`, { method: "POST", body: { reason } });
  },
  rollbackTask(collectorInstanceUid: string, versionId: number, sessionId?: string): Promise<Task> {
    return request<Task>("/api/v1/tasks/rollback", {
      method: "POST",
      body: { collector_instance_uid: collectorInstanceUid, version_id: versionId, session_id: sessionId },
    });
  },
  /** GitOps：按 git 提交回退（内容取自该 commit 的文件）。 */
  rollbackToGitCommit(collectorInstanceUid: string, gitCommit: string): Promise<Task> {
    return request<Task>("/api/v1/tasks/rollback", {
      method: "POST",
      body: { collector_instance_uid: collectorInstanceUid, git_commit: gitCommit },
    });
  },
  /** GitOps：按 git 提交下发（服务端从仓库读取内容）。 */
  applyFromGitRef(collectorInstanceUid: string, gitRef: string, note?: string): Promise<Task> {
    return request<Task>("/api/v1/tasks/apply", {
      method: "POST",
      body: { collector_instance_uid: collectorInstanceUid, git_ref: gitRef, note },
    });
  },
  /** GitOps：仓库只读状态。 */
  getGitStatus(): Promise<GitStatus> {
    return request<GitStatus>("/api/v1/git/status");
  },
  /** GitOps：某 Collector 配置文件的历史提交。 */
  getGitCommits(instanceUid: string, limit = 20): Promise<{
    items: GitCommit[];
    total: number;
    path: string;
    ref: string;
  }> {
    const q = new URLSearchParams({ instance_uid: instanceUid, limit: String(limit) });
    return request(`/api/v1/git/commits?${q}`);
  },
  /** GitOps：读取指定 ref 的配置内容。 */
  getGitFile(instanceUid: string, ref?: string): Promise<{
    ref: string;
    git_commit: string;
    path: string;
    yaml: string;
  }> {
    const q = new URLSearchParams({ instance_uid: instanceUid });
    if (ref) q.set("ref", ref);
    return request(`/api/v1/git/file?${q}`);
  },
  applyTask(collectorInstanceUid: string, yaml: string, note?: string, sessionId?: string): Promise<Task> {
    return request<Task>("/api/v1/tasks/apply", {
      method: "POST",
      body: { collector_instance_uid: collectorInstanceUid, yaml, note, session_id: sessionId },
    });
  },

  // 审计 / 会话 / 统计
  /** 审计列表：支持服务端筛选（操作者/动作/对象/时间区间）。 */
  listAudit(
    params: {
      actor?: string;
      action?: AuditAction;
      subject?: string;
      /** 时间区间（RFC3339）；后端为秒级半开区间 [from, to+1s) */
      from?: string;
      to?: string;
    } & ListParams = {},
  ): Promise<PageEnvelope<AuditLog>> {
    const q = new URLSearchParams();
    if (params.actor) q.set("actor", params.actor);
    if (params.action) q.set("action", params.action);
    if (params.subject) q.set("subject", params.subject);
    if (params.from) q.set("from", params.from);
    if (params.to) q.set("to", params.to);
    if (params.page) q.set("page", String(params.page));
    if (params.page_size) q.set("page_size", String(params.page_size));
    return request<PageEnvelope<AuditLog>>(`/api/v1/audit?${q}`);
  },
  /**
   * 导出审计（1.2.0 C-b）：复用列表筛选，服务端一次性写出；超出行数上限返回 400。
   * 浏览器端拿到 Blob 后由调用方触发下载（避免直接导航到 URL 时把错误 JSON 当页面显示）。
   */
  exportAudit(params: AuditExportParams = {}): Promise<AuditExportFile> {
    const q = new URLSearchParams();
    q.set("format", params.format ?? "csv");
    if (params.actor) q.set("actor", params.actor);
    if (params.action) q.set("action", params.action);
    if (params.subject) q.set("subject", params.subject);
    if (params.from) q.set("from", params.from);
    if (params.to) q.set("to", params.to);
    return requestBlob(`/api/v1/audit/export?${q}`);
  },
  listSessions(params: ListParams = {}): Promise<PageEnvelope<SessionSummary>> {
    const q = new URLSearchParams();
    if (params.page) q.set("page", String(params.page));
    if (params.page_size) q.set("page_size", String(params.page_size));
    return request<PageEnvelope<SessionSummary>>(`/api/v1/sessions?${q}`);
  },
  createSession(): Promise<{ session_id: string }> {
    return request("/api/v1/sessions", { method: "POST", body: {} });
  },
  getSession(id: string): Promise<ChatSession> {
    return request<ChatSession>(`/api/v1/sessions/${id}`);
  },
  /** 会话消息 keyset 分页：默认返回尾部窗口；before_id 向前翻历史，after_id 增量刷新。 */
  listSessionMessages(
    id: string,
    params: { before_id?: number; after_id?: number; limit?: number } = {},
  ): Promise<{ items: ChatMessage[]; total: number }> {
    const q = new URLSearchParams();
    if (params.before_id) q.set("before_id", String(params.before_id));
    if (params.after_id) q.set("after_id", String(params.after_id));
    if (params.limit) q.set("limit", String(params.limit));
    return request<{ items: ChatMessage[]; total: number }>(
      `/api/v1/sessions/${encodeURIComponent(id)}/messages?${q}`,
    );
  },
  /** 会话发起的任务（任务↔会话硬绑定，替代前端文本正则联动）。 */
  listSessionTasks(id: string): Promise<PageEnvelope<Task>> {
    return request<PageEnvelope<Task>>(
      `/api/v1/sessions/${encodeURIComponent(id)}/tasks?page=1&page_size=20`,
    );
  },
  chat(sessionId: string | undefined, message: string): Promise<{ session_id: string; reply: string }> {
    return request("/api/v1/chat", {
      method: "POST",
      body: sessionId ? { session_id: sessionId, message } : { message },
    });
  },
  stats(): Promise<Stats> {
    return request<Stats>("/api/v1/stats");
  },
  /** 运维效率指标（窗口 1~90 天，缺省 7）。 */
  statsOps(windowDays = 7): Promise<OpsStats> {
    return request<OpsStats>(`/api/v1/stats/ops?window_days=${windowDays}`);
  },
};

export type { AuthMode };
