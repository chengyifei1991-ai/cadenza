// 状态映射表单测：保证与后端枚举同步时映射完备、文案非空。
import { describe, expect, it } from "vitest";
import { COLLECTOR_STATUS, TASK_STATUS, TASK_TYPE_LABEL } from "./status";
import type { CollectorStatus, TaskStatus } from "../api/types";

describe("状态映射（唯一来源）", () => {
  it("Collector 全部状态均有映射", () => {
    const all: CollectorStatus[] = ["healthy", "unhealthy", "offline", "unknown"];
    for (const s of all) {
      expect(COLLECTOR_STATUS[s], `缺少 Collector 状态映射: ${s}`).toBeDefined();
      expect(COLLECTOR_STATUS[s].label.length).toBeGreaterThan(0);
      expect(COLLECTOR_STATUS[s].color.length).toBeGreaterThan(0);
    }
    expect(Object.keys(COLLECTOR_STATUS).sort()).toEqual([...all].sort());
  });

  it("Task 全部状态均有映射", () => {
    const all: TaskStatus[] = [
      "pending",
      "generating",
      "validating",
      "awaiting_approval",
      "applying",
      "done",
      "rejected",
      "failed",
    ];
    for (const s of all) {
      expect(TASK_STATUS[s], `缺少 Task 状态映射: ${s}`).toBeDefined();
      expect(TASK_STATUS[s].label.length).toBeGreaterThan(0);
    }
    expect(Object.keys(TASK_STATUS).sort()).toEqual([...all].sort());
  });

  it("任务类型标签覆盖全部类型", () => {
    for (const t of ["generate", "optimize", "apply", "upgrade", "rollback"]) {
      expect(TASK_TYPE_LABEL[t], `缺少任务类型标签: ${t}`).toBeTruthy();
    }
  });
});
