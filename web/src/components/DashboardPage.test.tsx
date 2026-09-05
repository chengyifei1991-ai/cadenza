// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import DashboardPage from "../pages/DashboardPage";
import { makeQueryClient, renderApp } from "../test/testUtils";

const iso = "2026-09-05T10:00:00.000Z";

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function stubDashboard(opts: { demo?: boolean; awaiting?: number } = {}) {
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/v1/stats")) {
      return json({
        collectors: { total: 5, healthy: 2, unhealthy: 1, offline: 1, unknown: 1 },
        tasks: {
          total: 6,
          pending: 0,
          generating: 0,
          validating: 0,
          awaiting_approval: opts.awaiting ?? 1,
          applying: 0,
          done: 4,
          rejected: 1,
          failed: 0,
        },
        sessions_total: 3,
      });
    }
    if (url.includes("/api/v1/system/info")) {
      return json({ version: "0.1.0", auth_mode: "off", demo_mode: opts.demo ?? true });
    }
    if (url.includes("/api/v1/tasks?page=1&page_size=5")) {
      return json({
        items: [
          {
            id: "t-recent",
            type: "apply",
            status: "awaiting_approval",
            input: "编辑器提交的内存限制（最近任务）",
            target_group_id: "demo-gateway-1",
            created_at: iso,
            updated_at: iso,
          },
        ],
        total: 1,
        page: 1,
        page_size: 5,
      });
    }
    throw new Error(`[test] 未桩请求 ${url}`);
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

describe("DashboardPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.clear();
  });

  it("聚合统计/待审批提醒/最近任务渲染", async () => {
    stubDashboard();
    renderApp(<DashboardPage />, "/", makeQueryClient());

    expect(await screen.findByText("Collector 总数")).toBeInTheDocument();
    expect(await screen.findByText("5")).toBeInTheDocument();
    // 待审批提醒
    expect(await screen.findByText(/1 个任务待审批/)).toBeInTheDocument();
    // 演示模式横幅 + 最近任务
    expect(await screen.findByText(/演示数据模式/)).toBeInTheDocument();
    expect(await screen.findByText(/编辑器提交的内存限制/)).toBeInTheDocument();
  });

  it("无待审批时不显示红色提醒；非演示模式无横幅", async () => {
    stubDashboard({ demo: false, awaiting: 0 });
    renderApp(<DashboardPage />, "/", makeQueryClient());
    await screen.findByText("Collector 总数");
    expect(screen.queryByText(/个任务待审批/)).not.toBeInTheDocument();
    expect(screen.queryByText(/演示数据模式/)).not.toBeInTheDocument();
  });
});
