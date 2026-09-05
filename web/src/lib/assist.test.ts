// assist 纯函数单测（任务 id 提取 / 接入示例 / 引导标记）。
import { describe, expect, it } from "vitest";
import { buildCollectorSnippet, extractTaskIds, markOnboardingDone, shouldShowOnboarding } from "./assist";

describe("extractTaskIds", () => {
  it("提取回复中的任务 id 并去重", () => {
    const idA = "1a0710caaef1afb91d4e20743e0";
    const idB = "ab0710caaef1afb91d4e20743e0";
    const ids = extractTaskIds(`任务 ${idA} 已创建，等待审批（关联 ${idA}），更多见 ${idB}。`);
    expect(ids).toEqual([idA, idB]);
  });
  it("无任务 id 返回空数组", () => {
    expect(extractTaskIds("配置已生成，请审批。")).toEqual([]);
  });
});

describe("buildCollectorSnippet", () => {
  it("host 规范化并生成 ws endpoint", () => {
    const s = buildCollectorSnippet("http://ops.example.com:8080/");
    expect(s).toContain("ws://ops.example.com:8080/v1/opamp");
    expect(s).toContain("instance_uid: <32位hex>");
  });
  it("保留 wss 前缀", () => {
    expect(buildCollectorSnippet("wss://ops.example.com")).toContain("wss://ops.example.com/v1/opamp");
  });
});

describe("onboarding 首次判定", () => {
  function memStorage(mem: Record<string, string>) {
    return {
      get: (k: string) => mem[k] ?? null,
      set: (k: string, v: string) => {
        mem[k] = v;
      },
    };
  }
  it("未标记/强制重开 → 显示", () => {
    const mem: Record<string, string> = {};
    const st = memStorage(mem);
    expect(shouldShowOnboarding(st)).toBe(true);
    expect(shouldShowOnboarding(st, true)).toBe(true);
  });
  it("标记后不再显示", () => {
    const mem: Record<string, string> = {};
    const st = memStorage(mem);
    markOnboardingDone(st);
    expect(shouldShowOnboarding(st)).toBe(false);
  });
});
