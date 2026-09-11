// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import AssistantPage from "../pages/AssistantPage";
import { makeQueryClient, renderApp } from "../test/testUtils";

const iso = "2026-09-05T10:00:00.000Z";

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const TASK_ID = "1a0710caaef1afb91d4e20743e0";

function stubRoutes(opts: { reply?: string; chatStatus?: number; boundTasks?: unknown[] } = {}) {
  const routes = {
    sessions: false,
    sessionDetail: false,
    chat: 0,
    getTask: 0,
  };
  // 对话记录：每次 chat 成功追加一组（服务端存储后随详情回读）
  const convo: Array<{ q: string; a: string }> = [];
  const replyText = () => opts.reply ?? `已生成配置任务 ${TASK_ID}，等待审批。`;
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "GET" && url.includes("/api/v1/sessions?page=1&page_size=20")) {
      routes.sessions = true;
      return json({
        items: [
          { id: "s-1", created_at: iso, message_count: 2, first_message: "历史会话问题", last_message_at: iso },
        ],
        total: 1,
        page: 1,
        page_size: 20,
      });
    }
    // 会话发起的任务（任务↔会话绑定端点）
    if (method === "GET" && /\/api\/v1\/sessions\/[^/]+\/tasks/.test(url)) {
      const items = opts.boundTasks ?? [];
      return json({ items, total: items.length, page: 1, page_size: 20 });
    }
    const sessMatch = url.match(/\/api\/v1\/sessions\/([\w-]+)$/);
    if (method === "GET" && sessMatch) {
      routes.sessionDetail = true;
      const msgs = [{ role: "user", content: "历史会话问题", created_at: iso }];
      if (sessMatch[1] === "s-1") msgs.push({ role: "assistant", content: "历史回答", created_at: iso });
      for (const c of convo) {
        msgs.push({ role: "user", content: c.q, created_at: iso });
        msgs.push({ role: "assistant", content: c.a, created_at: iso });
      }
      return json({ id: sessMatch[1], created_at: iso, messages: msgs });
    }
    if (method === "GET" && url.includes(`/api/v1/tasks/${TASK_ID}`)) {
      routes.getTask += 1;
      return json({ id: TASK_ID, type: "generate", status: "awaiting_approval", input: "x", created_at: iso, updated_at: iso });
    }
    if (method === "POST" && url.includes("/api/v1/chat")) {
      routes.chat += 1;
      const body = JSON.parse((init?.body as string) ?? "{}");
      const status = opts.chatStatus ?? 200;
      if (status !== 200) {
        return json({ error: "LLM 不可用" }, status);
      }
      convo.push({ q: body.message ?? "?", a: replyText() });
      return json({ session_id: body.session_id ?? "s-new", reply: replyText() }, 200);
    }
    if (method === "POST" && url.includes("/api/v1/sessions")) {
      return json({ session_id: "s-new" });
    }
    throw new Error(`[test] 未桩请求 ${method} ${url}`);
  });
  vi.stubGlobal("fetch", fn);
  return { fn, routes };
}

function renderAssistant() {
  renderApp(
    <Routes>
      <Route path="/assistant" element={<AssistantPage />} />
      <Route path="/tasks/:id" element={<div>TASK-STUB</div>} />
    </Routes>,
    "/assistant",
    makeQueryClient(),
  );
}

describe("AssistantPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("展示会话历史并发送消息（回复追加 + 任务联动卡片）", async () => {
    const user = userEvent.setup();
    const { routes } = stubRoutes();
    renderAssistant();

    // 历史会话出现在列表，点击后回读历史
    expect(await screen.findByText("历史会话问题")).toBeInTheDocument();
    fireEvent.click(screen.getByText("历史会话问题"));
    expect(await screen.findByText("历史回答")).toBeInTheDocument();

    // 发送消息
    const ta = screen.getByRole("textbox", { name: /对话输入/ });
    await user.type(ta, "给网关加内存限制");
    await user.click(screen.getByRole("button", { name: /发 送|发送/ }));
    await waitFor(() => expect(routes.chat).toBe(1));
    expect(await screen.findByText(/已生成配置任务/)).toBeInTheDocument();
    // 任务联动卡片（链接到任务详情）
    expect(await screen.findByText(/\[生成\] 待审批/)).toBeInTheDocument();
  });

  it("LLM 503 时显示降级横幅并支持重发", async () => {
    const user = userEvent.setup();
    const { routes } = stubRoutes({ reply: "ok", chatStatus: 503 });
    renderAssistant();

    const ta = screen.getByRole("textbox", { name: /对话输入/ });
    await user.type(ta, "第一条问题");
    await user.click(screen.getByRole("button", { name: /发送/ }));
    await waitFor(() => expect(routes.chat).toBe(1));
    expect(await screen.findByText(/智能体暂时不可用/)).toBeInTheDocument();
    // 重发按钮存在
    const retry = screen.getByRole("button", { name: /重 试|重试/ });
    await user.click(retry);
    await waitFor(() => expect(routes.chat).toBe(2));
  });

  it("空会话示例点击即发送", async () => {
    const user = userEvent.setup();
    const { routes } = stubRoutes({ reply: "示例已发送" });
    renderAssistant();
    // 未选会话时展示示例标签
    const example = await screen.findByText(/demo-gateway-1/);
    await user.click(example);
    await waitFor(() => expect(routes.chat).toBe(1));
    expect(await screen.findByText("示例已发送")).toBeInTheDocument();
  });
});
