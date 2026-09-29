# VAD/ASR/LLM/TTS 全链路压测 Mock 方案（待确认）

> 目标：在不调用真实 ASR/LLM/TTS 付费服务的前提下，保留现有 WebSocket 全链路行为，支持高并发压测、可控时延注入、可观测性统计。

## 1. 设计目标

1. **链路完整**：保留「设备音频输入 -> VAD -> ASR -> LLM -> TTS -> 音频下发」主流程。
2. **零外部成本**：ASR/LLM/TTS 均返回本地 mock 数据，不访问第三方云服务。
3. **低侵入**：基于现有 provider 工厂机制扩展 `mock` provider，尽量不改业务主流程。
4. **可压测可复现**：支持固定返回、模板返回、按概率错误注入、按配置注入延时。
5. **可对照真实服务**：通过配置切换，后续可随时恢复真实 provider 对比性能。

## 2. 总体方案

采用 **Provider 级 Mock + 压测客户端复用** 的方案：

- 新增三个 provider：
  - `asr/mock`
  - `llm/mock`
  - `tts/mock`
- 在后台配置里新增对应配置项（`type=asr|llm|tts`, `provider=mock`）。
- 通过角色/智能体绑定 mock 配置，实现会话内全链路 mock。
- 压测侧继续使用现有 websocket 压测工具（`ws_multi`）并发压入音频。

这样可以保证：
- WebSocket 协议、会话状态机、消息编排逻辑都走真实代码路径。
- 仅替换对外部云服务的调用，成本最低、风险最小。

## 3. Mock 行为设计

### 3.1 ASR Mock

输入：音频帧流（保持现有接口）。
输出：识别文本（固定/轮询/按规则）。

建议配置：

- `mode`: `fixed` | `sequence` | `echo_hint`
- `fixed_text`: 固定返回，例如“你好，这是压测文本”
- `sequence_texts`: 文本数组，按请求轮转
- `first_token_delay_ms`: 首包延迟模拟
- `final_delay_ms`: 结束包延迟模拟
- `error_rate`: 0~1 概率注入识别失败

### 3.2 LLM Mock

输入：ASR 文本 + 上下文消息。
输出：回复文本（可携带上下文长度信息）。

建议配置：

- `mode`: `fixed` | `template` | `echo`
- `fixed_answer`: 固定回复
- `template`: 模板，例如 `"收到：{{input}}"`
- `first_token_delay_ms`: 首 token 延迟
- `stream_chunk_chars`: 流式每片字符数
- `total_delay_ms`: 完成总耗时模拟
- `error_rate`: 概率失败

### 3.3 TTS Mock

输入：LLM 文本。
输出：可播放的 Opus/PCM 帧（建议优先 Opus，兼容当前链路）。

建议配置：

- `audio_source`: `builtin_silence` | `builtin_beep` | `file`
- `file_path`: 预置音频路径（本地 wav/opus）
- `frame_duration_ms`: 分帧长度（如 20ms）
- `first_frame_delay_ms`: 首帧延迟
- `inter_frame_delay_ms`: 帧间延迟
- `error_rate`: 概率失败

> 为降低复杂度，第一版建议：先返回“静音帧 + 固定时延”，后续再补“beep/文件回放”。

## 4. 压测场景矩阵

### 场景 A：纯成功链路（基准）
- ASR 固定文本
- LLM 固定短回复
- TTS 静音帧
- 目标：测最大稳定并发、平均RT、P95/P99

### 场景 B：高时延链路
- ASR/LLM/TTS 分别注入 100~500ms 延迟
- 目标：测超时阈值、排队堆积情况

### 场景 C：错误注入链路
- error_rate 设置 1%/5%/10%
- 目标：测错误恢复、连接稳定性、重试策略

### 场景 D：长文本链路
- LLM 输出超长文本（如 500~1500 字）
- 目标：测 TTS 分帧、发送背压和内存稳定性

## 5. 指标与验收标准（建议）

核心指标：
- 会话成功率（成功返回语音）
- 端到端首帧时延（listen stop -> 首个音频包）
- 端到端完成时延（listen stop -> tts finish）
- 每秒活跃会话数 / 峰值并发
- 错误率（分 ASR/LLM/TTS 阶段）
- 服务资源：CPU、内存、Goroutine、GC 次数

建议验收（可后续调整）：
- 成功率 >= 99%
- 在目标并发下 P95 首帧时延 < 1.5s
- 持续 30min 无明显内存泄漏（RSS 变化可控）

## 6. 实施步骤（分两阶段）

### Phase 1（最小可用，1~2 天）
1. 增加 ASR/LLM/TTS 三个 mock provider 注册。
2. 每个 provider 支持固定返回 + 固定延迟 + 错误率。
3. 后台新增三条 mock 配置并可设为默认。
4. 跑通 `ws_multi` 并输出基准压测结果。

### Phase 2（增强，1~2 天）
1. 增加模板回复、序列回复、文件音频回放。
2. 增加更细粒度指标日志（分阶段耗时）。
3. 增加压测脚本（批量场景执行 + 汇总报表）。

## 7. 风险与规避

1. **音频格式不匹配**：mock tts 输出格式需与当前下游解码一致。
   - 规避：第一版沿用现有常用编码路径并增加格式校验日志。
2. **并发下日志过大**：高并发详细日志会影响性能。
   - 规避：压测模式降级日志级别，关键指标聚合输出。
3. **配置误切真实服务**：导致仍调用外部接口。
   - 规避：压测环境禁网或加入 provider 白名单校验（非 mock 拒绝启动）。

## 8. 你确认后我将执行的落地内容

确认后我会按以下清单直接改代码：

1. 新增 `internal/domain/asr/mock`、`internal/domain/llm/mock`、`internal/domain/tts/mock`。
2. 在 provider factory / pool 注册点挂载 `mock` provider。
3. 补充默认配置样例（可在管理后台直接选 mock）。
4. 增加最小单元测试（至少 provider 行为测试）。
5. 给出一份压测执行命令清单（并发阶梯 + 指标采集）。

---

## 需你确认的选项

请确认以下 4 点，我再开始正式改造：

1. **Mock 粒度**：是否同意按 provider 级 mock（推荐）？
2. **TTS 输出**：第一版是否接受“静音帧”作为 mock 音频（最快）？
3. **压测目标并发**：先以多少并发为目标（如 100/300/500）？
4. **验收阈值**：是否按本文默认验收标准执行？
