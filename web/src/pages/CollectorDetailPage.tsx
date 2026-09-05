// Collector 详情（M2）：概览 / 当前配置 / 版本历史（含回滚确认，diff 概览）。
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Descriptions,
  Modal,
  Skeleton,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeftOutlined, EditOutlined, RollbackOutlined } from "@ant-design/icons";
import { api, ApiError } from "../api/client";
import type { ConfigVersion, CollectorStatus } from "../api/types";
import { COLLECTOR_STATUS } from "../lib/status";
import { fmtAgo, fmtDateTime } from "../lib/time";
import DiffView from "../components/DiffView";

export default function CollectorDetailPage() {
  const { uid = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [rollbackTarget, setRollbackTarget] = useState<ConfigVersion | null>(null);

  const collector = useQuery({
    queryKey: ["collector", uid],
    queryFn: () => api.getCollector(uid),
    enabled: Boolean(uid),
    refetchInterval: 30_000,
  });
  const versions = useQuery({
    queryKey: ["versions", uid],
    queryFn: () => api.listVersions(uid, { page: 1, page_size: 20 }),
    enabled: Boolean(uid),
  });

  const rollback = useMutation({
    mutationFn: (v: ConfigVersion) => api.rollbackTask(uid, v.id),
    onSuccess: (task) => {
      setRollbackTarget(null);
      qc.invalidateQueries({ queryKey: ["tasks"] });
      navigate(`/tasks/${task.id}`, { state: { justCreated: true } });
    },
    onError: (err: unknown) => {
      Modal.error({ title: "创建回滚任务失败", content: err instanceof ApiError ? err.message : "请重试" });
    },
  });

  const c = collector.data;
  const items = versions.data?.items ?? [];

  const latest = items.length > 0 ? items[0] : undefined; // id 倒序
  const isCurrent = (v: ConfigVersion) => latest?.id === v.id;

  const versionColumns: ColumnsType<ConfigVersion> = [
    { title: "版本", dataIndex: "id", width: 80 },
    { title: "时间", dataIndex: "created_at", render: fmtDateTime },
    {
      title: "校验",
      dataIndex: "validated",
      width: 80,
      render: (v: boolean) => (v ? <Tag color="green">通过</Tag> : <Tag color="red">未通过</Tag>),
    },
    {
      title: "Hash",
      dataIndex: "hash",
      ellipsis: true,
      render: (h: string) => <Typography.Text code>{h.slice(0, 12)}</Typography.Text>,
    },
    {
      title: "操作",
      width: 130,
      render: (_, v) => (
        <Button
          size="small"
          icon={<RollbackOutlined />}
          disabled={isCurrent(v)}
          onClick={() => setRollbackTarget(v)}
        >
          {isCurrent(v) ? "当前" : "回滚到本版本"}
        </Button>
      ),
    },
  ];

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

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
        <Link to="/collectors">
          <ArrowLeftOutlined /> 返回列表
        </Link>
        <Button
          type="primary"
          icon={<EditOutlined />}
          onClick={() => navigate(`/collectors/${encodeURIComponent(uid)}/edit`)}
        >
          编辑配置
        </Button>
      </div>
      <Typography.Title level={4} style={{ wordBreak: "break-all" }}>
        {c.instance_uid}
      </Typography.Title>
      <Tabs
        items={[
          {
            key: "overview",
            label: "概览",
            children: (
              <Descriptions column={{ xs: 1, md: 2 }} bordered size="small">
                <Descriptions.Item label="主机名">{c.hostname || "-"}</Descriptions.Item>
                <Descriptions.Item label="版本">{c.version || "-"}</Descriptions.Item>
                <Descriptions.Item label="状态">
                  <Tag color={COLLECTOR_STATUS[c.status as CollectorStatus]?.color}>
                    {COLLECTOR_STATUS[c.status as CollectorStatus]?.label ?? c.status}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="分组">{c.group_id || "-"}</Descriptions.Item>
                <Descriptions.Item label="最近上报">
                  {fmtDateTime(c.last_seen_at)}（{fmtAgo(c.last_seen_at)}）
                </Descriptions.Item>
              </Descriptions>
            ),
          },
          {
            key: "config",
            label: "当前配置",
            children: c.effective_config ? (
              <pre style={{ fontSize: 12, maxHeight: 520, overflow: "auto" }}>{c.effective_config}</pre>
            ) : (
              <Typography.Text type="secondary">尚未上报生效配置。</Typography.Text>
            ),
          },
          {
            key: "versions",
            label: `版本历史${items.length > 0 ? `（${versions.data?.total ?? items.length}）` : ""}`,
            children: (
              <div>
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="回滚将创建一个待审批任务，审批通过后把该历史版本下发给本 Collector（产生新的版本快照）。"
                />
                <Table<ConfigVersion>
                  rowKey="id"
                  columns={versionColumns}
                  dataSource={items}
                  loading={versions.isLoading}
                  pagination={false}
                  size="small"
                />
                {versions.data && versions.data.total > items.length && (
                  <Typography.Text type="secondary" style={{ marginTop: 8, display: "block" }}>
                    共 {versions.data.total} 条，当前展示最近 {items.length} 条。
                  </Typography.Text>
                )}
              </div>
            ),
          },
        ]}
      />

      <Modal
        title={`回滚到版本 #${rollbackTarget?.id ?? ""}`}
        open={rollbackTarget !== null}
        okText="创建回滚任务"
        okButtonProps={{ loading: rollback.isPending, danger: true, icon: <RollbackOutlined /> }}
        cancelText="取消"
        onOk={() => rollbackTarget && rollback.mutate(rollbackTarget)}
        onCancel={() => setRollbackTarget(null)}
        width={760}
      >
        {rollbackTarget && (
          <Space direction="vertical" style={{ width: "100%" }}>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
              将把 {c.instance_uid} 的生效配置（#{latest?.id}，{latest ? fmtDateTime(latest.created_at) : "-"}）回滚为版本 #
              {rollbackTarget.id}（{fmtDateTime(rollbackTarget.created_at)}）。
            </Typography.Paragraph>
            <DiffView
              oldText={c.effective_config}
              newText={rollbackTarget.yaml}
              maxLines={120}
              height={260}
            />
          </Space>
        )}
      </Modal>
    </div>
  );
}
