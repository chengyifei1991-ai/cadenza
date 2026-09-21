// 行级 diff 展示（diffText 的渲染层）：审批/回滚确认/编辑器变更预览共用。
import { useMemo } from "react";
import { Tag, Typography } from "antd";
import { diffText } from "../lib/diff";
import type { DiffResult } from "../lib/diff";

interface Props {
  /** 待比较的原文（与 result 二选一） */
  oldText?: string;
  /** 待比较的新文（与 result 二选一） */
  newText?: string;
  /** 直接传入已算好的差异（如服务端 unified diff 解析结果），优先于 oldText/newText */
  result?: DiffResult;
  /** 最大显示行数（超出显示截断提示，避免大配置卡渲染） */
  maxLines?: number;
  height?: number;
}

export default function DiffView({ oldText, newText, result: given, maxLines = 400, height = 320 }: Props) {
  const computed = useMemo(
    () => given ?? diffText(oldText ?? "", newText ?? ""),
    [given, oldText, newText],
  );
  const result = computed;
  if (!result.changed) {
    return <Typography.Text type="secondary">配置内容一致，无差异。</Typography.Text>;
  }

  const truncated = result.lines.length > maxLines;
  const rows = truncated ? result.lines.slice(0, maxLines) : result.lines;

  return (
    <div>
      <div style={{ marginBottom: 8 }}>
        <Tag color="red">-{result.removed}</Tag>
        <Tag color="green">+{result.added}</Tag>
      </div>
      <div
        style={{
          fontFamily: "'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace",
          fontSize: 12,
          lineHeight: 1.6,
          overflow: "auto",
          maxHeight: height,
          border: "1px solid #f0f0f0",
          borderRadius: 4,
          background: "#fff",
        }}
      >
        {rows.map((l, i) => (
          <div
            key={i}
            style={{
              padding: "0 8px",
              whiteSpace: "pre",
              background:
                l.kind === "add" ? "#f6ffed" : l.kind === "remove" ? "#fff1f0" : "transparent",
              color: l.kind === "add" ? "#389e0d" : l.kind === "remove" ? "#cf1322" : "inherit",
            }}
          >
            {l.kind === "add" ? "+ " : l.kind === "remove" ? "- " : "  "}
            {l.text || "␤"}
          </div>
        ))}
      </div>
      {truncated && (
        <Typography.Text type="secondary" style={{ marginTop: 4, display: "block" }}>
          变更行较多，仅显示前 {maxLines} 行（共 {result.lines.length} 行）。
        </Typography.Text>
      )}
    </div>
  );
}
