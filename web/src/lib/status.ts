// 状态 → 展示文案/颜色的唯一映射（与后端枚举一致，见 internal/store/models.go）。
import type { CollectorStatus, TaskStatus } from "../api/types";

export const COLLECTOR_STATUS: Record<CollectorStatus, { label: string; color: string }> = {
  healthy: { label: "健康", color: "green" },
  unhealthy: { label: "异常", color: "red" },
  offline: { label: "离线", color: "default" },
  unknown: { label: "未知", color: "orange" },
};

export const TASK_STATUS: Record<TaskStatus, { label: string; color: string }> = {
  pending: { label: "待开始", color: "default" },
  generating: { label: "生成中", color: "processing" },
  validating: { label: "校验中", color: "processing" },
  awaiting_approval: { label: "待审批", color: "warning" },
  applying: { label: "下发生效中", color: "processing" },
  done: { label: "已完成", color: "success" },
  rejected: { label: "已拒绝", color: "default" },
  failed: { label: "失败", color: "error" },
};

export const TASK_TYPE_LABEL: Record<string, string> = {
  generate: "生成",
  optimize: "优化",
  apply: "下发",
  rollback: "回滚",
  upgrade: "升级",
};
