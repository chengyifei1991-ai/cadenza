// 行级文本差异（jsdiff 封装）：配置编辑器/审批/回滚共用同一 diff 心智。
import { diffLines } from "diff";

export interface DiffLine {
  kind: "add" | "remove" | "keep";
  text: string;
}

export interface DiffResult {
  lines: DiffLine[];
  added: number;
  removed: number;
  changed: boolean;
}

/** 计算 oldText → newText 的行级差异与变更统计（新增/删除行数）。 */
export function diffText(oldText: string, newText: string): DiffResult {
  const parts = diffLines(oldText || "", newText || "");
  const lines: DiffLine[] = [];
  let added = 0;
  let removed = 0;
  for (const part of parts) {
    if (part.added) {
      added += countLines(part.value);
      for (const line of splitLines(part.value)) lines.push({ kind: "add", text: line });
    } else if (part.removed) {
      removed += countLines(part.value);
      for (const line of splitLines(part.value)) lines.push({ kind: "remove", text: line });
    } else {
      for (const line of splitLines(part.value)) lines.push({ kind: "keep", text: line });
    }
  }
  return { lines, added, removed, changed: added > 0 || removed > 0 };
}

function countLines(value: string): number {
  if (value === "") return 0;
  return splitLines(value).length;
}

/** 拆分时保留空段（渲染层逐行显示，含空行）。 */
function splitLines(value: string): string[] {
  if (value === "") return [];
  // diffLines 输出的 value 以 \n 结尾；去掉末尾空元素避免多渲染一行。
  const all = value.split("\n");
  if (all.length > 0 && all[all.length - 1] === "") all.pop();
  return all;
}

/**
 * 解析服务端 unified diff 文本（1.1.0-d）为统一渲染结构。
 * 忽略 `--- a` / `+++ b` 文件头与 `@@` hunk 头；其余按前缀归类。
 */
export function parseUnified(unified: string): DiffResult {
  const lines: DiffLine[] = [];
  let added = 0;
  let removed = 0;
  for (const raw of (unified || "").split("\n")) {
    if (raw === "" || raw.startsWith("--- ") || raw.startsWith("+++ ") || raw.startsWith("@@")) continue;
    const prefix = raw[0];
    const text = raw.slice(1);
    if (prefix === "+") {
      added += 1;
      lines.push({ kind: "add", text });
    } else if (prefix === "-") {
      removed += 1;
      lines.push({ kind: "remove", text });
    } else if (prefix === " ") {
      lines.push({ kind: "keep", text });
    }
  }
  return { lines, added, removed, changed: added > 0 || removed > 0 };
}
