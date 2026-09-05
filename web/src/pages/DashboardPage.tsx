// Dashboard（M1 骨架先挂 /stats 聚合；M4 补全图表与速览）。
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Col, Row, Statistic, Typography } from "antd";
import { api } from "../api/client";

const POLL = 30_000;

export default function DashboardPage() {
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.stats(), refetchInterval: POLL });
  const info = useQuery({ queryKey: ["system-info"], queryFn: () => api.systemInfo() });
  const s = stats.data;

  return (
    <div>
      <Typography.Title level={4}>仪表盘</Typography.Title>
      {info.data?.demo_mode && (
        <Alert
          type="info"
          showIcon
          message="演示数据模式（DEMO_MODE=true）"
          description="当前展示的 Collector/任务/审计均为演示数据，审批/回滚不会影响真实集群。"
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
            <Statistic title="健康" value={s?.collectors.healthy ?? 0} valueStyle={{ color: "#3f8600" }} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="异常 / 离线" value={(s?.collectors.unhealthy ?? 0) + (s?.collectors.offline ?? 0)} valueStyle={{ color: "#cf1322" }} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="会话数" value={s?.sessions_total ?? 0} />
          </Card>
        </Col>
      </Row>
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="任务总数" value={s?.tasks.total ?? 0} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic
              title={
                <Link to="/tasks">
                  待审批 {s && s.tasks.awaiting_approval > 0 ? <Typography.Text type="danger">●</Typography.Text> : null}
                </Link>
              }
              value={s?.tasks.awaiting_approval ?? 0}
            />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="已完成" value={s?.tasks.done ?? 0} valueStyle={{ color: "#3f8600" }} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card loading={stats.isLoading}>
            <Statistic title="失败" value={s?.tasks.failed ?? 0} valueStyle={{ color: "#cf1322" }} />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
