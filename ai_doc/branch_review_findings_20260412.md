# 分支代码审查记录（2026-04-12）

分支：`codex/optimize-tool-invocation-for-concurrency`

## 1. [P1] 媒体播放失败仍被标记为有媒体输出

- 位置：
  - `internal/app/server/chat/tool.go:257`
  - `internal/app/server/chat/tool.go:271`
  - `internal/app/server/chat/tool.go:275`
  - `internal/app/server/chat/llm.go:873`
- 问题描述：
  - 当 `handleAudioContent` / `handleResourceLink` 调用失败时，当前逻辑仍然设置：
    - `execResult.hasMediaOutput = true`
    - `execResult.shouldStopLLMProcessing = true`
  - 上层据此认为“媒体已输出”，会走抑制 `tts_stop`/不继续 LLM 的路径。
- 风险影响：
  - 实际没有成功播放媒体，但对话流程按“已成功媒体输出”收尾，可能出现客户端静默、状态不一致或无后续回复。

## 2. [P2] 空 ToolCall ID 的去重可能误伤合法重复调用

- 位置：
  - `internal/app/server/chat/tool.go:154`
  - `internal/app/server/chat/tool.go:160`
  - `internal/app/server/chat/tool.go:44`
  - `internal/app/server/chat/tool.go:67`
- 问题描述：
  - 目前对空 `ToolCall.ID` 使用 `auto_<name>_<arguments>` 生成标识并去重。
  - 若模型合法地产生两个“无 ID 且参数相同”的调用，第二个会被跳过。
- 风险影响：
  - 可能导致 assistant 中的 `tool_calls` 与后续 `tool_result` 数量/对应关系不一致，影响后续轮次上下文和工具调用可靠性。

