// 时间工具单测（fmtAgo 相对时间 / fmtDateTime 本地化）。
import { describe, expect, it } from "vitest";
import { fmtAgo, fmtDateTime } from "./time";

const MIN = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;

function isoAgo(ms: number): string {
  return new Date(Date.now() - ms).toISOString();
}

describe("fmtAgo", () => {
  it("一分钟内 → 刚刚", () => {
    expect(fmtAgo(isoAgo(5_000))).toBe("刚刚");
  });
  it("分钟级", () => {
    expect(fmtAgo(isoAgo(5 * MIN))).toBe("5 分钟前");
  });
  it("小时级", () => {
    expect(fmtAgo(isoAgo(5 * HOUR))).toBe("5 小时前");
  });
  it("天级", () => {
    expect(fmtAgo(isoAgo(5 * DAY))).toBe("5 天前");
  });
  it("超 30 天回退到本地日期字符串", () => {
    expect(fmtAgo(isoAgo(40 * DAY))).not.toContain("天前");
    expect(fmtAgo(isoAgo(40 * DAY))).toBeTruthy();
  });
  it("空/非法输入 → -", () => {
    expect(fmtAgo("")).toBe("-");
    expect(fmtAgo(undefined)).toBe("-");
    expect(fmtAgo("not-a-date")).toBe("-");
  });
});

describe("fmtDateTime", () => {
  it("输出本地化日期且包含年份", () => {
    const iso = new Date(2026, 0, 2, 3, 4, 5).toISOString(); // 固定时刻
    const out = fmtDateTime(iso);
    expect(out).not.toBe(iso);
    expect(out).toContain("2026");
  });
  it("非法输入原样返回", () => {
    expect(fmtDateTime("garbage")).toBe("garbage");
  });
});
