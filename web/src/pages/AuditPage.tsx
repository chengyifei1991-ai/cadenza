// 审计页（M2）：服务端分页审计日志（操作/actor 着色，detail 悬浮全文）。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { api } from "../api/client";
import ErrorState from "../components/ErrorState";
import type { AuditLog } from "../api/types";
import { AUDIT_ACTION } from "../lib/status";
import { fmtDateTime } from "../lib/time";

const PAGE_SIZE = 20;

export default function AuditPage() {
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: ["audit", page],
    queryFn: () => api.listAudit({ page, page_size: PAGE_SIZE }),
  });
  const data = query.data;

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
