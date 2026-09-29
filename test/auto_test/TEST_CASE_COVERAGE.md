# auto_test 覆盖测试 Case 说明

本文档说明 `test/auto_test` 下自动化协议测试当前覆盖的 case、验证目标与主要风险点，便于回归时快速判断该跑哪些用例。

---

## 1. 测试入口

主要入口文件：

- `xiaozhi_websocket_client.go`
- `automation_suite.go`
- `mqtt_udp_full_suite.go`
- `ota_mqtt_udp_suite.go`

常用编译检查：

```bash
CGO_LDFLAGS="-lm" go test -buildvcs=false -tags nolibopusfile ./test/auto_test
```

典型自动化运行方式：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device auto-test-device \
  -cases all \
  -case_timeout 80s
```

只跑部分 case：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device auto-test-device \
  -cases manual_roundtrip,abort_during_tts
```

当前 VM 共享目录场景建议保留：

- `-buildvcs=false`：避免共享目录 Git owner 检查影响构建。
- `CGO_LDFLAGS="-lm"`：补齐当前 opus 相关链接依赖。
- `-tags nolibopusfile`：使用当前自动化测试环境的构建标签。

---

## 2. 当前覆盖的测试用例

### 2.0 Case 输入 / 输出速查表

#### WebSocket

| Case | 输入 | 期望输出 / 断言 |
| --- | --- | --- |
| `manual_roundtrip` | WebSocket hello；`listen start manual`；发送一轮测试语音；`listen stop manual` | hello ack；STT 文本；TTS 文本输出；`tts start/stop`；收到服务端音频帧 |
| `manual_multi_turn` | WebSocket hello；连续 3 轮 `listen start manual`、测试语音、`listen stop manual` | 3 轮 STT/TTS 均成功；listen 数量为 6；会话状态未串轮 |
| `auto1_roundtrip` | WebSocket hello；`listen detect`；欢迎语结束后 `listen start auto`；发送测试语音 | hello ack；STT 文本；TTS 输出；`tts stop` 后自动重启 `listen start auto` |
| `auto2_roundtrip` | WebSocket hello；`listen start auto`；`listen detect`；欢迎语结束后再次 `listen start auto`；发送测试语音 | hello ack；STT 文本；TTS 输出；`tts stop` 后自动重启 `listen start auto` |
| `realtime_roundtrip` | WebSocket hello；`listen detect`；欢迎语结束后 `listen start realtime`；发送测试语音和静音帧 | hello ack；STT 文本；TTS 输出；`tts stop` 后不额外补发 listen start |
| `hello_metadata` | WebSocket hello | hello ack 包含 `session_id`、`audio_params`，且 `transport=websocket` |
| `invalid_hello_missing_audio_params` | WebSocket hello，省略 `audio_params` | 不应收到成功 hello ack |
| `invalid_hello_unsupported_transport` | WebSocket hello，`transport=invalid_transport` | 不应收到成功 hello ack |
| `duplicate_hello_rehandshake` | 同一 WebSocket 连接发送两次 hello；随后 manual 语音对话 | 收到 2 次 hello ack；duplicate hello 后 STT/TTS 仍成功 |
| `listen_before_hello_ignored` | hello 前先发 `listen start manual`；再补发 hello | hello 前 listen 不触发 STT/输出；补发 hello 后握手成功 |
| `injected_message_skip_llm` | WebSocket hello；调用 `/admin/inject_msg` 注入播报文本 | 直接收到 TTS 输出和 `tts start/stop`；不经过用户语音输入 |
| `iot_roundtrip` | WebSocket hello；发送 `iot` 文本 `turn_on_light` | 收到 `type=iot`、`state=success`，回包文本匹配输入 |
| `mcp_initialize` | WebSocket hello，声明 `features.mcp=true` | 收到 MCP `initialize` 和 `tools/list`；客户端 result id 匹配请求 id |
| `hello_without_mcp_no_initialize` | WebSocket hello，不声明 MCP feature | hello 成功；负向窗口内无 MCP `initialize/tools/list` |
| `mcp_duplicate_hello_no_reinitialize` | 启用 MCP 的 WebSocket hello；MCP 初始化完成后再次 hello | hello ack 数量为 2；MCP 初始化只发生 1 次 |
| `agent_ws_endpoint_mcp` | 智能体 MCP server 通过 `/mcp?token=...` 反连主程序 | endpoint 鉴权成功；主程序下发 MCP `initialize` 和 `tools/list`；短时保活不掉线 |
| `agent_ws_endpoint_mcp_keepalive` | 智能体 MCP server 反连后持续挂机，跨过 2 分钟离线窗口和 10 分钟工具刷新边界 | 收到主程序 JSON-RPC `ping`；10 分钟后再次收到 `tools/list`；连接未断开。该用例为显式选择用例，不包含在 `all` 中 |
| `abort_after_listen_start` | WebSocket hello；`listen start manual`；发送 `abort` | abort 发送成功；控制链路不报错 |
| `abort_during_tts` | WebSocket hello；manual 语音对话；TTS 开始后发送 `abort` | 已收到 STT/TTS；abort 后观察到 `tts stop` |
| `realtime_interrupt` | WebSocket realtime 首轮语音触发 TTS；TTS 期间发送第二轮语音 | 第二轮 STT 成功；TTS stop/切换行为可观察 |
| `realtime_listen_stop` | WebSocket realtime 建立后发送 `listen stop`；再发送 `listen start realtime` 和测试语音 | stop/start 后恢复识别；STT/TTS 成功 |
| `realtime_duplicate_start_ignored` | WebSocket realtime 已启动后重复发送 `listen start realtime`；再发送测试语音 | 重复 start 不破坏链路；STT/TTS 成功 |
| `goodbye_then_resume` | WebSocket hello；发送 `goodbye`；同连接继续 manual 语音对话 | 服务端不回发 goodbye；后续 STT/TTS 成功 |
| `tts_sentence_boundaries` | WebSocket manual 语音对话 | 收到 `tts sentence_start` 和 `tts sentence_end`；主链路成功 |

#### OTA

| Case | 输入 | 期望输出 / 断言 |
| --- | --- | --- |
| `ota_metadata` | POST `/xiaozhi/ota/`，携带设备信息、`Device-Id`、`Client-Id` | 返回 `websocket.url`、`server_time`、`firmware.version`；启用时校验 `activation` 和 `mqtt` |
| `ota_activate_invalid_algorithm` | POST `/xiaozhi/ota/activate`，提交非法 `algorithm` | HTTP 400 |
| `ota_activate_invalid_challenge_if_required` | OTA 返回 activation challenge 后，POST 错误 challenge | HTTP 202；若无 challenge 则 SKIP |

#### MQTT / UDP

| Case | 输入 | 期望输出 / 断言 |
| --- | --- | --- |
| `mqtt_udp_hello` | OTA 获取 MQTT 配置；MQTT 发布 `hello transport=udp`；再发 duplicate hello | 收到 UDP hello ack；包含 `session_id`、`audio_params`、`udp.server/port/key/nonce`；duplicate hello 后 nonce 变化 |
| `mqtt_udp_injected_message` | MQTT/UDP hello 建链；调用 `/admin/inject_msg` 注入播报文本 | 收到 `speak_request`；回 `speak_ready`；收到 TTS 文本、UDP 下行音频和服务端 goodbye |
| `mqtt_udp_manual_roundtrip` | MQTT/UDP hello；`listen start manual`；UDP 上行测试语音；`listen stop manual` | STT 文本；TTS 文本输出；UDP 下行音频 |
| `mqtt_udp_manual_multi_turn` | MQTT/UDP hello；连续 3 轮 manual 语音对话 | 3 轮 STT/TTS 均成功；listen 数量为 6；UDP 上下行稳定 |
| `mqtt_udp_auto1_roundtrip` | MQTT/UDP hello；`listen detect`；欢迎语结束后 `listen start auto`；UDP 上行测试语音 | STT 文本；TTS 输出；`tts stop` 后重启 `listen start auto` |
| `mqtt_udp_auto2_roundtrip` | MQTT/UDP hello；`listen start auto`；`listen detect`；欢迎语结束后再次 `listen start auto`；UDP 上行测试语音 | STT 文本；TTS 输出；`tts stop` 后重启 `listen start auto` |
| `mqtt_udp_realtime_roundtrip` | MQTT/UDP hello；`listen detect`；欢迎语结束后 `listen start realtime`；UDP 上行测试语音和静音帧 | STT 文本；TTS 输出；UDP 下行音频 |
| `mqtt_udp_mcp_initialize` | MQTT/UDP hello，声明 MCP feature | 收到 MCP `initialize` 和 `tools/list`；客户端 result id 匹配请求 id |
| `mqtt_udp_hello_without_mcp_no_initialize` | MQTT/UDP hello，不声明 MCP feature | hello 成功；负向窗口内无 MCP 流量 |
| `mqtt_udp_iot_roundtrip` | MQTT/UDP hello；发送 `iot` 文本 `turn_on_light` | 收到 `type=iot`、`state=success`，回包文本匹配输入 |
| `mqtt_udp_duplicate_hello_rehandshake` | MQTT/UDP hello；duplicate hello；随后 manual 语音对话 | 收到 2 次 hello ack；UDP session 重建；STT/TTS 仍成功 |
| `mqtt_udp_invalid_hello_missing_audio_params` | MQTT 发布 hello，省略 `audio_params` | 不应收到成功 hello ack |
| `mqtt_udp_invalid_hello_unsupported_transport` | MQTT 发布 hello，`transport=invalid_transport` | 不应收到成功 hello ack |
| `mqtt_udp_listen_before_hello_ignored` | MQTT hello 前先发 `listen start manual`；再补发 hello | hello 前 listen 不触发 STT/输出；补发 hello 后握手成功 |
| `mqtt_udp_abort_after_listen_start` | MQTT/UDP hello；`listen start manual`；发送 `abort` | abort 发送成功；控制链路不报错 |
| `mqtt_udp_abort_during_tts` | MQTT/UDP manual 语音对话；TTS 开始后发送 `abort` | 已收到 STT/TTS；abort 后观察到 `tts stop` |
| `mqtt_udp_realtime_interrupt` | MQTT/UDP realtime 首轮语音触发 TTS；TTS 期间发送第二轮语音 | 第二轮 STT 成功；TTS stop/切换行为可观察 |
| `mqtt_udp_realtime_listen_stop` | MQTT/UDP realtime 建立后发送 `listen stop`；再发送 `listen start realtime` 和测试语音 | stop/start 后恢复识别；STT/TTS 成功 |
| `mqtt_udp_realtime_duplicate_start_ignored` | MQTT/UDP realtime 已启动后重复发送 `listen start realtime`；再 UDP 上行测试语音 | 重复 start 不破坏链路；STT/TTS 成功 |
| `mqtt_udp_goodbye_then_resume` | MQTT/UDP hello；发送 `goodbye`；duplicate hello；继续 manual 语音对话 | 重建后 STT/TTS 成功；不期望服务端额外回发 goodbye |
| `mqtt_udp_tts_sentence_boundaries` | MQTT/UDP manual 语音对话 | 收到 `tts sentence_start` 和 `tts sentence_end`；主链路成功 |
| `mqtt_udp_injected_message_warm_path` | MQTT/UDP 已建链；调用 `/admin/inject_msg` 注入播报文本 | 收到 `speak_request`；回 `speak_ready`；收到 TTS 文本、UDP 下行音频和 goodbye |
| `mqtt_udp_injected_message_auto_listen` | MQTT/UDP 已建链；注入播报文本并设置 `auto_listen=true` | 收到 `speak_request`、`speak_ready`、TTS/UDP 音频；不应收到 goodbye |
| `mqtt_udp_injected_message_cold_rehello` | MQTT 冷链路收到 `speak_request`；duplicate hello 重建 UDP；再回 `speak_ready` | 收到 TTS 文本和 UDP 下行音频；冷链路可恢复播报 |

### 2.1 WebSocket 基础对话主链路

#### `manual_roundtrip`
- 覆盖场景：WebSocket manual 模式单轮对话。
- 核心校验：
  - `hello` 成功，`transport=websocket`
  - `listen start -> listen stop`
  - 收到 STT 文本
  - 收到 LLM/TTS 输出
  - 收到 `tts start/stop`
  - 收到服务端接受的音频帧

#### `manual_multi_turn`
- 覆盖场景：WebSocket manual 模式固定 3 轮连续对话。
- 核心校验：
  - 多轮 `listen start/stop` 数量稳定
  - 多轮 STT/TTS 主链路连续可用
- 目标风险：
  - 第二轮或第三轮卡死
  - 上下轮状态未清理干净

#### `auto1_roundtrip`
- 覆盖场景：WebSocket auto1 模式单轮对话。
- 核心校验：
  - 初始 `listen detect`
  - 等欢迎语 TTS 结束后进入 `listen start auto`
  - 本轮 `tts stop` 后重新 `listen start auto`

#### `auto2_roundtrip`
- 覆盖场景：WebSocket auto2 模式单轮对话。
- 核心校验：
  - 初始 `listen start auto -> listen detect`
  - 等欢迎语 TTS 结束
  - 本轮 `tts stop` 后重新 `listen start auto`

#### `realtime_roundtrip`
- 覆盖场景：WebSocket realtime 模式单轮对话。
- 核心校验：
  - 初始 `listen detect`
  - 等欢迎语 TTS 结束后进入 `listen start realtime`
  - `tts stop` 后不会额外补发新的 `listen start`

---

### 2.2 WebSocket Hello / 握手相关

#### `hello_metadata`
- 覆盖场景：WebSocket hello 成功响应后的元信息校验。
- 核心校验：
  - 响应中有 `session_id`
  - 响应中有 `audio_params`
  - `transport=websocket`

#### `invalid_hello_missing_audio_params`
- 覆盖场景：WebSocket hello 缺少 `audio_params`。
- 核心校验：
  - 服务端不会返回成功 hello。

#### `invalid_hello_unsupported_transport`
- 覆盖场景：WebSocket hello 的 `transport` 为非法值。
- 核心校验：
  - 服务端不会返回成功 hello。

#### `duplicate_hello_rehandshake`
- 覆盖场景：同一 WebSocket 连接上重复发送 hello。
- 核心校验：
  - 收到 2 次 hello ack
  - duplicate hello 后仍能继续正常 listen/STT/TTS

#### `listen_before_hello_ignored`
- 覆盖场景：WebSocket hello 之前先发送 `listen`。
- 核心校验：
  - hello 前 listen 不会触发 STT 或输出
  - 补发 hello 后可正常握手

---

### 2.3 WebSocket 主动播报 / IoT / MCP

#### `injected_message_skip_llm`
- 覆盖场景：服务端通过 `/admin/inject_msg` 向 WebSocket 设备注入播报。
- 核心校验：
  - hello 完成后可成功注入消息
  - 注入消息直接触发 `tts start/stop`
  - 收到可播报文本输出
  - 不要求额外校验 auto listen 数量，避免主动播报后的自动续听差异造成误判

#### `iot_roundtrip`
- 覆盖场景：WebSocket 发送 `iot` 控制消息。
- 核心校验：
  - 收到 `type=iot`
  - `state=success`
  - 回包文本与请求一致

#### `mcp_initialize`
- 覆盖场景：WebSocket hello 声明 `features.mcp=true`。
- 核心校验：
  - 服务端下发 `initialize`
  - 服务端下发 `tools/list`
  - 客户端发送 `result`
  - `result.id` 与请求 `id` 匹配
  - `initialize/tools.list` 次数符合预期

#### `hello_without_mcp_no_initialize`
- 覆盖场景：WebSocket hello 不声明 MCP feature。
- 核心校验：
  - hello 正常成功
  - 在负向窗口内不应收到任何 MCP 流量

#### `mcp_duplicate_hello_no_reinitialize`
- 覆盖场景：MCP 已健康初始化后再次 duplicate hello。
- 核心校验：
  - 首次 hello 触发一次完整 MCP 初始化
  - 第二次 hello 成功
  - duplicate hello 后不会重复收到 `initialize/tools/list`

#### `agent_ws_endpoint_mcp`
- 覆盖场景：智能体 WebSocket MCP endpoint 反连主程序 `/mcp?token=...`。
- 测试流程：
  - 根据 `-server` 的 host 派生 `ws(s)://host/mcp?token=...`
  - 使用 `-endpoint_auth_token` 生成 `purpose=mcp-endpoint`、包含 `agentId` 的 HS256 JWT
  - 在 auto_test 进程内启动一个 MCP server，提供 `hello_world` 与 `query_weather`
  - MCP server 作为 WebSocket client 连接主程序 endpoint
- 核心校验：
  - endpoint 连接建立且 token 鉴权通过
  - 主程序下发 MCP `initialize`
  - 主程序下发 MCP `tools/list`
  - auto_test MCP server 正确回传 result
  - 初始连接短时保持稳定，不立即断开

#### `agent_ws_endpoint_mcp_keepalive`
- 覆盖场景：用户反馈的“智能体 WebSocket MCP endpoint 一段时间后断连”回归链路。
- 测试流程：
  - 复用 `agent_ws_endpoint_mcp` 的连接方式
  - 持续保持连接，等待主程序心跳和工具刷新周期
  - 该用例耗时较长，默认不包含在 `-cases all`，需要显式指定
- 核心校验：
  - 超过 2 分钟离线判定窗口后仍能收到主程序 JSON-RPC `ping`
  - 跨过 10 分钟工具刷新边界后再次收到 `tools/list`
  - 过程中连接未断开

---

### 2.4 WebSocket Abort / Realtime / Goodbye

#### `abort_after_listen_start`
- 覆盖场景：正常握手后，先 `listen start` 再 `abort`。
- 核心校验：
  - abort 消息成功发出
  - 控制链路不报错

#### `abort_during_tts`
- 覆盖场景：TTS 已开始后发送 abort。
- 核心校验：
  - 先出现 `tts start`
  - abort 发出后能观察到 `tts stop`
  - 主链路包含 STT/输出

#### `realtime_interrupt`
- 覆盖场景：WebSocket realtime 模式下第二轮语音打断首轮播报。
- 核心校验：
  - 首轮 TTS 期间发起第二轮输入
  - 第二轮 STT 成功触发
  - 可观察到 stop/切换行为

#### `realtime_listen_stop`
- 覆盖场景：WebSocket realtime 模式显式发送 `listen stop` 后再恢复。
- 核心校验：
  - 已建立 realtime listen
  - 发 `listen stop`
  - 再次 `listen start`
  - 恢复后仍可完成 STT/TTS

#### `realtime_duplicate_start_ignored`
- 覆盖场景：WebSocket realtime 已处于 listening，再重复发 `listen start`。
- 核心校验：
  - 重复 start 不影响后续 STT/TTS
  - 整体链路仍稳定

#### `goodbye_then_resume`
- 覆盖场景：WebSocket 连接内发送 `goodbye` 后继续对话。
- 核心校验：
  - goodbye 后无需重连
  - 服务端不会回发 `goodbye`
  - 后续仍可完成新的 listen/STT/TTS

#### `tts_sentence_boundaries`
- 覆盖场景：WebSocket manual 对话中的句子边界信令。
- 核心校验：
  - 收到 `tts/sentence_start`
  - 收到 `tts/sentence_end`
  - 仍具备完整 STT/TTS 主链路

---

### 2.5 OTA / 激活流程

#### `ota_metadata`
- 覆盖场景：调用 `/xiaozhi/ota/`。
- 核心校验：
  - 返回 `websocket.url`
  - 返回 `server_time`
  - 返回 `firmware.version`
  - 若服务端开启了对应能力，则同时校验 `activation` / `mqtt` 结构完整性

#### `ota_activate_invalid_algorithm`
- 覆盖场景：调用 `/xiaozhi/ota/activate`，提交非法 `algorithm`。
- 核心校验：
  - 服务端返回 `400`
  - 激活接口的参数校验链路生效

#### `ota_activate_invalid_challenge_if_required`
- 覆盖场景：OTA 返回 activation challenge 后，提交错误 challenge。
- 核心校验：
  - 服务端返回 `202`
  - challenge 校验失败路径可达
- 说明：
  - 若当前环境未开启 auth，或设备已激活导致 OTA 不再返回 activation，本用例会 `SKIP`。

---

### 2.6 MQTT / UDP 基础握手与主动播报

#### `mqtt_udp_hello`
- 覆盖场景：设备通过 OTA 获取 MQTT 凭据后，以 `transport=udp` 发送 hello。
- 核心校验：
  - 收到 `transport=udp` 的 hello ack
  - 响应中包含 `session_id`
  - 响应中包含 `audio_params`
  - 响应中包含 `udp.server/port/key/nonce`
  - duplicate hello 后，服务端会重建 UDP session，`nonce` 变化
- 说明：
  - 若 OTA 未返回 mqtt 配置，本用例会 `SKIP`。

#### `mqtt_udp_injected_message`
- 覆盖场景：MQTT 设备完成 hello 后，服务端通过 `/admin/inject_msg` 主动播报。
- 核心校验：
  - 先收到 `speak_request`
  - 客户端回 `speak_ready`
  - 收到 `tts start/stop`
  - 收到文本输出
  - 收到至少一帧 UDP TTS 音频
  - 播报结束后收到服务端 `goodbye`
- 说明：
  - 这是早期 MQTT/UDP 主动播报入口，完整主动播报变体见 2.9。

---

### 2.7 MQTT / UDP 主链路

#### `mqtt_udp_manual_roundtrip`
- 覆盖场景：MQTT/UDP manual 模式单轮对话。
- 核心校验：
  - OTA 返回 MQTT 配置
  - MQTT hello 返回 UDP 配置
  - `listen start -> UDP 上行音频 -> listen stop`
  - 收到 STT 文本
  - 收到 TTS/LLM 输出
  - 收到 UDP 下行音频

#### `mqtt_udp_manual_multi_turn`
- 覆盖场景：MQTT/UDP manual 模式固定 3 轮连续对话。
- 核心校验：
  - 多轮 `listen start/stop` 数量稳定
  - 多轮 UDP 上行音频均可触发 STT
  - 多轮 TTS 下行音频可接收

#### `mqtt_udp_auto1_roundtrip`
- 覆盖场景：MQTT/UDP auto1 模式单轮对话。
- 核心校验：
  - `listen detect`
  - 等欢迎语 TTS 结束后进入 `listen start auto`
  - 对话 TTS stop 后重启监听

#### `mqtt_udp_auto2_roundtrip`
- 覆盖场景：MQTT/UDP auto2 模式单轮对话。
- 核心校验：
  - `listen start auto -> listen detect`
  - 等欢迎语 TTS 结束后再次进入 `listen start auto`
  - 对话 TTS stop 后重启监听

#### `mqtt_udp_realtime_roundtrip`
- 覆盖场景：MQTT/UDP realtime 模式单轮对话。
- 核心校验：
  - `listen detect`
  - 等欢迎语 TTS 结束后进入 `listen start realtime`
  - realtime 静音帧保持链路
  - UDP 上行音频可触发 STT/TTS

---

### 2.8 MQTT / UDP Hello / MCP / IoT / 异常路径

#### `mqtt_udp_mcp_initialize`
- 覆盖场景：MQTT/UDP hello 启用 MCP。
- 核心校验：
  - 服务端下发 `initialize`
  - 服务端下发 `tools/list`
  - 客户端发送 `result`
  - `initialize/tools.list` 次数符合预期

#### `mqtt_udp_hello_without_mcp_no_initialize`
- 覆盖场景：MQTT/UDP hello 不声明 MCP feature。
- 核心校验：
  - hello 正常成功
  - 在负向窗口内不应收到 MCP 流量

#### `mqtt_udp_iot_roundtrip`
- 覆盖场景：MQTT/UDP 发送 `iot` 控制消息。
- 核心校验：
  - 收到 `type=iot`
  - `state=success`
  - 回包文本与请求一致

#### `mqtt_udp_duplicate_hello_rehandshake`
- 覆盖场景：MQTT/UDP duplicate hello 后继续对话。
- 核心校验：
  - 收到 2 次 hello ack
  - 第二次 hello 重建 UDP session
  - 重建后仍可完成 listen/STT/TTS

#### `mqtt_udp_invalid_hello_missing_audio_params`
- 覆盖场景：MQTT/UDP hello 缺少 `audio_params`。
- 核心校验：
  - 服务端不会返回成功 hello。

#### `mqtt_udp_invalid_hello_unsupported_transport`
- 覆盖场景：MQTT/UDP hello 的 `transport` 为非法值。
- 核心校验：
  - 服务端不会返回成功 hello。

#### `mqtt_udp_listen_before_hello_ignored`
- 覆盖场景：MQTT/UDP hello 前发送 `listen`。
- 核心校验：
  - hello 前 listen 不会触发 STT 或输出
  - 补发 hello 后可正常握手

---

### 2.9 MQTT / UDP Abort / Realtime / Goodbye / 主动播报变体

#### `mqtt_udp_abort_after_listen_start`
- 覆盖场景：MQTT/UDP `listen start` 后发送 abort。
- 核心校验：
  - abort 消息成功发出
  - 控制链路不报错

#### `mqtt_udp_abort_during_tts`
- 覆盖场景：MQTT/UDP TTS 已开始后发送 abort。
- 核心校验：
  - 先出现 `tts start`
  - abort 发出后能观察到 `tts stop`
  - 主链路包含 STT/输出

#### `mqtt_udp_realtime_interrupt`
- 覆盖场景：MQTT/UDP realtime 下第二轮输入打断首轮播报。
- 核心校验：
  - 首轮 STT/TTS 成功
  - TTS 期间发起第二轮输入
  - 第二轮 STT/TTS 成功触发

#### `mqtt_udp_realtime_listen_stop`
- 覆盖场景：MQTT/UDP realtime 显式 stop/start 后继续对话。
- 核心校验：
  - 已建立 realtime listen
  - 发 `listen stop`
  - 再次 `listen start`
  - 恢复后仍可完成 STT/TTS

#### `mqtt_udp_realtime_duplicate_start_ignored`
- 覆盖场景：MQTT/UDP realtime 重复 `listen start`。
- 核心校验：
  - 重复 start 不破坏后续链路
  - 后续 UDP 上行音频仍可触发 STT/TTS

#### `mqtt_udp_goodbye_then_resume`
- 覆盖场景：MQTT/UDP 发送 `goodbye` 后 duplicate hello 恢复对话。
- 核心校验：
  - goodbye 后通过重复 hello 重建会话
  - 重建后仍可完成 listen/STT/TTS
  - 不期望服务端额外回发 goodbye

#### `mqtt_udp_tts_sentence_boundaries`
- 覆盖场景：MQTT/UDP manual 对话中的句子边界信令。
- 核心校验：
  - 收到 `tts/sentence_start`
  - 收到 `tts/sentence_end`
  - 仍具备完整 STT/TTS 主链路

#### `mqtt_udp_injected_message_warm_path`
- 覆盖场景：MQTT/UDP 已建链时服务端主动播报。
- 核心校验：
  - 收到 `speak_request`
  - 客户端回 `speak_ready`
  - 收到 `tts start/stop`
  - 收到 UDP 下行音频
  - 播报结束后收到服务端 `goodbye`

#### `mqtt_udp_injected_message_auto_listen`
- 覆盖场景：MQTT/UDP 主动播报携带 `auto_listen=true`。
- 核心校验：
  - 收到 `speak_request`
  - 客户端回 `speak_ready`
  - 收到 TTS 输出和 UDP 下行音频
  - 不应收到服务端 `goodbye`

#### `mqtt_udp_injected_message_cold_rehello`
- 覆盖场景：MQTT/UDP 冷链路收到 `speak_request` 后重建 UDP。
- 核心校验：
  - 冷链路先收到 `speak_request`
  - 通过 duplicate hello 重建 UDP session
  - 再回 `speak_ready`
  - 收到主动播报 TTS/UDP 下行音频

---

## 3. 当前断言能力

自动化框架目前已覆盖的断言包括：

- hello 响应存在与数量校验
- hello `transport` 校验
- hello `session_id` / `audio_params` 校验
- MQTT hello `udp.server/port/key/nonce` 校验
- MQTT duplicate hello 的 UDP session 轮转校验
- 服务端注入消息触发 TTS 校验
- `listen` 发送顺序与数量校验
- STT 文本存在校验
- LLM/Text/TTS sentence 输出存在校验
- TTS `sentence_start/sentence_end` 边界校验
- `tts start -> tts stop` 生命周期校验
- WebSocket 二进制音频帧接收校验
- MQTT/UDP 上行音频触发 STT 校验
- MQTT/UDP 下行音频帧接收校验
- MQTT 主动播报 `speak_request -> speak_ready` 校验
- MQTT 主动播报结束后服务端 `goodbye` 校验
- MQTT 主动播报 `auto_listen=true` 不回 goodbye 校验
- auto 模式下 `tts stop` 后自动重启监听校验
- realtime 重复 start 不破坏后续识别校验
- realtime stop/start 恢复识别校验
- MCP `initialize` / `tools/list` 次数校验
- MCP `result` 与请求 `id` 匹配校验
- 智能体 WebSocket MCP endpoint 反连、JWT 鉴权、JSON-RPC `ping` 保活、周期性 `tools/list` 刷新校验
- iot `success` 响应校验
- abort 已发送校验
- abort 后出现 `tts stop` 校验
- OTA 激活接口 `400/202` 失败路径校验

---

## 4. 用例分组建议

### Smoke

适合快速回归：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device smoke-device \
  -cases manual_roundtrip,auto1_roundtrip,realtime_roundtrip,hello_metadata,injected_message_skip_llm,iot_roundtrip,mcp_initialize,abort_during_tts,ota_metadata \
  -case_timeout 80s
```

### WebSocket 重点回归

适合改动 WebSocket listen/ASR/TTS/session 状态机后运行：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device ws-regression-device \
  -cases auto1_roundtrip,auto2_roundtrip,realtime_roundtrip,injected_message_skip_llm,realtime_interrupt,realtime_duplicate_start_ignored \
  -case_timeout 80s
```

### 智能体 WebSocket MCP Endpoint 回归

适合改动 `/mcp` endpoint、MCP session、工具列表刷新、状态管理、离线判定、重连/保活策略后运行：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://localhost:9990/xiaozhi/v1/ \
  -device smoke-agent \
  -cases agent_ws_endpoint_mcp \
  -endpoint_auth_token xiaozhi_mcp_openclaw_secret_key
```

长连接保活与 10 分钟工具刷新边界回归：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://localhost:9990/xiaozhi/v1/ \
  -device smoke-agent \
  -cases agent_ws_endpoint_mcp_keepalive \
  -endpoint_auth_token xiaozhi_mcp_openclaw_secret_key
```

### MQTT / UDP 主链路

适合改动 MQTT/UDP、OTA MQTT 下发、UDP 音频收发后运行：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device mqtt-regression-device \
  -cases mqtt_udp_manual_roundtrip,mqtt_udp_auto1_roundtrip,mqtt_udp_auto2_roundtrip,mqtt_udp_realtime_roundtrip,mqtt_udp_realtime_interrupt,mqtt_udp_realtime_duplicate_start_ignored \
  -case_timeout 80s
```

### 主动播报

适合改动 `/admin/inject_msg`、speak_request、speak_ready、主动 TTS 后运行：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device injected-regression-device \
  -cases injected_message_skip_llm,mqtt_udp_injected_message_warm_path,mqtt_udp_injected_message_auto_listen,mqtt_udp_injected_message_cold_rehello \
  -case_timeout 80s
```

### Full

适合协议改动后完整回归：

```bash
CGO_LDFLAGS="-lm" go run -buildvcs=false -tags nolibopusfile ./test/auto_test \
  -runner auto \
  -server ws://127.0.0.1:8990/xiaozhi/v1/ \
  -device full-regression-device \
  -cases all \
  -case_timeout 80s
```

---

## 5. 已知限制

1. MQTT/UDP 用例依赖 OTA 返回 `mqtt` 配置；如果当前服务端未开启 MQTT/UDP 能力，相关用例会 `SKIP` 或失败在配置获取阶段。
2. 主链路用例依赖 ASR、LLM、TTS 外部服务可用；外部服务慢或超时时会影响耗时和稳定性。
3. MQTT/UDP 测试会在批量运行时为每个 MQTT/UDP case 派生独立 device id，避免上一个 MQTT client 的异步 offline 生命周期影响后一个 case。
4. `ota_activate_invalid_challenge_if_required` 依赖 OTA 返回 activation challenge；未开启 auth 或设备已激活时会 `SKIP`。
5. 当前文档描述的是 `test/auto_test` 自动化 case 覆盖范围，不包含 `internal/...` 单元测试覆盖项。
