// 登录态上下文：会话校验（/auth/me）、登录、登出与路由守卫。
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { Spin } from "antd";
import { api, ApiError } from "../api/client";
import type { AuthMode } from "../api/types";

export interface AuthState {
  ready: boolean;
  /** null 表示未登录（simple 模式）；off 模式恒为 username="user" */
  user: string | null;
  mode: AuthMode;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [user, setUser] = useState<string | null>(null);
  const [mode, setMode] = useState<AuthMode>("simple");

  useEffect(() => {
    let alive = true;
    api
      .me()
      .then((me) => {
        if (!alive) return;
        setUser(me.username);
        if (me.auth_mode) setMode(me.auth_mode);
      })
      .catch((err: unknown) => {
        if (!alive) return;
        if (err instanceof ApiError && err.status === 401) {
          // 未登录（simple 模式才会返回 401）。
          setUser(null);
          setMode("simple");
        } else {
          // 后端不可达等：以 system/info 判定模式，允许进入后页面自行报错。
          setMode("off");
          setUser("user");
        }
      })
      .finally(() => {
        if (alive) setReady(true);
      });
    return () => {
      alive = false;
    };
  }, []);

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
    () => ({ ready, user, mode, login, logout }),
    [ready, user, mode, login, logout],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth 必须在 AuthProvider 内使用");
  return ctx;
}

/** 鉴权就绪门（闪烁保护） */
export function AuthGate({ children }: { children: ReactNode }) {
  const { ready } = useAuth();
  if (!ready) {
    return (
      <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <Spin size="large" tip="加载中…">
          <div style={{ width: 160, height: 80 }} />
        </Spin>
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
