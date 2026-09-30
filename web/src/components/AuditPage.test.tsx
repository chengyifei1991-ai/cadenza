// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import AuditPage from "../pages/AuditPage";
import { makeQueryClient, renderApp } from "../test/testUtils";

const iso = "2026-09-29T10:00:00.000Z";

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

const AUDIT_ITEMS = [
  {
    id: 2,
    actor: "admin",
    action: "approve",
    subject: "t-1",
    detail: "审批通过",
    created_at: iso,
  },
  { id: 1, actor: "agent", action: "generate", subject: "t-1", detail: "生成配置", created_at: iso },
];

/** 桩：列表 200；导出按 exportStatus 返回 CSV 附件或错误。 */
function stubAudit(opts: { exportStatus?: number } = {}) {
  const status = opts.exportStatus ?? 200;
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/v1/audit/export")) {
      if (status !== 200) {
        return new Response(JSON.stringify({ error: "导出范围过大（120000 行，上限 100000）" }), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("id,created_at,actor,action,subject,detail\n2,2026-09-29T10:00:00Z,admin,approve,t-1,审批通过\n", {
        status: 200,
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "Content-Disposition": 'attachment; filename="cadenza-audit-20260929-100000.csv"',
        },
      });
    }
    if (url.includes("/api/v1/audit")) {
      return json({ items: AUDIT_ITEMS, total: AUDIT_ITEMS.length, page: 1, page_size: 20 });
    }
    throw new Error(`[test] 未桩请求 ${url}`);
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

describe("AuditPage 导出（1.2.0 C-b）", () => {
  beforeEach(() => {
    // jsdom 不实现 Blob URL，下载链路需要这两个桩
    Object.defineProperty(URL, "createObjectURL", { value: vi.fn(() => "blob:mock"), writable: true });
    Object.defineProperty(URL, "revokeObjectURL", { value: vi.fn(), writable: true });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("按当前筛选导出 CSV 并触发下载", async () => {
    const user = userEvent.setup();
    const fetchMock = stubAudit();
    renderApp(<AuditPage />, "/audit", makeQueryClient());

    expect(await screen.findByText("生成配置")).toBeInTheDocument();
    // 先设筛选，导出 URL 应带上它
    await user.type(screen.getByLabelText("按操作者筛选"), "admin");
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) =>
          String(c[0]).includes("/api/v1/audit?") && String(c[0]).includes("actor=admin"),
        ),
      ).toBe(true),
    );

    await user.click(screen.getByRole("button", { name: /导出 CSV/ }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) =>
          String(c[0]).includes("/api/v1/audit/export?format=csv&actor=admin"),
        ),
      ).toBe(true),
    );
    // 下载触发（拿到 Blob 后创建 object URL）
    await waitFor(() => expect(URL.createObjectURL).toHaveBeenCalled());
    expect(await screen.findByText(/已导出 cadenza-audit-/)).toBeInTheDocument();
  });

  it("导出 JSON 使用 format=json", async () => {
    const user = userEvent.setup();
    const fetchMock = stubAudit();
    renderApp(<AuditPage />, "/audit", makeQueryClient());
    await screen.findByText("生成配置");

    await user.click(screen.getByRole("button", { name: /导出 JSON/ }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) => String(c[0]).includes("/api/v1/audit/export?format=json")),
      ).toBe(true),
    );
  });

  it("超出行数上限时提示后端原因，不触发下载", async () => {
    const user = userEvent.setup();
    stubAudit({ exportStatus: 400 });
    renderApp(<AuditPage />, "/audit", makeQueryClient());
    await screen.findByText("生成配置");

    await user.click(screen.getByRole("button", { name: /导出 CSV/ }));
    expect(await screen.findByText(/导出范围过大/)).toBeInTheDocument();
    expect(URL.createObjectURL).not.toHaveBeenCalled();
  });
});
