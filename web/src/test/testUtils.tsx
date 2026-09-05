// DOM 测试公共工具：fetch 桩 + 渲染容器。
import { render } from "@testing-library/react";
import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import type { ReactNode } from "react";

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

export function renderApp(ui: ReactNode, route = "/", queryClient?: QueryClient) {
  const qc = queryClient ?? makeQueryClient();
  return render(
    <ConfigProvider locale={zhCN}>
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

export interface StubRoute {
  method?: "GET" | "POST";
  /** URL 子串匹配 */
  match: string;
  status?: number;
  body?: unknown;
}

/** 安装基于 URL 子串 + method 的 fetch 桩；按顺序命中首个 route。 */
export function stubFetch(routes: StubRoute[]) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    const hit = routes.find((r) => (!r.method || r.method === method) && url.includes(r.match));
    if (!hit) throw new Error(`[test] 未桩的请求: ${method} ${url}`);
    return new Response(JSON.stringify(hit.body ?? {}), {
      status: hit.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}
