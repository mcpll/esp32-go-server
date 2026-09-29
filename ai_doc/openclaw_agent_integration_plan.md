# OpenClaw Agent 维度集成实施方案

## 1. 目标

基于 `XIAOZHI_OPENCLAW_PROTOCOL.md`，在 `xiaozhi-esp32-server-golang` 中实现 OpenClaw 集成，要求如下：

1. 控制台不新增独立 OpenClaw 配置页面。
2. OpenClaw endpoint 生成方式与 MCP endpoint 一致（按智能体生成，token 内含 `user_id`、`agent_id`）。
3. 主程序按 `agent_id` 管理 OpenClaw WebSocket 连接。
4. 设备配置通过结构体下发 OpenClaw 配置（允许标记 + 进入/退出关键词）。
5. ASR 后支持进入/退出 OpenClaw 模式；模式内消息绕过 LLM，直接走 OpenClaw，再走 TTS。
6. OpenClaw 响应延迟到达时，设备离线则进入内存离线队列；设备下次上线补发。
7. 离线队列策略：每设备最多 20 条，TTL 24 小时。

## 2. 控制台改造（manager backend）

### 2.1 智能体字段扩展

扩展 `models.Agent`：

- `OpenclawEnabled bool`
- `OpenclawEnterKeywords string`（JSON 数组字符串）
- `OpenclawExitKeywords string`（JSON 数组字符串）

说明：

- 不新增独立 OpenClaw endpoint 配置表。
- endpoint 获取逻辑对齐 MCP：通过智能体接口动态生成并返回。

### 2.2 Endpoint 接口

新增接口（用户与管理端）：

- `GET /api/user/agents/:id/openclaw-endpoint`
- `GET /api/admin/agents/:id/openclaw-endpoint`

行为：

1. 校验 agent 存在且归属合法。
2. 读取 OTA 外网 WebSocket URL。
3. 生成稳定 JWT token（长期有效，不设置 exp/iat）。
4. 返回 `ws(s)://host/ws/openclaw?token=<token>`。

### 2.3 Token Claims

新增 OpenClaw claims：

- `user_id`
- `agent_id`
- `endpoint_id`（`agent_<agentID>`）
- `purpose=openclaw-endpoint`

## 3. 设备配置下发与解析

### 3.1 结构体

在主程序 `UConfig` 中新增：

```go
type OpenClawConfig struct {
    Allowed       bool     `json:"allowed"`
    EnterKeywords []string `json:"enter_keywords"`
    ExitKeywords  []string `json:"exit_keywords"`
}
```

并在 `UConfig` 中加入：

```go
OpenClaw OpenClawConfig `json:"openclaw"`
```

### 3.2 /api/configs 响应

管理端 `GetDeviceConfigs` 增加：

```json
"openclaw": {
  "allowed": true,
  "enter_keywords": ["进入爪子模式", "openclaw"],
  "exit_keywords": ["退出爪子模式", "退出openclaw"]
}
```

填充规则：

1. `allowed = agent.openclaw_enabled`
2. `enter_keywords/exit_keywords` 从 agent 字段解析 JSON 数组
3. 若字段为空或解析失败则回退为空数组

### 3.3 主程序配置拉取

`ConfigManager.GetUserConfig` 解析 `openclaw` 对象，写入 `types.UConfig.OpenClaw`。

## 4. 主程序 OpenClaw WebSocket 服务端

### 4.1 路由

新增路由：

- `/ws/openclaw`

### 4.2 连接维度

按 `agent_id` 管理连接池：

- key: `agentID`
- value: OpenClaw session（单连接）

新连接建立时替换旧连接，保证一个 agent 只有一个活跃 OpenClaw WS 连接。

### 4.3 协议处理

1. 建连后先发 `handshake_ack`
2. 收到 `ping` 回 `pong`
3. 收到 `response` 时：
   - 按 `correlation_id` 找 device 路由
   - 若设备在线则推送 TTS
   - 若设备离线则写离线队列
4. 收到 `error/close` 记录并按会话清理

## 5. Chat 流程改造

### 5.1 会话状态

`ClientState` 新增 OpenClaw 运行态：

- `OpenClawMode bool`

### 5.2 ASR 后分流

在 `ChatSession.actionDoChat` 增加流程：

1. 识别退出关键词（优先级最高）
2. 识别进入关键词
3. 若当前是 OpenClaw 模式：
   - 文本直接发 OpenClaw
   - 不调用 LLM
4. 若非 OpenClaw 模式：
   - 保持原 LLM 流程

关键词匹配方式：先归一化文本（去首尾空白、常见标点），再 `contains` 匹配。

## 6. 离线消息队列

新增内存离线队列管理器：

- key: `deviceID`
- value: `[]OfflineMessage`
- 每条记录字段：`Text`、`CreatedAt`、`CorrelationID`

策略：

1. 每设备最多 20 条（超出时丢最旧）
2. TTL 24h（写入与读取时双重清理）
3. 设备上线时自动回放并清空成功发送消息

上线触发点：

- `App.OnNewConnection` 中设备在线后，触发该设备离线消息回放

## 7. 主要代码改动点

1. `manager/backend/models/models.go`
2. `manager/backend/controllers/admin.go`
3. `manager/backend/controllers/user.go`
4. `manager/backend/router/router.go`
5. `internal/domain/config/types/types.go`
6. `internal/domain/config/manager/manager.go`
7. `internal/app/server/websocket/websocket_server.go`
8. `internal/app/server/websocket/openclaw.go`（新增）
9. `internal/domain/openclaw/*`（新增：连接池、消息模型、离线队列）
10. `internal/data/client/client.go`
11. `internal/app/server/chat/session.go`
12. `internal/app/server/app.go`

## 8. 安装步骤（含配置channel）

1. 安装 xiaozhi OpenClaw 插件：
   `openclaw plugins install @xiaozhi_openclaw/xiaozhi`
2. 在控制台打开智能体 OpenClaw 配置，复制该智能体的 OpenClaw 接入点（`ws(s)://.../ws/openclaw?token=...`）。
3. 在 OpenClaw 会话中执行“配置channel”：
   - 将上一步接入点直接发给 OpenClaw
   - 明确告诉它：`配置xiaozhi渠道插件`
4. 配置完成后，使用测试会话发送一条消息，确认能收到 OpenClaw 回复。

## 9. 验证清单

1. 控制台可获取 OpenClaw endpoint（与 MCP 生成逻辑一致）。
2. `/api/configs` 返回结构化 `openclaw` 字段。
3. OpenClaw client 可通过 `/ws/openclaw` 建立连接并握手。
4. 进入关键词可切入 OpenClaw 模式，退出关键词可退出模式。
5. OpenClaw 模式下消息不走 LLM，响应可转 TTS。
6. 设备离线时响应进入离线队列；上线后补发。
7. 离线队列满足 20 条上限与 24 小时 TTL。
