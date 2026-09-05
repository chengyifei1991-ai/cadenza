// 统一 fetch 客户端与类型化 API（契约清单见 docs/web-frontend-prd.md §3）。
import type {
  AuditLog,
  AuthMode,
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
  listTasks(params: { status?: TaskStatus } & ListParams = {}): Promise<PageEnvelope<Task>> {
    const q = new URLSearchParams();
    if (params.status) q.set("status", params.status);
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

  // 审计 / 会话 / 统计
  listAudit(params: ListParams = {}): Promise<PageEnvelope<AuditLog>> {
    const q = new URLSearchParams();
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
  getSession(id: string): Promise<ChatSession> {
    return request<ChatSession>(`/api/v1/sessions/${id}`);
  },
  stats(): Promise<Stats> {
    return request<Stats>("/api/v1/stats");
  },
};

export type { AuthMode };
