// 任务详情（M2）：元信息 + 变更 diff（vs 目标当前生效配置）+ 审批/拒绝操作。
// 审批人由后端绑定当前登录用户（design-web-p0.md P0-3）。
import { useMemo, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Input,
  Modal,
  Space,
  Tag,
  Typography,
  message,
} from "antd";
import {
  ArrowLeftOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined,
} from "@ant-design/icons";
import { api, ApiError } from "../api/client";
import type { TaskStatus, TaskType } from "../api/types";
import { TASK_STATUS, TASK_TYPE_LABEL } from "../lib/status";
import { fmtDateTime } from "../lib/time";
import DiffView from "../components/DiffView";

const TERMINAL: TaskStatus[] = ["done", "rejected", "failed"];

export default function TaskDetailPage() {
  const { id = "" } = useParams();
  const location = useLocation();
  const qc = useQueryClient();
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [rejectError, setRejectError] = useState<string | null>(null);

  const justCreated = (location.state as { justCreated?: boolean } | null)?.justCreated;

  const task = useQuery({
    queryKey: ["task", id],
    queryFn: () => api.getTask(id),
    enabled: Boolean(id),
    refetchInterval: (q) => (q.state.data && TERMINAL.includes(q.state.data.status) ? false : 5000),
  });
  const t = task.data;

  // diff 底座：目标为单实例时取其实时生效配置。
  const targetUid = t?.target_instance_uid || t?.target_group_id || "";
  const targetCollector = useQuery({
    queryKey: ["collector", targetUid],
    queryFn: () => api.getCollector(targetUid),
    enabled: Boolean(targetUid),
    retry: false,
  });

  const approve = useMutation({
    mutationFn: () => api.approveTask(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["task", id] });
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      message.success("已审批并触发下发");
    },
    onError: (err: unknown) => {
      message.error(err instanceof ApiError ? err.message : "审批失败，请重试");
    },
  });

  const reject = useMutation({
    mutationFn: (reason: string) => api.rejectTask(id, reason),
    onSuccess: async () => {
      setRejectOpen(false);
      setRejectReason("");
      await qc.invalidateQueries({ queryKey: ["task", id] });
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      message.success("已拒绝该任务");
    },
    onError: (err: unknown) => {
      setRejectError(err instanceof ApiError ? err.message : "拒绝失败，请重试");
    },
  });

  const diff = useMemo(() => {
    if (!t) return null;
    const next = t.generated_yaml;
    const base = targetCollector.data?.effective_config;
    if (!next || !base) return null;
    return { oldText: base, newText: next };
  }, [t, targetCollector.data]);

  const canReview = t?.status === "awaiting_approval";

  if (task.isLoading) return <Card loading />;
  if (task.isError || !t) {
    return (
      <Alert type="error" showIcon message="任务不存在或加载失败" action={<Link to="/tasks">返回任务中心</Link>} />
    );
  }

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Link to="/tasks">
          <ArrowLeftOutlined /> 返回任务中心
        </Link>
      </Space>
      {justCreated && (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 12 }}
          message="任务已创建"
          description={canReview ? "变更已通过校验，等待审批后下发。" : "任务处理中，请关注状态。"}
        />
      )}
      {t.status === "failed" && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message="任务失败" description={t.error || "未知错误"} />
      )}
      <Typography.Title level={4} style={{ wordBreak: "break-all" }}>
        任务 {t.id}
      </Typography.Title>

      <Card title="任务信息" style={{ marginBottom: 16 }}>
        <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
          <Descriptions.Item label="类型">
            <Tag>{TASK_TYPE_LABEL[t.type as TaskType] ?? t.type}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            <Tag color={TASK_STATUS[t.status as TaskStatus]?.color}>
              {TASK_STATUS[t.status as TaskStatus]?.label ?? t.status}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="目标">
            {t.target_group_id || t.target_instance_uid || "-"}
          </Descriptions.Item>
          <Descriptions.Item label="审批人">{t.approver || "-"}</Descriptions.Item>
          {t.reject_reason && <Descriptions.Item label="拒绝原因">{t.reject_reason}</Descriptions.Item>}
          {t.error && <Descriptions.Item label="错误">{t.error}</Descriptions.Item>}
          {t.model_used && <Descriptions.Item label="模型">{t.model_used}</Descriptions.Item>}
          {t.rollback_version_id ? (
            <Descriptions.Item label="回滚目标">版本 #{t.rollback_version_id}</Descriptions.Item>
          ) : null}
          <Descriptions.Item label="创建时间">{fmtDateTime(t.created_at)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{fmtDateTime(t.updated_at)}</Descriptions.Item>
        </Descriptions>
        <Typography.Paragraph style={{ marginTop: 12, marginBottom: 0 }}>
          <Typography.Text strong>变更说明：</Typography.Text>
          {t.input || "-"}
        </Typography.Paragraph>
      </Card>

      {diff ? (
        <Card title="变更内容（与目标当前生效配置对比）" style={{ marginBottom: 16 }}>
          <DiffView oldText={diff.oldText} newText={diff.newText} maxLines={300} height={380} />
        </Card>
      ) : (
        <Card title="配置内容" style={{ marginBottom: 16 }}>
          {t.generated_yaml ? (
            <pre style={{ fontSize: 12, maxHeight: 380, overflow: "auto" }}>{t.generated_yaml}</pre>
          ) : (
            <Typography.Text type="secondary">本任务无可展示的配置内容。</Typography.Text>
          )}
          {!t.generated_yaml && !targetCollector.data && t.rollback_version_id && (
            <Typography.Text type="secondary">回滚任务的变更目标见版本历史（目标 Collector 详情）。</Typography.Text>
          )}
        </Card>
      )}

      <Card title="操作">
        {canReview ? (
          <Space>
            <Button
              type="primary"
              icon={<CheckCircleOutlined />}
              loading={approve.isPending}
              onClick={() => approve.mutate()}
            >
              审批通过并下发
            </Button>
            <Button danger icon={<CloseCircleOutlined />} onClick={() => setRejectOpen(true)}>
              拒绝
            </Button>
            <Typography.Text type="secondary">
              审批人将记录为当前登录用户，操作进入审计。
            </Typography.Text>
          </Space>
        ) : (
          <Typography.Text type="secondary">
            {TERMINAL.includes(t.status)
              ? "该任务已进入终态，无需操作。"
              : "任务处理中，请稍候刷新…"}
          </Typography.Text>
        )}
      </Card>

      <Modal
        title="拒绝任务"
        open={rejectOpen}
        okText="确认拒绝"
        okButtonProps={{ danger: true, loading: reject.isPending, icon: <ExclamationCircleOutlined /> }}
        onOk={() => rejectReason.trim() && reject.mutate(rejectReason.trim())}
        onCancel={() => {
          setRejectOpen(false);
          setRejectError(null);
        }}
      >
        {rejectError && <Alert type="error" showIcon message={rejectError} style={{ marginBottom: 12 }} />}
        <Input.TextArea
          rows={3}
          placeholder="请输入拒绝原因（必填，将写入任务与审计）"
          value={rejectReason}
          maxLength={500}
          onChange={(e) => setRejectReason(e.target.value)}
        />
      </Modal>
    </div>
  );
}
