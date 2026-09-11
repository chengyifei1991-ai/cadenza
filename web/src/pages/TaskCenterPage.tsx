// 任务中心（M2）：状态筛选 + 服务端分页 + 10s 轮询；行点击进详情（审批/回滚跟踪）。
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, Select, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { api } from "../api/client";
import ErrorState from "../components/ErrorState";
import { TASK_STATUS, TASK_TYPE_LABEL } from "../lib/status";
import type { Task, TaskStatus, TaskType } from "../api/types";
import { fmtAgo } from "../lib/time";

const PAGE_SIZE = 10;

export default function TaskCenterPage() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<TaskStatus | "">("");
  // 服务端筛选（1.1.0-c）：类型过滤走后端，避免前端只筛当前页。
  const [taskType, setTaskType] = useState<TaskType | "">("");
  const [page, setPage] = useState(1);

  const query = useQuery({
    queryKey: ["tasks", status, page],
    queryFn: () =>
      api.listTasks({
        status: status || undefined,
        type: taskType || undefined,
        page,
        page_size: PAGE_SIZE,
      }),
    refetchInterval: 10_000,
  });

  const data = query.data;
  const columns: ColumnsType<Task> = [
    {
      title: "类型",
      dataIndex: "type",
      width: 80,
      render: (t: Task["type"]) => <Tag>{TASK_TYPE_LABEL[t] ?? t}</Tag>,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 110,
      render: (s: TaskStatus) => (
        <Tag color={TASK_STATUS[s]?.color}>{TASK_STATUS[s]?.label ?? s}</Tag>
      ),
    },
    { title: "说明", dataIndex: "input", ellipsis: true },
    {
      title: "目标",
      dataIndex: "target_group_id",
      width: 190,
      ellipsis: true,
      render: (g: string) => g || "-",
    },
    { title: "审批人", dataIndex: "approver", width: 100, render: (a?: string) => a || "-" },
    { title: "创建", dataIndex: "created_at", width: 130, render: fmtAgo },
  ];

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 12 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          任务中心
        </Typography.Title>
        <Select
          aria-label="类型筛选"
          placeholder="类型筛选"
          allowClear
          style={{ width: 150, marginRight: 8 }}
          value={taskType || undefined}
          onChange={(v) => {
            setTaskType((v as TaskType) ?? "");
            setPage(1);
          }}
          options={Object.entries(TASK_TYPE_LABEL).map(([value, label]) => ({ value, label }))}
        />
        <Select
          placeholder="状态筛选"
          allowClear
          style={{ width: 150 }}
          value={status || undefined}
          onChange={(v) => {
            setStatus(v ?? "");
            setPage(1);
          }}
          options={Object.entries(TASK_STATUS).map(([value, meta]) => ({
            value,
            label: meta.label,
          }))}
        />
      </div>
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      <Card>
        <Table<Task>
          rowKey="id"
          columns={columns}
          dataSource={data?.items ?? []}
          loading={query.isLoading}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total: data?.total ?? 0,
            showSizeChanger: false,
            onChange: (p) => setPage(p),
          }}
          onRow={(record) => ({
            onClick: () => navigate(`/tasks/${record.id}`),
            style: { cursor: "pointer" },
          })}
          size="middle"
        />
      </Card>
    </div>
  );
}
