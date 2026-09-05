// AI 助手（M3）：会话列表/历史回读 + 同步对话（/chat）+ LLM 降级重发
// + 回复中的任务 id 联动卡片（跳任务详情审批）。
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Avatar,
  Button,
  Card,
  Empty,
  Input,
  Layout,
  List,
  Skeleton,
  Space,
  Tag,
  Typography,
  message,
} from "antd";
import { Link } from "react-router-dom";
import { CommentOutlined, PlusOutlined, RobotOutlined, SendOutlined } from "@ant-design/icons";
import { api, ApiError } from "../api/client";
import type { ChatMessage, SessionSummary, Task } from "../api/types";
import { TASK_STATUS, TASK_TYPE_LABEL } from "../lib/status";
import { fmtAgo } from "../lib/time";
import { extractTaskIds } from "../lib/assist";

const { Sider, Content } = Layout;

const EXAMPLES = [
  "帮我给 demo-gateway-1 增加 memory_limiter，避免 OOM",
  "检查 demo-api-1 的配置并提议优化",
  "生成一个接收 otlp 并导出到 debug 的 Collector 配置",
];

export default function AssistantPage() {
  const qc = useQueryClient();
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [failedMessage, setFailedMessage] = useState<string | null>(null);
  const [tracked, setTracked] = useState<Task[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  const sessions = useQuery({
    queryKey: ["sessions"],
    queryFn: () => api.listSessions({ page: 1, page_size: 20 }),
  });

  const history = useQuery({
    queryKey: ["session", currentId],
    queryFn: () => api.getSession(currentId as string),
    enabled: Boolean(currentId),
  });
  const messages: ChatMessage[] = history.data?.messages ?? [];

  useEffect(() => {
    // jsdom 无 scrollTo 实现，做存在性守卫（真实浏览器平滑滚动到最新消息）。
    if (scrollRef.current?.scrollTo) {
      scrollRef.current.scrollTo({ top: scrollRef.current.scrollHeight });
    }
  }, [messages.length, sending]);

  const failedMessageRef = useRef<string | null>(null);
  const chat = useMutation({
    mutationFn: (question: string) => api.chat(currentId ?? undefined, question),
    onSuccess: async (resp) => {
      // 回复中提取任务 id 联动（尽力而为：命中则拉任务卡）。
      const ids = extractTaskIds(resp.reply);
      if (ids.length > 0) {
        const found: Task[] = [];
        for (const id of ids.slice(0, 3)) {
          try {
            found.push(await api.getTask(id));
          } catch {
            /* 非任务 id 忽略 */
          }
        }
        if (found.length > 0) setTracked((prev) => [...found, ...prev.filter((t) => !found.some((n) => n.id === t.id))].slice(0, 5));
      }
      if (!currentId && resp.session_id) setCurrentId(resp.session_id);
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["sessions"] }),
        qc.invalidateQueries({ queryKey: ["session", resp.session_id || currentId] }),
      ]);
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError && err.status === 503) {
        setFailedMessage("智能体暂时不可用（LLM 链路故障，不影响手动操作）。可直接重试刚才的问题。");
      } else {
        message.error(err instanceof ApiError ? err.message : "发送失败，请重试");
      }
    },
  });

  const send = async (raw?: string) => {
    const text = (raw ?? input).trim();
    if (!text || sending) return;
    setSending(true);
    failedMessageRef.current = text;
    setFailedMessage(null);
    try {
      await chat.mutateAsync(text);
      setInput("");
    } finally {
      setSending(false);
    }
  };

  const pickSession = async (id: string) => {
    setCurrentId(id);
    setTracked([]);
  };
  const newSession = async () => {
    const resp = await api.createSession();
    setCurrentId(resp.session_id);
    setTracked([]);
    await qc.invalidateQueries({ queryKey: ["sessions"] });
  };

  const listItems = sessions.data?.items ?? [];
  const currentSummary = listItems.find((s) => s.id === currentId);

  return (
    <Layout style={{ background: "transparent", height: "calc(100vh - 110px)" }}>
      <Sider width={260} style={{ background: "transparent" }} breakpoint={undefined}>
        <Button block icon={<PlusOutlined />} style={{ marginBottom: 8 }} onClick={newSession}>
          新建会话
        </Button>
        <Card size="small" bodyStyle={{ padding: 0 }} style={{ maxHeight: "calc(100vh - 210px)", overflow: "auto" }}>
          {sessions.isLoading ? (
            <Skeleton active paragraph={{ rows: 5 }} />
          ) : listItems.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" style={{ padding: 16 }} />
          ) : (
            <List<SessionSummary>
              dataSource={listItems}
              size="small"
              renderItem={(s) => (
                <List.Item
                  onClick={() => pickSession(s.id)}
                  style={{
                    cursor: "pointer",
                    background: s.id === currentId ? "#e6f4ff" : "transparent",
                    padding: "6px 12px",
                  }}
                >
                  <List.Item.Meta
                    title={<Typography.Text ellipsis style={{ fontSize: 13 }}>{s.first_message || "（新会话）"}</Typography.Text>}
                    description={
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {s.message_count} 条 · {fmtAgo(s.last_message_at ?? s.created_at)}
                      </Typography.Text>
                    }
                  />
                </List.Item>
              )}
            />
          )}
        </Card>
      </Sider>
      <Content style={{ paddingLeft: 16, display: "flex", flexDirection: "column", minWidth: 0 }}>
        <Card
          size="small"
          title={currentSummary?.first_message || "AI 助手"}
          extra={
            currentSummary ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                会话 {currentId?.slice(0, 8)}…
              </Typography.Text>
            ) : undefined
          }
          style={{ display: "flex", flexDirection: "column", flex: 1 }}
          bodyStyle={{ display: "flex", flexDirection: "column", flex: 1, padding: 12 }}
        >
          <div ref={scrollRef} style={{ flex: 1, overflow: "auto", paddingRight: 8 }}>
            {currentId === null ? (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  <Space direction="vertical">
                    <Typography.Text>用大白话生成/优化 Collector 配置，我会先交给你审批。</Typography.Text>
                    <Space wrap>
                      {EXAMPLES.map((e) => (
                        <Tag
                          key={e}
                          style={{ cursor: "pointer" }}
                          onClick={() => send(e)}
                        >
                          {e.length > 22 ? `${e.slice(0, 22)}…` : e}
                        </Tag>
                      ))}
                    </Space>
                  </Space>
                }
              />
            ) : history.isLoading ? (
              <Skeleton active />
            ) : (
              messages.map((m, i) => (
                <MessageBubble key={i} role={m.role} content={m.content} sending={m.role === "user" && i === messages.length - 1 && sending} />
              ))
            )}
            {sending && (
              <MessageBubble role="assistant" content="" loading />
            )}
            {failedMessage && (
              <Alert
                type="warning"
                showIcon
                style={{ margin: "8px 0" }}
                message="智能体暂时不可用（LLM 链路故障，不影响手动操作）"
                description="可直接重试刚才的问题。"
                action={
                  <Button size="small" onClick={() => void send(failedMessageRef.current ?? undefined)}>
                    重试
                  </Button>
                }
              />
            )}
          </div>

          {tracked.length > 0 && (
            <div style={{ margin: "8px 0" }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                对话创建/关联的任务：
              </Typography.Text>
              <Space wrap style={{ marginTop: 4 }}>
                {tracked.map((t) => (
                  <Tag key={t.id} icon={<CommentOutlined />} style={{ padding: "2px 8px" }}>
                    <Link to={`/tasks/${t.id}`} style={{ textDecoration: "none" }}>
                      [{TASK_TYPE_LABEL[t.type] ?? t.type}] {TASK_STATUS[t.status]?.label ?? t.status}
                      <Typography.Text type="secondary" style={{ marginLeft: 4, fontSize: 12 }}>
                        {t.id.slice(0, 8)}…
                      </Typography.Text>
                    </Link>
                  </Tag>
                ))}
              </Space>
            </div>
          )}

          <Space.Compact style={{ width: "100%", marginTop: 8 }}>
            <Input.TextArea
              aria-label="对话输入"
              value={input}
              placeholder="描述你的配置需求（Shift+Enter 换行，Enter 发送）"
              autoSize={{ minRows: 1, maxRows: 4 }}
              onChange={(e) => setInput(e.target.value)}
              onPressEnter={(e) => {
                if (!e.shiftKey) {
                  e.preventDefault();
                  void send();
                }
              }}
              disabled={sending}
            />
            <Button
              type="primary"
              icon={<SendOutlined />}
              loading={sending}
              disabled={!input.trim()}
              onClick={() => void send()}
              style={{ height: "auto" }}
            >
              发送
            </Button>
          </Space.Compact>
        </Card>
      </Content>
    </Layout>
  );
}

function MessageBubble({ role, content, sending = false, loading = false }: { role: "user" | "assistant"; content: string; sending?: boolean; loading?: boolean }) {
  const isUser = role === "user";
  return (
    <div style={{ display: "flex", justifyContent: isUser ? "flex-end" : "flex-start", margin: "6px 0" }}>
      {!isUser && (
        <Avatar size={28} icon={<RobotOutlined />} style={{ marginRight: 8, background: "#1677ff" }} />
      )}
      <div
        style={{
          maxWidth: "76%",
          background: isUser ? "#1677ff" : "#f5f5f5",
          color: isUser ? "#fff" : "rgba(0,0,0,0.88)",
          borderRadius: 10,
          padding: "8px 12px",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
        }}
      >
        {loading ? "…" : content}
        {sending && !loading ? "（发送中…）" : null}
      </div>
    </div>
  );
}
