// 设置页（M1 简化）：系统信息/鉴权模式展示 + 登出入口。
// 后端版本与 LLM 链路健康等扩展项在 M4（docs/web-frontend-prd.md §5.10）。
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Card, Descriptions, Space, Tag, Typography } from "antd";
import { LogoutOutlined } from "@ant-design/icons";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { RobotOutlined } from "@ant-design/icons";
import OnboardingModal, { useOnboarding } from "../components/OnboardingModal";
import { useAuth } from "../app/auth";

export default function SettingsPage() {
  const { user, mode, logout } = useAuth();
  const navigate = useNavigate();
  const info = useQuery({ queryKey: ["system-info"], queryFn: () => api.systemInfo() });
  const loginEnabled = mode === "simple";

  const onboarding = useOnboarding({ auto: false });
  const onLogout = async () => {
    await logout();
    navigate("/login", { replace: true });
  };

  return (
    <div>
      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        {info.data?.demo_mode && (
          <Alert type="info" showIcon message="演示数据模式已启用（DEMO_MODE=true）" />
        )}
        <Card title="系统信息" loading={info.isLoading} style={{ maxWidth: 640 }}>
          <Descriptions column={1}>
            <Descriptions.Item label="版本">{info.data?.version ?? "-"}</Descriptions.Item>
            <Descriptions.Item label="鉴权模式">
              <Tag color={loginEnabled ? "orange" : "default"}>
                {loginEnabled ? "登录保护" : "免登模式"}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="当前用户">{user ?? "-"}</Descriptions.Item>
          </Descriptions>
          <Space style={{ marginTop: 16 }}>
            {loginEnabled ? (
              <Button icon={<LogoutOutlined />} onClick={onLogout}>
                退出登录
              </Button>
            ) : (
              <Alert type="info" showIcon message="免登模式（WEB_AUTH_MODE=off），无需登录/登出。" />
            )}
            <Button icon={<RobotOutlined />} onClick={onboarding.openModal}>
              重新打开引导
            </Button>
          </Space>
        </Card>
        <Card title="AI 助手说明" style={{ maxWidth: 640 }}>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            AI 助手由服务端 LLM 稳定性链路驱动（超时/重试/熔断/主备与本地多模型 failover）。
            链路故障只会影响“对话生成/优化”，Collector 查看、手动编辑下发、审批与回滚均不受影响。
          </Typography.Paragraph>
        </Card>
        <OnboardingModal open={onboarding.open} onClose={onboarding.close} />
      </Space>
    </div>
  );
}
