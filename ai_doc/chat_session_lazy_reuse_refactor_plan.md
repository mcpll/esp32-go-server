# ChatSession 懒创建与长期复用改造方案

## 目标

- `ChatSession` 从“连接建立即创建”改为“`hello` 成功后懒创建，并在连接存活期长期复用”。
- `ChatSession.Close()` 只释放聊天域资源，不再关闭 `serverTransport`，也不再关闭设备侧 IoT-over-MCP。
- `hello` 按职责拆分为两段：
  - transport 级：返回 `transport/udp(server, port, key, nonce)` 等握手信息。
  - chat 级：写入 `audio_params`、初始化或复用 `SessionID`、刷新设备配置、触发会话创建。
- `mcp/iot/goodbye` 从 `ChatSession` 的运行主链路中剥离，交给 `ChatManager` 处理。

## 设计边界

### ChatManager

- 连接级 owner。
- 持有 `transport`、`serverTransport`、`clientState`、`mcpTransport`、`hookHub`、`transformRegistry`。
- 负责启动并持有命令循环、音频循环。
- 负责处理：
  - `hello`
  - `mcp`
  - `iot`
  - `goodbye`
- 对 `listen/abort` 做路由，必要时 `ensureSession()`。

### ChatSession

- 只负责聊天域：
  - `listen`
  - `abort`
  - ASR/VAD
  - LLM/TTS
  - 会话级媒体播放
- `Start()` 不再启动连接级 `CmdMessageLoop/AudioMessageLoop`。
- `Start()` 在输入音频格式已经就绪的前提下启动：
  - VAD/ASR 后台循环
  - `processChatText`
  - `llmManager.Start`
  - `ttsManager.Start`

## 生命周期约定

- 首个 `hello`：
  - 写入 `clientState.InputAudioFormat`
  - 创建 `SessionID`
  - 可选初始化设备侧 MCP
  - `ensureSession()`
  - 回 `hello`
- 重复 `hello`：
  - 更新 `audio_params`
  - 刷新设备配置
  - 若当前没有活动 `ChatSession`，重新 `ensureSession()`
  - 可选重新触发设备侧 MCP 初始化
- `mqtt_udp`：
  - 正常聊天结束不关闭 transport
  - 显式退出/致命错误只销毁 `ChatSession`
  - 后续可继续复用连接并重建 `ChatSession`
- `websocket`：
  - 显式退出/致命错误后，`ChatManager` 在会话清理完成后关闭 transport

## 代码改动点

- `internal/app/server/chat/chat.go`
  - `ChatManager` 持有连接级资源与消息路由
  - 增加 `ensureSession()`、`HandleHelloMessage()`、连接级 `cmd/audio` loop
- `internal/app/server/chat/session.go`
  - `Start()` 只保留聊天域后台任务
  - `Close()` 改成纯聊天资源释放
  - 增加会话关闭回调，供 `ChatManager` 做协议差异化处理
- `internal/app/server/chat/server_transport.go`
  - 增加“不关闭底层 transport 的关闭路径”，用于远端已断开场景
- `internal/app/server/event_handle.go`
  - 退出聊天事件改由 `ChatManager` 执行，而不是直接拿 `ChatSession`

## 验证点

- 新连接建立后，不会立即创建 `ChatSession`。
- 首个 `hello` 后创建 `ChatSession`，并能继续 `listen/start`。
- `mqtt_udp` 下 `ChatSession.Close()` 后 transport 仍可继续收发命令。
- `websocket` 下显式退出后连接会被关闭。
- MCP 工具查找继续保持 transport-aware，不回退到无 transport 维度。
