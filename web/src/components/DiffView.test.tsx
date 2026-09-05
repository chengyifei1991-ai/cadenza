// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import DiffView from "./DiffView";

describe("DiffView", () => {
  it("无差异时提示一致", () => {
    render(<DiffView oldText="a\nb\n" newText="a\nb\n" />);
    expect(screen.getByText(/配置内容一致，无差异/)).toBeInTheDocument();
  });

  it("有差异时渲染 +/- 统计与变更行", () => {
    const { container } = render(<DiffView oldText="x=1\n" newText="x=1\ny=2\n" />);
    const tags = container.querySelectorAll(".ant-tag");
    expect(tags.length).toBeGreaterThanOrEqual(2);
    expect(container.textContent ?? "").toContain("+");
    expect(container.textContent ?? "").toContain("y=2");
  });

  it("行数截断提示存在（超大 diff）", () => {
    const bigOld = Array.from({ length: 300 }, (_, i) => `line-${i}`).join("\n") + "\n";
    const bigNew = bigOld + "extra-tail\n";
    const { container } = render(<DiffView oldText={bigOld} newText={bigNew} maxLines={120} />);
    expect(container.textContent ?? "").toContain("仅显示前 120 行");
  });
});
