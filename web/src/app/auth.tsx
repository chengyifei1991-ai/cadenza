// 登录态上下文：会话校验（/auth/me）、登录、登出与路由守卫。
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { Button, Result, Spin } from "antd";
import { api, ApiError } from "../api/client";
import type { AuthMode } from "../api/types";

export type BootState = "loading" | "ready" | "error";

export interface AuthState {
  /** loading：正在校验会话；ready：可用；error：后端不可达且无法判定模式（需重试） */
  boot: BootState;
  /** null 表示未登录（simple 模式）；off 模式恒为 username="user" */
  user: string | null;
  mode: AuthMode;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  retry: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [boot, setBoot] = useState<BootState>("loading");
  const [user, setUser] = useState<string | null>(null);
  const [mode, setMode] = useState<AuthMode>("simple");
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let alive = true;
    setBoot("loading");
    api
      .me()
      .then((me) => {
        if (!alive) return;
        setUser(me.username);
        setMode(me.auth_mode ?? "simple");
        setBoot("ready");
      })
      .catch(async (err: unknown) => {
        if (!alive) return;
        if (err instanceof ApiError && err.status === 401) {
          // 未登录（simple 模式才会返回 401）。
          setUser(null);
          setMode("simple");
          setBoot("ready");
          return;
        }
        // 后端不可达/5xx：尝试以公开的 /system/info 判定真实鉴权模式；
        // 仍失败则进入 error 态（AuthGate 显示重试），绝不静默按免登放行。
        try {
          const info = await api.systemInfo();
          if (!alive) return;
          if (info.auth_mode === "off") {
            setUser("user");
            setMode("off");
            setBoot("ready");
            return;
          }
          // 后端可达且为 simple：交还登录流程（user=null）。
          setUser(null);
          setMode("simple");
          setBoot("ready");
        } catch {
          if (!alive) return;
          setBoot("error");
        }
      });
    return () => {
      alive = false;
    };
  }, [nonce]);

  const retry = useCallback(() => setNonce((n) => n + 1), []);

  const login = useCallback(async (username: string, password: string) => {
    const me = await api.login(username, password);
    setUser(me.username);
    setMode(me.auth_mode ?? "simple");
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setUser(null);
    }
  }, []);

  const value = useMemo(
    () => ({ boot, user, mode, login, logout, retry }),
    [boot, user, mode, login, logout, retry],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth 必须在 AuthProvider 内使用");
  return ctx;
}

/** 鉴权就绪门：loading 转圈；error 展示重试（不静默放行）。 */
export function AuthGate({ children }: { children: ReactNode }) {
  const { boot, retry } = useAuth();
  if (boot === "loading") {
    return (
      <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <Spin size="large" tip="加载中…">
          <div style={{ width: 160, height: 80 }} />
        </Spin>
      </div>
    );
  }
  if (boot === "error") {
    return (
      <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <Result
          status="warning"
          title="无法连接服务端"
          subTitle="请确认服务已启动（go run ./cmd/server 或 bin/cadenza）后重试。"
          extra={
            <Button type="primary" onClick={retry}>
              重试
            </Button>
          }
        />
      </div>
    );
  }
  return <>{children}</>;
}

/** 受保护布局守卫：simple 且未登录 → 重定向登录页 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { mode, user } = useAuth();
  const location = useLocation();
  if (mode === "simple" && !user) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  return <>{children}</>;
}

/** 登录页守卫：已登录访问 /login 时回首页 */
export function RedirectIfAuthed({ children }: { children: ReactNode }) {
  const { mode, user } = useAuth();
  if (mode === "off" || user) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
}

/** 是否处于“有登录概念”的模式（off 免登模式下应隐藏登出等入口）。 */
export function useLoginEnabled(): boolean {
  const { mode } = useAuth();
  return mode === "simple";
}
