// 设置页（M1 简化）：系统信息/鉴权模式展示 + 登出入口。
// 后端版本与 LLM 链路健康等扩展项在 M4（docs/web-frontend-prd.md §5.10）。
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Card, Descriptions, Space, Tag } from "antd";
import { LogoutOutlined } from "@ant-design/icons";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../app/auth";

export default function SettingsPage() {
  const { user, mode, logout } = useAuth();
  const navigate = useNavigate();
  const info = useQuery({ queryKey: ["system-info"], queryFn: () => api.systemInfo() });
  const loginEnabled = mode === "simple";

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
          </Space>
        </Card>
      </Space>
    </div>
  );
}
