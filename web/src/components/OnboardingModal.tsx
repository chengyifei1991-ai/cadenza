// Onboarding 引导（M4）：首次进入展示 4 步引导（含 Collector 接入示例复制）。
import { useCallback, useState } from "react";
import { Alert, Button, Modal, Steps, Typography } from "antd";
import { CopyOutlined } from "@ant-design/icons";
import { buildCollectorSnippet, markOnboardingDone, shouldShowOnboarding } from "../lib/assist";

interface Options {
  /** 是否在首次进入时自动弹出（Dashboard=true，其他入口传 false 仅手动触发） */
  auto?: boolean;
  demo?: boolean;
}

function lsAdapter() {
  return {
    get: (k: string) => (typeof window === "undefined" ? null : window.localStorage.getItem(k)),
    set: (k: string, v: string) => {
      if (typeof window !== "undefined") window.localStorage.setItem(k, v);
    },
  };
}

export function useOnboarding(opts: Options = {}) {
  const [open, setOpen] = useState<boolean>(() => (opts.auto ? shouldShowOnboarding(lsAdapter()) : false));
  const openModal = useCallback(() => setOpen(true), []);
  const close = useCallback(() => {
    markOnboardingDone(lsAdapter());
    setOpen(false);
  }, []);
  return { open, openModal, close };
}

export default function OnboardingModal({ open, onClose, demo }: { open: boolean; onClose: () => void; demo?: boolean }) {
  const [step, setStep] = useState(0);
  const [copied, setCopied] = useState(false);

  const snippet = buildCollectorSnippet(
    typeof window !== "undefined" ? window.location.host : "localhost:8080",
  );

  const copySnippet = async () => {
    try {
      await navigator.clipboard.writeText(snippet);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* 非安全上下文忽略 */
    }
  };

  const steps = [
    {
      title: "欢迎",
      content: (
        <Typography.Paragraph>
          Cadenza 是 OpAMP 统一管控台：接入 OpenTelemetry Collector 集群后，你可以在本页面
          查看健康、用对话/编辑器生成配置、先审批后下发，全程可回滚可审计。
        </Typography.Paragraph>
      ),
    },
    {
      title: "接入一个 Collector",
      content: (
        <div>
          <Typography.Paragraph>
            在你的 Collector 配置中加入 opamp extension（本机浏览器与容器场景请将
            <Typography.Text code>endpoint</Typography.Text>改为可达地址；演示数据可跳过本步）：
          </Typography.Paragraph>
          {demo && <Alert type="info" showIcon message="当前为演示模式（DEMO_MODE=true），可直接点下一步体验审批/回滚。" style={{ marginBottom: 12 }} />}
          <pre style={{ background: "#fafafa", border: "1px solid #f0f0f0", borderRadius: 4, padding: 12, fontSize: 12, overflow: "auto" }}>{snippet}</pre>
          <Button icon={<CopyOutlined />} onClick={copySnippet} disabled={copied}>
            {copied ? "已复制" : "复制示例"}
          </Button>
        </div>
      ),
    },
    {
      title: "体验配置闭环",
      content: (
        <Typography.Paragraph>
          3 分钟体验：到 <Typography.Text strong>Collectors</Typography.Text> 查看集群健康 →
          进入某实例 <Typography.Text strong>编辑配置</Typography.Text> 做小改动 →
          <Typography.Text strong>保存并下发</Typography.Text> 进入待审批 →
          到 <Typography.Text strong>任务中心</Typography.Text> 审批/拒绝 → 出问题时在版本历史
          <Typography.Text strong>回滚</Typography.Text>。
        </Typography.Paragraph>
      ),
    },
    {
      title: "用 AI 助手",
      content: (
        <Typography.Paragraph>
          不想手写 YAML？打开 <Typography.Text strong>AI 助手</Typography.Text>，用大白话描述需求，
          例如“给网关加内存限制，避免 OOM”——生成结果会经校验后交给你审批。
        </Typography.Paragraph>
      ),
    },
  ];

  return (
    <Modal
      open={open}
      title="欢迎使用 Cadenza"
      width={680}
      okText={step === steps.length - 1 ? "开始体验" : "下一步"}
      cancelText={step === 0 ? "关闭（不再显示）" : "上一步"}
      onCancel={() => {
        if (step === 0) onClose();
        else setStep((s) => s - 1);
      }}
      onOk={() => {
        if (step < steps.length - 1) setStep((s) => s + 1);
        else onClose();
      }}
      destroyOnClose
    >
      <Steps current={step} size="small" items={steps.map((s) => ({ title: s.title }))} style={{ marginBottom: 16 }} />
      <div style={{ minHeight: 140 }}>{steps[step].content}</div>
    </Modal>
  );
}
