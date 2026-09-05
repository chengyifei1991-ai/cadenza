// 统一 fetch 客户端与公共类型（契约先行；页面里程碑按需扩展）。
const BASE = ""; // 同源部署；开发期由 Vite proxy 转发 /api。

export interface SystemInfo {
  version: string;
  auth_mode: "simple" | "off";
  demo_mode: boolean;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    credentials: "same-origin",
    headers: init?.body ? { "Content-Type": "application/json" } : undefined,
    ...init,
  });
  if (!resp.ok) {
    let message = `请求失败（${resp.status}）`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* 非 JSON 响应，保留默认文案 */
    }
    const err = new Error(message) as Error & { status?: number };
    err.status = resp.status;
    throw err;
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

export const api = {
  get<T>(path: string): Promise<T> {
    return request<T>(path);
  },
  post<T>(path: string, body: unknown): Promise<T> {
    return request<T>(path, { method: "POST", body: JSON.stringify(body) });
  },
  systemInfo(): Promise<SystemInfo> {
    return request<SystemInfo>("/api/v1/system/info");
  },
};
