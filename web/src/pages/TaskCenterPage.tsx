// 任务中心（M2）：状态筛选 + 服务端分页 + 10s 轮询；行点击进详情（审批/回滚跟踪）。
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SearchOutlined } from "@ant-design/icons";
import { Card, Select, Table, Tag, Tooltip, Typography } from "antd";
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
    queryKey: ["tasks", status, taskType, page],
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
  // 列头过滤（服务端）：点击列头的搜索图标选择取值 → 重新查询后端。
  const filterDropdown = (
    options: Array<{ value: string; label: string }>,
    value: string,
    onPick: (v: string) => void,
  ) => (
    <div style={{ padding: 8 }}>
      <Select
        autoFocus
        open
        allowClear
        placeholder="全部"
        style={{ width: 180 }}
        value={value || undefined}
        onChange={(v) => onPick((v as string) ?? "")}
        options={options}
      />
    </div>
  );

  const columns: ColumnsType<Task> = [
    {
      title: "类型",
      dataIndex: "type",
      width: 110,
      filteredValue: taskType ? [taskType] : null,
      filterIcon: (filtered: boolean) => <SearchOutlined style={{ color: filtered ? "#1677ff" : undefined }} />,
      filterDropdown: () =>
        filterDropdown(
          Object.entries(TASK_TYPE_LABEL).map(([value, label]) => ({ value, label })),
          taskType,
          (v) => {
            setTaskType(v as TaskType | "");
            setPage(1);
          },
        ),
      render: (t: Task["type"]) => <Tag>{TASK_TYPE_LABEL[t] ?? t}</Tag>,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 140,
      filteredValue: status ? [status] : null,
      filterIcon: (filtered: boolean) => <SearchOutlined style={{ color: filtered ? "#1677ff" : undefined }} />,
      filterDropdown: () =>
        filterDropdown(
          Object.entries(TASK_STATUS).map(([value, meta]) => ({ value, label: meta.label })),
          status,
          (v) => {
            setStatus(v as TaskStatus | "");
            setPage(1);
          },
        ),
      render: (s: TaskStatus) => (
        <Tag color={TASK_STATUS[s]?.color}>{TASK_STATUS[s]?.label ?? s}</Tag>
      ),
    },
    {
      title: "目标",
      dataIndex: "target_group_id",
      width: 190,
      ellipsis: true,
      render: (g: string) => g || "-",
    },
    { title: "审批人", dataIndex: "approver", width: 100, render: (a?: string) => a || "-" },
    { title: "创建", dataIndex: "created_at", width: 130, render: fmtAgo },
    // 说明放最后一列：内容可能很长，截断 + 悬浮查看全文。
    {
      title: "说明",
      dataIndex: "input",
      ellipsis: true,
      render: (text: string) =>
        text ? (
          <Tooltip title={text} placement="topLeft">
            <span>{text}</span>
          </Tooltip>
        ) : (
          "-"
        ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 12 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          任务中心
        </Typography.Title>
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
