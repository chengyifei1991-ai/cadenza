// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import DashboardPage from "../pages/DashboardPage";
import { makeQueryClient, renderApp } from "../test/testUtils";

const iso = "2026-09-05T10:00:00.000Z";

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

/** /api/v1/stats/ops 桩数据（窗口内 8 个任务：7 完成 / 1 失败）。 */
const OPS = {
  window_days: 7,
  tasks: { total: 8, done: 7, failed: 1, success_rate: 0.875 },
  approval_wait_ms: { count: 3, avg: 42000, p50: 30000, p90: 60000 },
  dispatch_ms: { count: 5, avg: 9000, p50: 8000, p90: 12000 },
  rollback: { count: 1, avg_duration_ms: 15000 },
};

const OPS_EMPTY = {
  window_days: 7,
  tasks: { total: 0, done: 0, failed: 0, success_rate: 0 },
  approval_wait_ms: { count: 0, avg: 0, p50: 0, p90: 0 },
  dispatch_ms: { count: 0, avg: 0, p50: 0, p90: 0 },
  rollback: { count: 0, avg_duration_ms: 0 },
};

function stubDashboard(
  opts: { demo?: boolean; awaiting?: number; ops?: unknown; opsStatus?: number } = {},
) {
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    // 注意：ops 路由必须先于 /api/v1/stats 前缀匹配（后者是其前缀）。
    if (url.includes("/api/v1/stats/ops")) {
      if (opts.opsStatus && opts.opsStatus !== 200) {
        return new Response(JSON.stringify({ error: "查询任务事件失败" }), {
          status: opts.opsStatus,
          headers: { "Content-Type": "application/json" },
        });
      }
      return json(opts.ops ?? OPS);
    }
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

  it("运维效率卡片（F-17）：成功率/审批等待/下发时长/回滚按窗口渲染", async () => {
    const fetchMock = stubDashboard();
    renderApp(<DashboardPage />, "/", makeQueryClient());

    expect(await screen.findByText("运维效率")).toBeInTheDocument();
    expect(await screen.findByText("下发成功率")).toBeInTheDocument();
    // AntD Statistic 会把数值拆成整数/小数/后缀多个 span，按容器整体文本断言
    const stat = (label: string) =>
      (screen.getByText(label).closest(".ant-statistic") as HTMLElement | null)?.textContent ?? "";
    await waitFor(() => expect(stat("下发成功率")).toContain("87.5"));
    expect(stat("下发成功率")).toContain("%");
    expect(await screen.findByText(/已完成 7 \/ 失败 1（窗口内 8 个任务）/)).toBeInTheDocument();
    // 审批等待：P50 30000ms → 30.0 s；P90 60000ms → 1.0 分钟
    expect(stat("审批等待 P50")).toContain("30.0");
    expect(await screen.findByText(/P90 1\.0 分钟｜样本 3/)).toBeInTheDocument();
    // 下发时长：P50 8000ms → 8.0 s
    expect(stat("下发时长 P50")).toContain("8.0");
    expect(await screen.findByText(/P90 12\.0 s｜样本 5/)).toBeInTheDocument();
    // 回滚：次数 + 平均耗时
    expect(await screen.findByText("回滚次数")).toBeInTheDocument();
    expect(stat("回滚次数")).toContain("1");
    expect(await screen.findByText(/平均耗时 15\.0 s/)).toBeInTheDocument();
    // 默认窗口为 7 天
    expect(
      fetchMock.mock.calls.some((c) => String(c[0]).includes("/api/v1/stats/ops?window_days=7")),
    ).toBe(true);
  });

  it("运维效率卡片：切窗口后按新窗口重新取数", async () => {
    const fetchMock = stubDashboard();
    renderApp(<DashboardPage />, "/", makeQueryClient());
    await screen.findByText("运维效率");

    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(await screen.findByTitle("近 30 天"));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) => String(c[0]).includes("/api/v1/stats/ops?window_days=30")),
      ).toBe(true),
    );
  });

  it("运维效率卡片：窗口内无数据时给引导文案，不显示 0% 误导值", async () => {
    stubDashboard({ ops: OPS_EMPTY });
    renderApp(<DashboardPage />, "/", makeQueryClient());
    expect(await screen.findByText(/近 7 天暂无任务数据/)).toBeInTheDocument();
    expect(screen.queryByText("下发成功率")).not.toBeInTheDocument();
  });

  it("运维效率卡片：接口异常时降级提示，不影响其他卡片", async () => {
    stubDashboard({ opsStatus: 500 });
    renderApp(<DashboardPage />, "/", makeQueryClient());
    expect(await screen.findByText(/运维效率指标暂不可用/)).toBeInTheDocument();
    expect(await screen.findByText("Collector 总数")).toBeInTheDocument();
  });
});
