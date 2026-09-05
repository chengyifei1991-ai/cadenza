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

    // 审批
    await user.click(screen.getByRole("button", { name: /审批通过并下发/ }));
    await waitFor(() => expect(approveCalled).toBe(1));
    // 刷新后状态 → 已完成，审批按钮消失
    expect(await screen.findByText("已完成")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /审批通过并下发/ })).not.toBeInTheDocument();
    expect(screen.getByText("admin")).toBeInTheDocument();
  });
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
