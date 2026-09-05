// 未到里程碑的页面占位（任务中心 M2 / 审计 M2 / AI 助手 M3 / 404）。
import { Result } from "antd";
import { Link } from "react-router-dom";

interface Props {
  title: string;
  milestone: string;
  description: string;
  status?: "404" | "info";
}

export default function PlaceholderPage({ title, milestone, description, status = "info" }: Props) {
  if (status === "404") {
    return <Result status="404" title="页面不存在" extra={<Link to="/">返回仪表盘</Link>} />;
  }
  return (
    <Result
      icon={null}
      title={title}
      subTitle={`${description}（规划于 ${milestone}，见 docs/web-frontend-prd.md）`}
      extra={<Link to="/">返回仪表盘</Link>}
    />
  );
}
