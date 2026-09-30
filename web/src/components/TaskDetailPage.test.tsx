// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import TaskDetailPage from "../pages/TaskDetailPage";
import { makeQueryClient, renderApp } from "../test/testUtils";

const iso = "2026-09-05T10:00:00.000Z";
const CURRENT = "receivers:\n  otlp:\n    protocols:\n      grpc:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n";
const GENERATED = "receivers:\n  otlp:\n    protocols:\n      grpc:\n      http:\nexporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n";

const awaitingTask = {
  id: "t-1",
  type: "apply",
  status: "awaiting_approval",
  require_approval: true,
  input: "编辑器提交：启用 otlp/http（e2e）",
  generated_yaml: GENERATED,
  target_instance_uid: "demo-gateway-1",
  target_group_id: "demo-gateway-1",
  created_at: iso,
  updated_at: iso,
};

const doneTask = { ...awaitingTask, status: "done", approver: "admin" };

/** 状态迁移事件（时间线数据源）：创建 → 待审批（间隔 30s）。 */
const TASK_EVENTS = [
  { id: 1, task_id: "t-1", from_status: "", to_status: "pending", created_at: "2026-09-05T10:00:00.000Z" },
  {
    id: 2,
    task_id: "t-1",
    from_status: "pending",
    to_status: "awaiting_approval",
    created_at: "2026-09-05T10:00:30.000Z",
  },
];

describe("TaskDetailPage 审批闭环", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("展示变更 diff 并完成审批通过（approve → done）", async () => {
    const user = userEvent.setup();
    let approveCalled = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET" && url.includes("/api/v1/collectors/demo-gateway-1")) {
        return json({ instance_uid: "demo-gateway-1", status: "healthy", effective_config: CURRENT });
      }
      // 注意：events 必须先于 /api/v1/tasks/t-1 前缀匹配
      if (method === "GET" && url.includes("/api/v1/tasks/t-1/events")) {
        const items = approveCalled > 0 ? [
          ...TASK_EVENTS,
          { id: 3, task_id: "t-1", from_status: "awaiting_approval", to_status: "applying", created_at: "2026-09-05T10:01:00.000Z" },
          { id: 4, task_id: "t-1", from_status: "applying", to_status: "done", created_at: "2026-09-05T10:01:05.000Z" },
        ] : TASK_EVENTS;
        return json({ task_id: "t-1", items, total: items.length });
      }
      if (method === "GET" && url.includes("/api/v1/tasks/t-1")) {
        return json(approveCalled > 0 ? doneTask : awaitingTask);
      }
      if (method === "POST" && url.includes("/api/v1/tasks/t-1/approve")) {
        approveCalled += 1;
        return json({ task_id: "t-1", status: "done", message: "已下发" });
      }
      throw new Error(`[test] 未桩请求 ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp(
      <Routes>
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
      </Routes>,
      "/tasks/t-1",
      makeQueryClient(),
    );

    // 状态为待审批；diff 卡片异步出现（等待目标 Collector 加载）
    expect(await screen.findByText("待审批")).toBeInTheDocument();
    expect(await screen.findByText(/变更内容/)).toBeInTheDocument();
    // 新增行内容可见（http 出现在差异区）
    expect(screen.getByText(/\+[1-9]\d*$/)).toBeInTheDocument();

    // 状态流转时间线（1.2.0 C-a）：创建事件 + 相邻间隔时长
    expect(await screen.findByText("状态流转")).toBeInTheDocument();
    expect(await screen.findByText("任务创建")).toBeInTheDocument();
    expect(await screen.findByText(/距上一步 30\.0 s/)).toBeInTheDocument();
    expect(await screen.findByText(/待开始 → 待审批/)).toBeInTheDocument();

    // 审批
    await user.click(screen.getByRole("button", { name: /审批通过并下发/ }));
    await waitFor(() => expect(approveCalled).toBe(1));
    // 刷新后进入终态：审批按钮消失，状态与时间线均出现"已完成"
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /审批通过并下发/ })).not.toBeInTheDocument(),
    );
    expect(screen.getAllByText("已完成").length).toBeGreaterThan(0);
    expect(screen.getByText("admin")).toBeInTheDocument();
    // 终态后时间线补齐下发与完成两步
    expect(await screen.findByText(/下发生效中 → 已完成/)).toBeInTheDocument();
  });

  it("历史任务无事件时给空态文案；事件接口异常时降级不影响详情", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET" && url.includes("/api/v1/tasks/t-2/events")) {
        return new Response(JSON.stringify({ error: "查询任务事件失败" }), {
          status: 500,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (method === "GET" && url.includes("/api/v1/tasks/t-2")) {
        return json({ ...doneTask, id: "t-2" });
      }
      if (method === "GET" && url.includes("/api/v1/collectors/")) {
        return json({ instance_uid: "demo-gateway-1", status: "healthy", effective_config: CURRENT });
      }
      throw new Error(`[test] 未桩请求 ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp(
      <Routes>
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
      </Routes>,
      "/tasks/t-2",
      makeQueryClient(),
    );

    expect(await screen.findByText("状态流转")).toBeInTheDocument();
    expect(await screen.findByText(/状态流转暂不可用/)).toBeInTheDocument();
    // 详情主体不受影响
    expect(await screen.findByText("任务信息")).toBeInTheDocument();
  });
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
