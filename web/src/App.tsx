// 根组件：Provider + 路由（BrowserRouter，生产由后端 SPA fallback 支持）。
// 页面按路由懒加载（React.lazy），配合 vite manualChunks 实现拆包（评审 F-M1-5）。
import { lazy, Suspense } from "react";
import { ConfigProvider, Spin } from "antd";
import zhCN from "antd/locale/zh_CN";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AuthGate, AuthProvider, RedirectIfAuthed, RequireAuth } from "./app/auth";
import AppShell from "./app/AppShell";
import PlaceholderPage from "./pages/PlaceholderPage";

const LoginPage = lazy(() => import("./pages/LoginPage"));
const DashboardPage = lazy(() => import("./pages/DashboardPage"));
const CollectorsPage = lazy(() => import("./pages/CollectorsPage"));
const CollectorDetailPage = lazy(() => import("./pages/CollectorDetailPage"));
const SettingsPage = lazy(() => import("./pages/SettingsPage"));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

function RouteFallback() {
  return (
    <div style={{ minHeight: "60vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
      <Spin size="large" />
    </div>
  );
}

function AppRoutes() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <Routes>
        <Route
          path="/login"
          element={
            <AuthGate>
              <RedirectIfAuthed>
                <LoginPage />
              </RedirectIfAuthed>
            </AuthGate>
          }
        />
        <Route
          element={
            <AuthGate>
              <RequireAuth>
                <AppShell />
              </RequireAuth>
            </AuthGate>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="collectors" element={<CollectorsPage />} />
          <Route path="collectors/:uid" element={<CollectorDetailPage />} />
          <Route
            path="tasks"
            element={
              <PlaceholderPage
                title="任务中心"
                milestone="M2"
                description="任务状态流转、diff 审批与回滚操作"
              />
            }
          />
          <Route
            path="audit"
            element={<PlaceholderPage title="审计" milestone="M2" description="审计日志检索与过滤" />}
          />
          <Route
            path="assistant"
            element={
              <PlaceholderPage
                title="AI 助手"
                milestone="M3"
                description="对话生成配置、会话历史与结果 diff"
              />
            }
          />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="*" element={<PlaceholderPage title="页面不存在" milestone="-" description="" status="404" />} />
        </Route>
      </Routes>
    </Suspense>
  );
}

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <AuthProvider>
            <AppRoutes />
          </AuthProvider>
        </BrowserRouter>
      </QueryClientProvider>
    </ConfigProvider>
  );
}
