// Collector 配置编辑器（M2）：基于当前生效配置编辑 → 客户端语法预检（js-yaml）→
// "保存并下发"（POST /tasks/apply）→ 跳任务详情（审批/跟踪）。
// 权威校验仍由服务端两级校验完成（design-web-p0.md P0-6）。
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Card, Input, Space, Tag, Typography } from "antd";
import { ArrowLeftOutlined, ExperimentOutlined, SendOutlined } from "@ant-design/icons";
import { load } from "js-yaml";
import { api, ApiError } from "../api/client";
import { diffText } from "../lib/diff";
interface Preflight {
  ok: boolean;
  message?: string;
}

export default function CollectorEditPage() {
  const { uid = "" } = useParams();
  const navigate = useNavigate();

  const collector = useQuery({
    queryKey: ["collector", uid],
    queryFn: () => api.getCollector(uid),
    enabled: Boolean(uid),
  });

  const [text, setText] = useState("");
  const [dirty, setDirty] = useState(false);
  const [preflight, setPreflight] = useState<Preflight | null>(null);
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // 首次装载：以当前生效配置为基底。
  const base = collector.data?.effective_config ?? "";
  useEffect(() => {
    if (!dirty) {
      setText(base);
    }
  }, [base, dirty]);

  const changedCount = useMemo(() => {
    const r = diffText(base, text);
    return { added: r.added, removed: r.removed, changed: r.changed };
  }, [base, text]);

  const runPreflight = () => {
    try {
      load(text, { json: false });
      setPreflight({ ok: true, message: "YAML 语法正确（服务端将执行深度校验）。" });
    } catch (err) {
      setPreflight({ ok: false, message: err instanceof Error ? err.message : "YAML 语法错误" });
    }
  };

  const submit = async () => {
    setSubmitting(true);
    setSubmitError(null);
    try {
      const task = await api.applyTask(uid, text, note.trim() || undefined);
      navigate(`/tasks/${task.id}`, { state: { justCreated: true } });
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : "提交失败，请重试");
    } finally {
      setSubmitting(false);
    }
  };

  if (collector.isLoading) return <Card loading />;
  if (collector.isError || !collector.data) {
    return (
      <Alert
        type="error"
        showIcon
        message="Collector 不存在或加载失败"
        action={<Link to="/collectors">返回列表</Link>}
      />
    );
  }
  const c = collector.data;

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Link to={`/collectors/${encodeURIComponent(uid)}`}>
          <ArrowLeftOutlined /> 返回详情
        </Link>
      </Space>
      <Typography.Title level={4} style={{ wordBreak: "break-all" }}>
        编辑配置 — {c.instance_uid}
      </Typography.Title>
      <Card
        size="small"
        title={
          <Space>
            <Tag color={c.status === "healthy" ? "green" : "orange"}>{c.status}</Tag>
            <span style={{ fontSize: 13 }}>{c.hostname}</span>
          </Space>
        }
        extra={
          !dirty ? (
            <Typography.Text type="secondary">内容与当前生效配置一致</Typography.Text>
          ) : (
            <Space>
              <Tag color={changedCount.changed ? "processing" : "default"}>
                ±{changedCount.added}/{changedCount.removed} 行
              </Tag>
              <Button size="small" onClick={() => setText(base)}>
                还原为当前配置
              </Button>
            </Space>
          )
        }
      >
        <textarea
          aria-label="Collector YAML 配置"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setDirty(true);
            setPreflight(null);
          }}
          spellCheck={false}
          style={{
            width: "100%",
            minHeight: 420,
            fontFamily: "'SFMono-Regular', Consolas, monospace",
            fontSize: 12,
            lineHeight: 1.6,
            border: "1px solid #d9d9d9",
            borderRadius: 4,
            padding: 8,
            boxSizing: "border-box",
          }}
        />
        {submitError && (
          <Alert type="error" showIcon message="保存下发失败" description={submitError} style={{ marginTop: 12 }} />
        )}
        {preflight && (
          <Alert
            type={preflight.ok ? "success" : "error"}
            showIcon
            message={preflight.message}
            style={{ marginTop: 12 }}
          />
        )}
        <div style={{ marginTop: 12 }}>
          <Space.Compact style={{ width: "100%" }}>
            <Input
              placeholder="变更说明（可选，写入任务与审计）"
              value={note}
              maxLength={200}
              onChange={(e) => setNote(e.target.value)}
            />
          </Space.Compact>
          <Space style={{ marginTop: 12 }}>
            <Button icon={<ExperimentOutlined />} onClick={runPreflight}>
              语法预检
            </Button>
            <Button
              type="primary"
              icon={<SendOutlined />}
              loading={submitting}
              disabled={preflight !== null && !preflight.ok}
              onClick={submit}
            >
              保存并下发
            </Button>
          </Space>
        </div>
      </Card>
    </div>
  );
}
