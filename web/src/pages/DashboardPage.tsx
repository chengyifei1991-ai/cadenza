// Dashboard（M4）：聚合统计 + 状态分布 + 最近任务速览 + 首次引导。
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Col,
  Progress,
  Row,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { RobotOutlined } from "@ant-design/icons";
import { api } from "../api/client";
import type { Task } from "../api/types";
import { TASK_STATUS, TASK_TYPE_LABEL } from "../lib/status";
import { fmtAgo } from "../lib/time";
import OnboardingModal, { useOnboarding } from "../components/OnboardingModal";

const POLL = 30_000;

export default function DashboardPage() {
  const navigate = useNavigate();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.stats(), refetchInterval: POLL });
  const info = useQuery({ queryKey: ["system-info"], queryFn: () => api.systemInfo() });
  const recent = useQuery({
    queryKey: ["recent-tasks"],
    queryFn: () => api.listTasks({ page: 1, page_size: 5 }),
    refetchInterval: POLL,
  });
  const onboarding = useOnboarding({ auto: true, demo: info.data?.demo_mode });
  const s = stats.data;

  const collectorTotal = s?.collectors.total ?? 0;
  const distribution = [
    { label: "健康", value: s?.collectors.healthy ?? 0, color: "#3f8600" },
    { label: "异常", value: s?.collectors.unhealthy ?? 0, color: "#cf1322" },
    { label: "离线", value: s?.collectors.offline ?? 0, color: "rgba(0,0,0,0.45)" },
    { label: "未知", value: s?.collectors.unknown ?? 0, color: "#d46b08" },
  ];

  const columns: ColumnsType<Task> = [
    { title: "说明", dataIndex: "input", ellipsis: true },
    {
      title: "类型",
      dataIndex: "type",
      width: 76,
      render: (t: Task["type"]) => <Tag>{TASK_TYPE_LABEL[t] ?? t}</Tag>,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (st: Task["status"]) => <Tag color={TASK_STATUS[st]?.color}>{TASK_STATUS[st]?.label ?? st}</Tag>,
    },
    { title: "时间", dataIndex: "created_at", width: 110, render: fmtAgo },
  ];

  return (
    <div>
      <Space style={{ width: "100%", justifyContent: "space-between", marginBottom: 8 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          仪表盘
        </Typography.Title>
        <Space>
          {s && s.tasks.awaiting_approval > 0 && (
            <Alert
              type="warning"
              showIcon
              message={`${s.tasks.awaiting_approval} 个任务待审批`}
              action={<Link to="/tasks">去处理</Link>}
              style={{ marginBottom: 0, padding: "2px 12px" }}
            />
          )}
          <Button icon={<RobotOutlined />} onClick={() => onboarding.openModal()}>
            重新打开引导
          </Button>
        </Space>
      </Space>

      {info.data?.demo_mode && (
        <Alert
          type="info"
          showIcon
          message="演示数据模式（DEMO_MODE=true）"
          description="Collector/任务/审计均为演示数据，审批/回滚不会影响真实集群。"
          style={{ marginBottom: 16 }}
        />
      )}

      <Row gutter={[16, 16]}>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="Collector 总数" value={s?.collectors.total ?? 0} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="任务总数" value={s?.tasks.total ?? 0} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic
              title="已完成 / 失败"
              value={`${s?.tasks.done ?? 0} / ${s?.tasks.failed ?? 0}`}
              valueStyle={{ color: (s?.tasks.failed ?? 0) > 0 ? "#cf1322" : undefined }}
            />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="会话数" value={s?.sessions_total ?? 0} />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={9}>
          <Card title="Collector 状态分布" loading={stats.isLoading}>
            {collectorTotal === 0 ? (
              <Typography.Text type="secondary">
                尚未接入 Collector。可先查看
                <Typography.Link onClick={() => onboarding.openModal()}> 接入引导</Typography.Link>，
                或开启 <Typography.Text code>DEMO_MODE=true</Typography.Text> 体验演示数据。
              </Typography.Text>
            ) : (
              <Space direction="vertical" style={{ width: "100%" }}>
                {distribution.map((d) => (
                  <div key={d.label}>
                    <Space style={{ justifyContent: "space-between", width: "100%" }}>
                      <Typography.Text type="secondary">{d.label}</Typography.Text>
                      <Typography.Text strong>{d.value}</Typography.Text>
                    </Space>
                    <Progress
                      percent={collectorTotal > 0 ? Math.round(((d.value ?? 0) / collectorTotal) * 100) : 0}
                      strokeColor={d.color}
                      showInfo={false}
                    />
                  </div>
                ))}
              </Space>
            )}
          </Card>
        </Col>
        <Col xs={24} lg={15}>
          <Card
            title="最近任务"
            extra={<Link to="/tasks">任务中心</Link>}
            loading={recent.isLoading}
          >
            <Table<Task>
              rowKey="id"
              size="small"
              columns={columns}
              dataSource={recent.data?.items ?? []}
              pagination={false}
              onRow={(record) => ({
                onClick: () => navigate(`/tasks/${record.id}`),
                style: { cursor: "pointer" },
              })}
            />
          </Card>
        </Col>
      </Row>

      <OnboardingModal open={onboarding.open} onClose={onboarding.close} demo={info.data?.demo_mode} />
    </div>
  );
}
