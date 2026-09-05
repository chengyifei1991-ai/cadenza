// 列表/详情页通用错误态（Alert + 重试），避免请求失败时页面静默空白。
import { Alert } from "antd";

interface Props {
  onRetry?: () => void;
  message?: string;
}

export default function ErrorState({ onRetry, message = "数据加载失败" }: Props) {
  return (
    <Alert
      type="error"
      showIcon
      message={message}
      style={{ marginBottom: 12 }}
      action={onRetry ? <a onClick={onRetry}>重试</a> : undefined}
    />
  );
}
