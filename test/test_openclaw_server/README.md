# test_openclaw_server

用于对接 `test/xiaozhi_openclaw/xiaozhi-integration/openclaw-channel` 插件的 WebSocket 测试服务。

## 功能

- 提供 `/ws/openclaw` WebSocket 端点（校验 `user_id/agent_id/endpoint_id` token）。
- 连接建立后主动发送 `handshake_ack`。
- 接收插件 `ping` 并回复 `pong`。
- 接收插件 `response`，记录回包（包含 `metadata.device_id`）。
- 提供 HTTP API 主动向插件发送 `message`。

## 启动

```bash
go run ./test/test_openclaw_server -addr :18080 -jwt-secret xiaozhi_admin_secret_key
```

输出详细 WebSocket 调试日志：

```bash
go run ./test/test_openclaw_server -addr :18080 -jwt-secret xiaozhi_admin_secret_key -verbose
```

## 生成 token（测试）

```bash
node test/xiaozhi_openclaw/xiaozhi-integration/generate-token.js 1 main agent_main
```

或显式指定与服务端一致的密钥（推荐）：

```bash
JWT_SECRET=xiaozhi_admin_secret_key \
node test/xiaozhi_openclaw/xiaozhi-integration/generate-token.js 1 main agent_main
```

把输出的 `Token` 配到插件配置里：

- `channels.xiaozhi.url = ws://127.0.0.1:18080/ws/openclaw`
- `channels.xiaozhi.token = <token>`
- `JWT_SECRET` 必须和 `go run ./test/test_openclaw_server -jwt-secret ...` 完全一致，否则会报 `signature is invalid`

## HTTP API

### 1) 健康检查

```bash
curl -sS http://127.0.0.1:18080/healthz | jq
```

### 1.1) 鉴权调试（排查 401）

```bash
curl -sS "http://127.0.0.1:18080/debug/ws-auth?token=<token>" | jq
```

也支持从请求头带 token：

```bash
curl -sS "http://127.0.0.1:18080/debug/ws-auth" \
  -H "Authorization: Bearer <token>" | jq
```

### 2) 查看当前连接

```bash
curl -sS http://127.0.0.1:18080/api/connections | jq
```

### 3) 发送测试消息给插件

```bash
curl -sS -X POST http://127.0.0.1:18080/api/send \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"main",
    "device_id":"esp32-001",
    "content":"请回复一条测试消息",
    "session_id":"test-session-1"
  }' | jq
```

多连接模式下可指定 `conn_id` 精确发送：

```bash
curl -sS -X POST http://127.0.0.1:18080/api/send \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id":"main",
    "conn_id":"main-2",
    "device_id":"esp32-001",
    "content":"发给指定连接"
  }' | jq
```

### 4) 查看插件回包

```bash
curl -sS "http://127.0.0.1:18080/api/responses?limit=20" | jq
```

可按 agent 过滤：

```bash
curl -sS "http://127.0.0.1:18080/api/responses?agent_id=main&limit=20" | jq
```
