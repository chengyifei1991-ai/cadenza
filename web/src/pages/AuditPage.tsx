// 审计页（M2）：服务端分页审计日志（操作/actor 着色，detail 悬浮全文）+ 导出（1.2.0 C-b）。
import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  DatePicker,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { DownloadOutlined } from "@ant-design/icons";
import { api, ApiError } from "../api/client";
import ErrorState from "../components/ErrorState";
import type { AuditAction, AuditLog } from "../api/types";
import { AUDIT_ACTION } from "../lib/status";
import { fmtDateTime } from "../lib/time";

const PAGE_SIZE = 20;

const ACTION_OPTIONS = Object.entries(AUDIT_ACTION).map(([value, meta]) => ({
  value,
  label: meta.label,
}));

export default function AuditPage() {
  const [page, setPage] = useState(1);
  // 服务端筛选（1.1.0-c）：操作者/动作/时间区间，避免前端全量过滤。
  const [actor, setActor] = useState("");
  const [action, setAction] = useState<AuditAction | undefined>(undefined);
  const [range, setRange] = useState<[string, string] | null>(null);
  const query = useQuery({
    queryKey: ["audit", page, actor, action, range],
    queryFn: () =>
      api.listAudit({
        page,
        page_size: PAGE_SIZE,
        actor: actor || undefined,
        action,
        from: range?.[0],
        to: range?.[1],
      }),
  });
  const data = query.data;

  // 导出（C-b）：按当前筛选导出，超限时后端返回 400 的中文原因，这里提示而不是下载半份数据。
  const exportAudit = useMutation({
    mutationFn: (format: "csv" | "json") =>
      api.exportAudit({
        actor: actor || undefined,
        action,
        from: range?.[0],
        to: range?.[1],
        format,
      }),
    onSuccess: ({ filename, blob }) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      message.success(`已导出 ${filename}`);
    },
    onError: (err: unknown) => {
      message.error(err instanceof ApiError ? err.message : "导出失败，请重试");
    },
  });

  const columns: ColumnsType<AuditLog> = [
    { title: "时间", dataIndex: "created_at", width: 180, render: fmtDateTime },
    { title: "操作者", dataIndex: "actor", width: 100 },
    {
      title: "动作",
      dataIndex: "action",
      width: 110,
      render: (a: AuditLog["action"]) => (
        <Tag color={AUDIT_ACTION[a]?.color}>{AUDIT_ACTION[a]?.label ?? a}</Tag>
      ),
    },
    {
      title: "对象",
      dataIndex: "subject",
      width: 230,
      ellipsis: true,
      render: (s: string) => (
        <Tooltip title={s}>
          <Typography.Text code>{s}</Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: "详情",
      dataIndex: "detail",
      ellipsis: true,
      render: (d: string) =>
        d ? (
          <Tooltip title={d}>
            <span>{d}</span>
          </Tooltip>
        ) : (
          "-"
        ),
    },
  ];

  return (
    <div>
      <Typography.Title level={4}>审计日志</Typography.Title>
      <Space wrap style={{ marginBottom: 12 }}>
        <Input
          aria-label="按操作者筛选"
          placeholder="操作者（如 admin/agent）"
          allowClear
          style={{ width: 180 }}
          value={actor}
          onChange={(e) => {
            setActor(e.target.value);
            setPage(1);
          }}
        />
        <Select
          aria-label="按动作筛选"
          placeholder="动作"
          allowClear
          style={{ width: 140 }}
          value={action}
          onChange={(v) => {
            setAction(v as AuditAction | undefined);
            setPage(1);
          }}
          options={ACTION_OPTIONS}
        />
        <DatePicker.RangePicker
          showTime
          onChange={(values) => {
            setRange(
              values && values[0] && values[1]
                ? [values[0].toISOString(), values[1].toISOString()]
                : null,
            );
            setPage(1);
          }}
        />
        <Button
          onClick={() => {
            setActor("");
            setAction(undefined);
            setRange(null);
            setPage(1);
          }}
        >
          重置
        </Button>
        <Button
          icon={<DownloadOutlined />}
          loading={exportAudit.isPending}
          onClick={() => exportAudit.mutate("csv")}
        >
          导出 CSV
        </Button>
        <Button
          icon={<DownloadOutlined />}
          loading={exportAudit.isPending}
          onClick={() => exportAudit.mutate("json")}
        >
          导出 JSON
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          导出遵循当前筛选条件，动作本身也会记入审计
        </Typography.Text>
      </Space>
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      <Card>
        <Table<AuditLog>
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
          size="small"
        />
      </Card>
    </div>
  );
}
