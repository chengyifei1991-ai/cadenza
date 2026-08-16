#!/usr/bin/env python3
"""MCP streamable HTTP 客户端：initialize → initialized → tools/call。
用法：python3 mcp_client.py <tool_name> '<json_arguments>'"""
import json
import sys
import urllib.request

BASE = "http://192.168.143.1:8080/mcp"
SESSION = None


def post(payload, headers=None):
    req = urllib.request.Request(BASE, data=json.dumps(payload).encode(),
                                 headers={"Content-Type": "application/json",
                                          "Accept": "application/json, text/event-stream",
                                          **(headers or {})})
    # 禁用环境代理（本机/局域网目标不经代理）。
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=15) as resp:
            sess = resp.headers.get("Mcp-Session-Id")
            body = resp.read().decode()
            return resp.status, sess, body
    except urllib.error.HTTPError as e:
        return e.code, e.headers.get("Mcp-Session-Id"), e.read().decode()


def parse_sse(body):
    """解析 SSE 响应，返回最后一条 data 的 JSON。"""
    data = None
    for line in body.splitlines():
        if line.startswith("data:"):
            data = line[5:].strip()
    return json.loads(data) if data else None


def main():
    tool = sys.argv[1]
    if len(sys.argv) > 2 and sys.argv[2].startswith("@"):
        args = json.loads(open(sys.argv[2][1:]).read())
    else:
        args = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}

    # 1. initialize
    code, sess, body = post({"jsonrpc": "2.0", "id": 1, "method": "initialize",
                             "params": {"protocolVersion": "2025-03-26",
                                        "capabilities": {},
                                        "clientInfo": {"name": "test-client", "version": "1.0"}}})
    if code != 200:
        print(f"initialize 失败: {code} {body}")
        sys.exit(1)
    global SESSION
    SESSION = sess
    init = parse_sse(body) or json.loads(body)
    if init and init.get("result"):
        print(f"initialize OK: serverInfo={init['result'].get('serverInfo')}")

    # 2. initialized 通知
    post({"jsonrpc": "2.0", "method": "notifications/initialized",
          "params": {}}, {"Mcp-Session-Id": SESSION})

    # 3. tools/call
    code, _, body = post({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
                          "params": {"name": tool, "arguments": args}},
                         {"Mcp-Session-Id": SESSION})
    if code != 200:
        print(f"tools/call 失败: {code} {body}")
        sys.exit(1)
    result = parse_sse(body) or json.loads(body)
    if result and "result" in result and "content" in result["result"]:
        for c in result["result"]["content"]:
            print(c.get("text", ""))
        if result["result"].get("isError"):
            print("[isError=True]")
    else:
        print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
