// 主布局：侧栏导航 + 顶栏用户区（AntD Layout），业务页经 <Outlet/> 渲染。
import { useMemo } from "react";
import { Link, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  AuditOutlined,
  CommentOutlined,
  DashboardOutlined,
  LogoutOutlined,
  SettingOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import { Dropdown, Layout, Menu, Tag, Typography } from "antd";
import type { MenuProps } from "antd";
import { useAuth } from "./auth";

const { Header, Sider, Content } = Layout;

type MenuItem = Required<MenuProps>["items"][number];

const MENU: MenuItem[] = [
  { key: "/", icon: <DashboardOutlined />, label: <Link to="/">仪表盘</Link> },
  { key: "/collectors", icon: <UnorderedListOutlined />, label: <Link to="/collectors">Collectors</Link> },
  { key: "/tasks", icon: <CommentOutlined />, label: <Link to="/tasks">任务中心</Link> },
  { key: "/audit", icon: <AuditOutlined />, label: <Link to="/audit">审计</Link> },
  { key: "/assistant", icon: <CommentOutlined />, label: <Link to="/assistant">AI 助手</Link> },
  { key: "/settings", icon: <SettingOutlined />, label: <Link to="/settings">设置</Link> },
];

export default function AppShell() {
  const { user, mode, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();

  // 侧栏高亮：匹配当前一级路径。
  const selected = useMemo(() => {
    const seg = "/" + location.pathname.split("/")[1];
    return MENU.some((m) => m && typeof m === "object" && "key" in m && m.key === seg) ? seg : "/";
  }, [location.pathname]);

  const userMenu: MenuProps = {
    items: [{ key: "logout", icon: <LogoutOutlined />, label: "退出登录" }],
    onClick: async () => {
      await logout();
      navigate("/login", { replace: true });
    },
  };

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider breakpoint="lg" collapsedWidth={0}>
        <div style={{ color: "#fff", padding: 16, fontWeight: 700, fontSize: 16 }}>Cadenza</div>
        <Menu theme="dark" mode="inline" selectedKeys={[selected]} items={MENU} />
      </Sider>
      <Layout>
        <Header
          style={{
            background: "#fff",
            padding: "0 24px",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            borderBottom: "1px solid #f0f0f0",
          }}
        >
          <Typography.Text type="secondary">OpAMP 统一管控台</Typography.Text>
          <Dropdown menu={userMenu} placement="bottomRight">
            <span style={{ cursor: "pointer" }}>
              {user ?? "未登录"}
              {mode === "off" && <Tag style={{ marginLeft: 8 }}>免登模式</Tag>}
            </span>
          </Dropdown>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
