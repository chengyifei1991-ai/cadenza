// AI 助手/Onboarding 纯逻辑：任务 id 提取、Collector 接入示例生成、引导首次判定。
// 均为纯函数，便于单测（不改动即可验证）。

const TASK_ID_RE = /\b([0-9a-f]{24,32})\b/g;

/** 从回复文本中提取疑似任务 ULID（去重，保持出现顺序）。 */
export function extractTaskIds(text: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const m of text.matchAll(TASK_ID_RE)) {
    const id = m[1];
    if (!seen.has(id)) {
      seen.add(id);
      out.push(id);
    }
  }
  return out;
}

/** 生成 Collector 接入（opamp extension）示例 YAML；host 为空时使用 location。 */
export function buildCollectorSnippet(host: string, wsPath = "/v1/opamp", instanceUid = "<32位hex>"): string {
  const clean = (h: string) => h.replace(/\/+$/, "");
  let wsEndpoint: string;
  if (host.startsWith("ws://") || host.startsWith("wss://")) {
    wsEndpoint = `${clean(host)}${wsPath}`;
  } else {
    const hostOnly = host.replace(/^https?:\/\//, "");
    wsEndpoint = `ws://${clean(hostOnly)}${wsPath}`;
  }
  return `# collector.yaml（opamp extension）
extensions:
  opamp:
    server:
      ws:
        endpoint: ${wsEndpoint}
    instance_uid: ${instanceUid}
service:
  extensions: [opamp]
  pipelines: {}
`;
}

/** Onboarding 首次引导判定：受控封装（storage 可注入便于测试）。 */
export interface FlagStorage {
  get(key: string): string | null;
  set(key: string, value: string): void;
}

const FLAG_KEY = "cadenza:onboarding:done";

export function shouldShowOnboarding(storage: FlagStorage, force = false): boolean {
  return force || storage.get(FLAG_KEY) !== "1";
}

export function markOnboardingDone(storage: FlagStorage): void {
  storage.set(FLAG_KEY, "1");
}
