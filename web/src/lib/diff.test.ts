// 行级差异工具单测。
import { describe, expect, it } from "vitest";
import { diffText } from "./diff";

describe("diffText", () => {
  it("相同文本 → 无变更", () => {
    const r = diffText("a\nb\nc\n", "a\nb\nc\n");
    expect(r.changed).toBe(false);
    expect(r.added).toBe(0);
    expect(r.removed).toBe(0);
    expect(r.lines.every((l) => l.kind === "keep")).toBe(true);
  });

  it("单行新增/删除计数正确", () => {
    const r = diffText("a\nb\n", "a\nb\nc\n");
    expect(r.added).toBe(1);
    expect(r.removed).toBe(0);
    expect(r.changed).toBe(true);
    expect(r.lines.filter((l) => l.kind === "add").length).toBe(1);
  });

  it("替换内容 → 删除+新增", () => {
    const r = diffText("x=1\n", "x=2\n");
    expect(r.added).toBe(1);
    expect(r.removed).toBe(1);
  });

  it("空文本边界", () => {
    expect(diffText("", "").changed).toBe(false);
    expect(diffText("", "a\nb").added).toBe(2);
    expect(diffText("a\nb\n", "").removed).toBe(2);
  });
});
