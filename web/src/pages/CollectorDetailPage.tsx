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
  Drawer,
  Tabs,
  message,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeftOutlined, EditOutlined, HistoryOutlined, RollbackOutlined } from "@ant-design/icons";
import { api, ApiError } from "../api/client";
import type { ConfigVersion, CollectorStatus, GitCommit } from "../api/types";
import { COLLECTOR_STATUS } from "../lib/status";
import { fmtAgo, fmtDateTime } from "../lib/time";
import DiffView from "../components/DiffView";

export default function CollectorDetailPage() {
  const { uid = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [rollbackTarget, setRollbackTarget] = useState<ConfigVersion | null>(null);
  // GitOps（可选模式）：Git 历史抽屉与按提交操作的反馈。
  const [gitOpen, setGitOpen] = useState(false);
  const [gitPreview, setGitPreview] = useState<{ commit: string; yaml: string } | null>(null);
  const sysInfo = useQuery({ queryKey: ["system-info"], queryFn: api.systemInfo });
  const gitEnabled = Boolean(sysInfo.data?.git_enabled);
  const gitCommits = useQuery({
    queryKey: ["git-commits", uid],
    queryFn: () => api.getGitCommits(uid, 20),
    enabled: gitEnabled && gitOpen,
  });
  const applyFromGit = useMutation({
    mutationFn: (sha: string) => api.applyFromGitRef(uid, sha, `GitOps 下发 ${sha.slice(0, 8)}`),
    onSuccess: async (t) => {
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      message.success("已创建下发任务（来源 git），请到任务中心审批");
      navigate(`/tasks/${t.id}`);
    },
    onError: (err: unknown) => message.error(err instanceof ApiError ? err.message : "下发失败"),
  });
  const rollbackFromGit = useMutation({
    mutationFn: (sha: string) => api.rollbackToGitCommit(uid, sha),
    onSuccess: async (t) => {
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      message.success("已创建回退任务（来源 git commit），请到任务中心审批");
      navigate(`/tasks/${t.id}`);
    },
    onError: (err: unknown) => message.error(err instanceof ApiError ? err.message : "回退失败"),
  });

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
        <div>
          {gitEnabled && (
            <Button
              style={{ marginRight: 8 }}
              icon={<HistoryOutlined />}
              onClick={() => setGitOpen(true)}
            >
              Git 历史
            </Button>
          )}
          <Button
            type="primary"
            icon={<EditOutlined />}
            onClick={() => navigate(`/collectors/${encodeURIComponent(uid)}/edit`)}
          >
            编辑配置
          </Button>
        </div>
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
                  message={
                    gitEnabled
                      ? "本实例为 GitOps 模式：配置版本权威在 git 仓库，可用右上角「Git 历史」按提交下发/回退；下表为内置下发记录。"
                      : "回滚将创建一个待审批任务，审批通过后把该历史版本下发给本 Collector（产生新的版本快照）。"
                  }
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
      <Drawer
        title="Git 历史（配置版本权威在 git 仓库）"
        width={760}
        open={gitOpen}
        onClose={() => setGitOpen(false)}
      >
        {gitCommits.isLoading && <Skeleton active />}
        {gitCommits.isError && (
          <Alert
            type="error"
            showIcon
            message="读取 git 历史失败"
            description={String((gitCommits.error as Error)?.message ?? "")}
          />
        )}
        {gitCommits.data && (
          <>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              文件：<Typography.Text code>{gitCommits.data.path}</Typography.Text>（ref：{gitCommits.data.ref}）
            </Typography.Paragraph>
            <Table<GitCommit>
              rowKey="sha"
              size="small"
              pagination={false}
              dataSource={gitCommits.data.items}
              columns={[
                {
                  title: "提交",
                  dataIndex: "short_sha",
                  width: 90,
                  render: (v: string) => <Typography.Text code>{v}</Typography.Text>,
                },
                { title: "说明", dataIndex: "subject", ellipsis: true },
                { title: "作者", dataIndex: "author", width: 90 },
                { title: "时间", dataIndex: "date", width: 150, render: fmtDateTime },
                {
                  title: "操作",
                  width: 220,
                  render: (_: unknown, row: GitCommit) => (
                    <Space size={4}>
                      <Button
                        size="small"
                        onClick={async () => {
                          try {
                            const f = await api.getGitFile(uid, row.sha);
                            setGitPreview({ commit: row.sha, yaml: f.yaml });
                          } catch (e) {
                            message.error(e instanceof ApiError ? e.message : "读取内容失败");
                          }
                        }}
                      >
                        查看
                      </Button>
                      <Button size="small" type="primary" onClick={() => applyFromGit.mutate(row.sha)}>
                        按此提交下发
                      </Button>
                      <Button size="small" danger onClick={() => rollbackFromGit.mutate(row.sha)}>
                        回退到此
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </>
        )}
      </Drawer>
      <Modal
        open={Boolean(gitPreview)}
        title={gitPreview ? `该提交的配置内容（${gitPreview.commit.slice(0, 8)}）` : ""}
        width={760}
        footer={null}
        onCancel={() => setGitPreview(null)}
      >
        <pre style={{ fontSize: 12, maxHeight: 480, overflow: "auto" }}>{gitPreview?.yaml}</pre>
      </Modal>
    </div>
  );
}
