// 统一 fetch 客户端与类型化 API（契约清单见 docs/web-frontend-prd.md §3）。
import type {
  AuditAction,
  AuditLog,
  AuthMode,
  ChatMessage,
  ChatSession,
  Collector,
  ConfigVersion,
  Me,
  PageEnvelope,
  SessionSummary,
  Stats,
  SystemInfo,
  Task,
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
};

export type { AuthMode };
