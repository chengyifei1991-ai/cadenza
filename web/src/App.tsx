import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Alert, Card, Layout, List, Space, Tag, Typography } from "antd";
import { api } from "./api/client";

const { Header, Content } = Layout;

interface InfoRow {
  label: string;
  value: ReactNode;
}

// M0 骨架页：展示系统信息与里程碑占位。
// 后续里程碑（M1+）据此替换为 登录/主布局/业务页面（docs/web-frontend-prd.md）。
export default function App() {
  const info = useQuery({
    queryKey: ["system-info"],
    queryFn: () => api.systemInfo(),
  });

  const rows: InfoRow[] = info.data
    ? [
        { label: "后端版本", value: info.data.version },
        {
          label: "鉴权模式",
          value: (
            <Tag color={info.data.auth_mode === "simple" ? "orange" : "default"}>
              {info.data.auth_mode === "simple" ? "登录保护" : "免登"}
            </Tag>
          ),
        },
      ]
    : [];

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Header style={{ color: "#fff", fontSize: 18, fontWeight: 600 }}>
        Cadenza — OpAMP 统一管控台
      </Header>
      <Content style={{ padding: 24 }}>
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          {info.data?.demo_mode && (
            <Alert type="info" showIcon message="演示数据模式（DEMO_MODE=true）已启用" />
          )}
          <Card title="系统信息" loading={info.isLoading}>
            <List size="small" dataSource={rows} renderItem={(item) => (
              <List.Item>
                <Typography.Text type="secondary">{item.label}：</Typography.Text>
                {item.value}
              </List.Item>
            )} />
          </Card>
          <Card title="前端骨架（M0）">
            <Typography.Paragraph type="secondary">
              本页仅为单二进制内嵌联调骨架。页面里程碑按规划推进：M1 登录与布局 →
              M2 Collector/配置编辑器/任务审批/审计 → M3 AI 助手 → M4 仪表盘与 Onboarding。
              详见 <Typography.Text code>docs/web-frontend-prd.md</Typography.Text>。
            </Typography.Paragraph>
          </Card>
        </Space>
      </Content>
    </Layout>
  );
}
