// 后端 REST 契约类型（字段与 internal/store & internal/api 的 JSON 对齐）。
// 页面里程碑按需扩展；契约变更请先改后端再同步此处（docs/web-frontend-prd.md §3）。

export type AuthMode = "simple" | "off";

export interface SystemInfo {
  version: string;
  auth_mode: AuthMode;
  demo_mode: boolean;
}

export interface Me {
  username: string;
  auth_mode?: AuthMode;
}

export type CollectorStatus = "healthy" | "unhealthy" | "offline" | "unknown";

export interface Collector {
  instance_uid: string;
  hostname: string;
  version: string;
  last_seen_at: string;
  status: CollectorStatus;
  effective_config: string;
  group_id: string;
}

export interface ConfigVersion {
  id: number;
  collector_instance_uid: string;
  yaml: string;
  hash: string;
  validated: boolean;
  created_at: string;
}

export type TaskType = "generate" | "optimize" | "apply" | "upgrade" | "rollback";

export type TaskStatus =
  | "pending"
  | "generating"
  | "validating"
  | "awaiting_approval"
  | "applying"
  | "done"
  | "rejected"
  | "failed";

export interface Task {
  id: string;
  type: TaskType;
  status: TaskStatus;
  require_approval: boolean;
  input: string;
  generated_yaml?: string;
  target_group_id?: string;
  target_instance_uid?: string;
  rollback_version_id?: number;
  approvers?: string[];
  approver?: string;
  reject_reason?: string;
  model_used?: string;
  error?: string;
  created_at: string;
  updated_at: string;
}

export type AuditAction = "generate" | "approve" | "reject" | "apply" | "upgrade" | "rollback";

export interface AuditLog {
  id: number;
  actor: string;
  action: AuditAction;
  subject: string;
  detail: string;
  created_at: string;
}

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
  created_at: string;
}

export interface ChatSession {
  id: string;
  messages: ChatMessage[];
  created_at: string;
}

export interface SessionSummary {
  id: string;
  created_at: string;
  message_count: number;
  last_message_at?: string;
  first_message?: string;
  last_message?: string;
}

export interface PageEnvelope<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface Stats {
  collectors: { total: number; healthy: number; unhealthy: number; offline: number; unknown: number };
  tasks: {
    total: number;
    pending: number;
    generating: number;
    validating: number;
    awaiting_approval: number;
    applying: number;
    done: number;
    rejected: number;
    failed: number;
  };
  sessions_total: number;
}
