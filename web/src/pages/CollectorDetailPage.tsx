// Collector 详情（M1，只读）：概览 + 当前生效配置 + 版本历史速览。
// 配置编辑/校验/回滚交互属 M2 范围（docs/web-frontend-prd.md §5.5-5.6）。
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeftOutlined } from "@ant-design/icons";
import { api } from "../api/client";
import type { ConfigVersion, CollectorStatus } from "../api/types";
import { COLLECTOR_STATUS } from "../lib/status";
import { fmtAgo, fmtDateTime } from "../lib/time";

export default function CollectorDetailPage() {
  const { uid = "" } = useParams();

  const collector = useQuery({
    queryKey: ["collector", uid],
    queryFn: () => api.getCollector(uid),
    enabled: Boolean(uid),
    refetchInterval: 30_000,
  });
  const versions = useQuery({
    queryKey: ["versions", uid],
    queryFn: () => api.listVersions(uid, { page: 1, page_size: 10 }),
    enabled: Boolean(uid),
  });

  const c = collector.data;
  if (collector.isLoading) return <Skeleton active />;
  if (collector.isError || !c) {
    return (
      <Alert
        type="error"
        showIcon
        message="Collector 不存在或加载失败"
        action={<Link to="/collectors">返回列表</Link>}
      />
    );
  }

  const versionColumns: ColumnsType<ConfigVersion> = [
    { title: "版本 ID", dataIndex: "id", width: 90 },
    { title: "时间", dataIndex: "created_at", render: fmtDateTime },
    {
      title: "校验",
      dataIndex: "validated",
      width: 90,
      render: (v: boolean) => (v ? <Tag color="green">通过</Tag> : <Tag color="red">未通过</Tag>),
    },
    { title: "Hash", dataIndex: "hash", ellipsis: true, render: (h: string) => <Typography.Text code>{h.slice(0, 12)}</Typography.Text> },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Link to="/collectors">
          <ArrowLeftOutlined /> 返回列表
        </Link>
      </Space>
      <Typography.Title level={4} style={{ wordBreak: "break-all" }}>
        {c.instance_uid}
      </Typography.Title>
      <Card title="概览" style={{ marginBottom: 16 }}>
        <Descriptions column={{ xs: 1, md: 2 }}>
          <Descriptions.Item label="主机名">{c.hostname || "-"}</Descriptions.Item>
          <Descriptions.Item label="版本">{c.version || "-"}</Descriptions.Item>
          <Descriptions.Item label="状态">
            <Tag color={COLLECTOR_STATUS[c.status as CollectorStatus]?.color}>
              {COLLECTOR_STATUS[c.status as CollectorStatus]?.label ?? c.status}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="分组">{c.group_id || "-"}</Descriptions.Item>
          <Descriptions.Item label="最近上报">{fmtDateTime(c.last_seen_at)}（{fmtAgo(c.last_seen_at)}）</Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="当前生效配置" style={{ marginBottom: 16, maxHeight: 480, overflow: "auto" }}>
        {c.effective_config ? (
          <pre style={{ margin: 0, fontSize: 12 }}>{c.effective_config}</pre>
        ) : (
          <Typography.Text type="secondary">尚未上报生效配置（demo 状态或新接入实例）。</Typography.Text>
        )}
      </Card>
      <Card
        title="版本历史"
        extra={
          versions.data && versions.data.total > 10 ? (
            <Typography.Text type="secondary">共 {versions.data.total} 条，仅示最近 10 条</Typography.Text>
          ) : undefined
        }
      >
        <Table<ConfigVersion>
          rowKey="id"
          columns={versionColumns}
          dataSource={versions.data?.items ?? []}
          loading={versions.isLoading}
          pagination={false}
          size="small"
        />
      </Card>
    </div>
  );
}
