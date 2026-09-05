// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import TaskCenterPage from "../pages/TaskCenterPage";
import { makeQueryClient, renderApp, stubFetch } from "../test/testUtils";

const SAMPLE_TASK = {
  id: "t-1",
  type: "generate",
  status: "awaiting_approval",
  require_approval: true,
  input: "为网关加内存限制",
  generated_yaml: "exporters:\n  debug:\n",
  target_group_id: "demo-gateway-1",
  created_at: "2026-09-05T10:00:00Z",
  updated_at: "2026-09-05T10:00:00Z",
};

describe("TaskCenterPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("加载任务列表并渲染（含状态/说明列），行点击进入详情路由", async () => {
    stubFetch([
      {
        match: "/api/v1/tasks?page=1&page_size=10",
        body: { items: [SAMPLE_TASK], total: 1, page: 1, page_size: 10 },
      },
    ]);
    renderApp(
      <Routes>
        <Route path="/tasks" element={<TaskCenterPage />} />
        <Route path="/tasks/:id" element={<div>TASK-DETAIL-STUB</div>} />
      </Routes>,
      "/tasks",
      makeQueryClient(),
    );

    // 等待数据渲染
    expect(await screen.findByText("为网关加内存限制")).toBeInTheDocument();
    expect(screen.getByText("待审批")).toBeInTheDocument();
    expect(screen.getByText("生成")).toBeInTheDocument();

    // 点击行 → 路由跳转到详情占位
    fireEvent.click(screen.getByText("为网关加内存限制"));
    expect(await screen.findByText("TASK-DETAIL-STUB")).toBeInTheDocument();
  });

  it("数据加载失败时显示错误与重试入口", async () => {
    stubFetch([{ match: "/api/v1/tasks", status: 500, body: { error: "boom" } }]);
    renderApp(<TaskCenterPage />, "/tasks", makeQueryClient());
    // 等待错误态（Alert + 重试）出现
    await waitFor(() => expect(screen.getByText(/数据加载失败/)).toBeInTheDocument(), { timeout: 3000 });
    expect(screen.getByText("重试")).toBeInTheDocument();
  });
});
