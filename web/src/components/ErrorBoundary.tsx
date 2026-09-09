// 顶层错误边界：渲染/懒加载异常时给出可见提示（替代白屏，便于定位）。
import { Component } from "react";
import type { ErrorInfo, ReactNode } from "react";
import { Button, Result } from "antd";

interface Props {
  children: ReactNode;
}
interface State {
  error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }
  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("[Cadenza] 渲染异常", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
          <Result
            status="error"
            title="页面加载出错"
            subTitle={
              <span style={{ wordBreak: "break-all" }}>{String(this.state.error?.message || this.state.error)}</span>
            }
            extra={
              <Button type="primary" onClick={() => window.location.reload()}>
                刷新重试
              </Button>
            }
          />
        </div>
      );
    }
    return this.props.children;
  }
}
