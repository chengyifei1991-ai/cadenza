// 运维效率卡片（F-17）：消费后端 /api/v1/stats/ops 埋点，展示下发成功率、
// 审批等待、下发时长与回滚耗时的窗口化统计（窗口 7/14/30 天可切换）。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, Col, Row, Select, Space, Statistic, Tooltip, Typography } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import { api } from "../api/client";
import { fmtDurationMs } from "../lib/time";

const POLL = 30_000;

const WINDOW_OPTIONS = [
  { value: 7, label: "近 7 天" },
  { value: 14, label: "近 14 天" },
  { value: 30, label: "近 30 天" },
];

/** 成功率配色：≥90% 绿、≥70% 橙、其余红。 */
function rateColor(rate: number): string {
  if (rate >= 0.9) return "#3f8600";
  if (rate >= 0.7) return "#d46b08";
  return "#cf1322";
}

const CALIBER =
  "口径来自任务状态迁移埋点：审批等待 = 进入待审批 → 离开待审批；" +
  "下发时长 = 进入 applying → 终态（done/failed）；成功率 = 已完成 /（已完成 + 失败）。";

export default function OpsEfficiencyCard() {
  const [windowDays, setWindowDays] = useState(7);
  const ops = useQuery({
    queryKey: ["stats-ops", windowDays],
    queryFn: () => api.statsOps(windowDays),
    refetchInterval: POLL,
  });
  const d = ops.data;
  const empty =
    !d || (d.tasks.total === 0 && d.approval_wait_ms.count === 0 && d.dispatch_ms.count === 0);

  return (
    <Card
      title={
        <Space size={6}>
          运维效率
          <Tooltip title={CALIBER}>
            <InfoCircleOutlined style={{ color: "rgba(0,0,0,0.45)" }} data-testid="ops-caliber" />
          </Tooltip>
        </Space>
      }
      loading={ops.isLoading}
      extra={
        <Select
          size="small"
          value={windowDays}
          onChange={setWindowDays}
          options={WINDOW_OPTIONS}
          style={{ width: 104 }}
          data-testid="ops-window"
        />
      }
    >
      {ops.isError ? (
        <Typography.Text type="secondary">运维效率指标暂不可用，请稍后重试。</Typography.Text>
      ) : empty ? (
        <Typography.Text type="secondary">
          近 {windowDays} 天暂无任务数据。发起一次配置变更（生成 / 下发 / 回滚）后，
          这里会显示下发成功率与耗时分布。
        </Typography.Text>
      ) : (
        <Row gutter={[16, 16]}>
          <Col xs={12} md={6}>
            <Statistic
              title="下发成功率"
              value={(d.tasks.success_rate * 100).toFixed(1)}
              suffix="%"
              valueStyle={{ color: rateColor(d.tasks.success_rate) }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              已完成 {d.tasks.done} / 失败 {d.tasks.failed}（窗口内 {d.tasks.total} 个任务）
            </Typography.Text>
          </Col>
          <Col xs={12} md={6}>
            <Statistic title="审批等待 P50" value={fmtDurationMs(d.approval_wait_ms.p50)} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              P90 {fmtDurationMs(d.approval_wait_ms.p90)}｜样本 {d.approval_wait_ms.count}
            </Typography.Text>
          </Col>
          <Col xs={12} md={6}>
            <Statistic title="下发时长 P50" value={fmtDurationMs(d.dispatch_ms.p50)} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              P90 {fmtDurationMs(d.dispatch_ms.p90)}｜样本 {d.dispatch_ms.count}
            </Typography.Text>
          </Col>
          <Col xs={12} md={6}>
            <Statistic title="回滚次数" value={d.rollback.count} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              平均耗时 {fmtDurationMs(d.rollback.avg_duration_ms)}
            </Typography.Text>
          </Col>
        </Row>
      )}
    </Card>
  );
}
