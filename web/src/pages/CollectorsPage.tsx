// Collectors 列表（M1，只读）：服务端分页 + 客户端状态/关键字筛选 + 30s 轮询。
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { api } from "../api/client";
import type { Collector, CollectorStatus } from "../api/types";
import { COLLECTOR_STATUS } from "../lib/status";
import { fmtAgo } from "../lib/time";

const PAGE_SIZE = 100; // 服务端上限（分页页签与后端化筛选在 M2 完善）

export default function CollectorsPage() {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<CollectorStatus | "">("");

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["collectors"],
    queryFn: () => api.listCollectors({ page: 1, page_size: PAGE_SIZE }),
    refetchInterval: 30_000,
  });

  const rows = useMemo(() => {
    const items = data?.items ?? [];
    const kw = keyword.trim().toLowerCase();
    return items.filter((c) => {
      if (status && c.status !== status) return false;
      if (!kw) return true;
      return c.instance_uid.toLowerCase().includes(kw) || c.hostname.toLowerCase().includes(kw);
    });
  }, [data, keyword, status]);

  const columns: ColumnsType<Collector> = [
    {
      title: "instance_uid",
      dataIndex: "instance_uid",
      render: (uid: string) => <Link to={`/collectors/${encodeURIComponent(uid)}`}>{uid}</Link>,
    },
    { title: "主机名", dataIndex: "hostname" },
    { title: "版本", dataIndex: "version", width: 110 },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (s: CollectorStatus) => (
        <Tag color={COLLECTOR_STATUS[s]?.color}>{COLLECTOR_STATUS[s]?.label ?? s}</Tag>
      ),
    },
    { title: "分组", dataIndex: "group_id", render: (g: string) => g || "-" },
    { title: "最近上报", dataIndex: "last_seen_at", width: 140, render: fmtAgo },
  ];

  return (
    <div>
      <Typography.Title level={4}>Collectors</Typography.Title>
      {data && data.total > PAGE_SIZE && (
        <Alert
          type="warning"
          showIcon
          message={`共 ${data.total} 个 Collector，当前仅展示前 ${PAGE_SIZE} 个（分页在 M2 完善）。`}
          style={{ marginBottom: 12 }}
        />
      )}
      {isError && (
        <Alert
          type="error"
          showIcon
          message="Collector 列表加载失败"
          action={<a onClick={() => refetch()}>重试</a>}
          style={{ marginBottom: 12 }}
        />
      )}
      <Card>
        <Space style={{ marginBottom: 12 }}>
          <Input.Search
            placeholder="搜索 instance_uid / hostname"
            allowClear
            style={{ width: 280 }}
            onChange={(e) => setKeyword(e.target.value)}
          />
          <Select
            placeholder="状态筛选"
            allowClear
            style={{ width: 140 }}
            value={status || undefined}
            onChange={(v) => setStatus(v ?? "")}
            options={Object.entries(COLLECTOR_STATUS).map(([value, meta]) => ({
              value,
              label: meta.label,
            }))}
          />
        </Space>
        <Table<Collector>
          rowKey="instance_uid"
          columns={columns}
          dataSource={rows}
          loading={isLoading}
          pagination={{ pageSize: 20, showTotal: (n) => `共 ${n} 条` }}
          size="middle"
        />
      </Card>
    </div>
  );
}
