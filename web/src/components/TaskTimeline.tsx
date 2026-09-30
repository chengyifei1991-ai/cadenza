// 任务状态流转时间线（1.2.0 C-a）：回放 task_events 里的每次状态迁移与相邻间隔时长。
// 数据源就是埋点事件表（事实回放），不按任务当前状态反推——状态被改过也能看出来。
import { useQuery } from "@tanstack/react-query";
import { Card, Timeline, Typography } from "antd";
import { api } from "../api/client";
import type { TaskEvent, TaskStatus } from "../api/types";
import { TASK_STATUS } from "../lib/status";
import { fmtDateTime, fmtDurationMs } from "../lib/time";

/** 事件色板：终态绿/红，待审批橙，其余处理中蓝（与状态标签同色系）。 */
function eventColor(to: string): string {
  switch (to) {
    case "done":
      return "green";
    case "failed":
      return "red";
    case "rejected":
      return "gray";
    case "awaiting_approval":
      return "orange";
    default:
      return "blue";
  }
}

/** 事件文案：迁移前状态为空即"创建"事件（任务可能直接创建为待审批），其余用状态标签。 */
function eventLabel(e: TaskEvent): string {
  if (!e.from_status) return "任务创建";
  return TASK_STATUS[e.to_status as TaskStatus]?.label ?? e.to_status;
}

/** 迁移箭头文案：创建事件没有前态，用"开始"占位。 */
function transitionLabel(e: TaskEvent): string {
  const from = e.from_status
    ? TASK_STATUS[e.from_status as TaskStatus]?.label ?? e.from_status
    : "开始";
  const to = TASK_STATUS[e.to_status as TaskStatus]?.label ?? e.to_status;
  return `${from} → ${to}`;
}

export default function TaskTimeline({
  taskId,
  active,
}: {
  taskId: string;
  /** 任务未进终态时随详情一起轮询，终态后停止。 */
  active: boolean;
}) {
  const q = useQuery({
    queryKey: ["task-events", taskId],
    queryFn: () => api.taskEvents(taskId),
    enabled: Boolean(taskId),
    refetchInterval: active ? 5000 : false,
  });
  const items = q.data?.items ?? [];

  return (
    <Card title="状态流转" loading={q.isLoading} style={{ marginBottom: 16 }}>
      {q.isError ? (
        <Typography.Text type="secondary">状态流转暂不可用（事件埋点查询失败）。</Typography.Text>
      ) : items.length === 0 ? (
        <Typography.Text type="secondary">
          暂无状态流转记录（早于埋点上线的历史任务没有事件数据）。
        </Typography.Text>
      ) : (
        <Timeline
          items={items.map((e, i) => {
            const prev = i > 0 ? new Date(items[i - 1].created_at).getTime() : 0;
            const cur = new Date(e.created_at).getTime();
            const delta = prev && cur > prev ? cur - prev : 0;
            return {
              color: eventColor(e.to_status),
              children: (
                <div>
                  <Typography.Text strong>{eventLabel(e)}</Typography.Text>
                  <Typography.Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>
                    {fmtDateTime(e.created_at)}
                  </Typography.Text>
                  {delta > 0 && (
                    <Typography.Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>
                      （距上一步 {fmtDurationMs(delta)}）
                    </Typography.Text>
                  )}
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {transitionLabel(e)}
                    </Typography.Text>
                  </div>
                </div>
              ),
            };
          })}
        />
      )}
    </Card>
  );
}
