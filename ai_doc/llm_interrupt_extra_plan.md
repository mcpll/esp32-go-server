# LLM 打断标识与历史内容增强方案（待确认）

## 1. 目标

在当前项目中实现两件事：

1. 当 LLM 流式处理中途被打断时，把打断标识写入该条 assistant 历史消息的 `Extra`。  
2. 后续组装 LLM 请求历史时，若检测到该标识，则在该条消息 `content` 末尾追加 `" [用户打断]"` 后再送给模型。

说明：此方案先描述实现路径，不直接改代码。

---

## 2. 现状代码路径（关键点）

- 中断触发：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/common.go:3`  
  `StopSpeaking()` 会取消 `SessionCtx`，导致 LLM/TTS 处理上下文结束。

- LLM 流式处理与落历史：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go:323`  
  当前 `handleLLMResponse()` 只有在 `llmResponse.IsEnd=true` 时才保存 assistant 消息；  
  `ctx.Done()` 分支直接 return，不会保存“已输出但被打断”的 assistant。

- 历史组装入口：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go:1050`  
  `GetMessages()` 当前直接把历史 `msg` 追加到请求，未根据 `Extra` 做内容增强。

---

## 3. 设计原则

1. **最小侵入**：仅改 `llm.go` 的历史保存与历史组装逻辑。  
2. **不污染原始历史**：在组装请求时复制消息再改 `content`，不原地修改内存中的历史对象。  
3. **避免重复落库**：中断落历史只做一次，且与正常 `IsEnd` 路径互斥。  
4. **向后兼容**：未带 `Extra.interrupt` 的历史保持原行为。

---

## 4. 方案细节

### 4.1 打断时写入 Extra（LLM 阶段）

改动位置：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go:324` 的 `handleLLMResponse()`

新增逻辑：

1. 在函数内引入局部状态：
   - `assistantSaved bool`：防止同一次处理重复保存。

2. 抽一个内部 helper（函数内局部闭包或私有方法），在 `ctx.Done()` 触发时执行：
   - 从 `fullText.String()` 取当前已累计文本；
   - 若文本为空，跳过；
   - 构造 `assistantMsg := schema.AssistantMessage(text, nil)`；
   - 设置：
     - `assistantMsg.Extra["interrupt"] = true`
     - `assistantMsg.Extra["interrupt_by"] = "user"`
     - `assistantMsg.Extra["interrupt_stage"] = "llm"`
   - `AddLlmMessage(ctx, assistantMsg)` 保存历史。

3. 在 `ctx.Done()` 的多个返回点调用该 helper，再返回。

备注：
- 正常 `IsEnd` 路径保持不变（不加 interrupt 标识）。
- 仅在确有已累计文本时保存，避免空 assistant 消息。

---

### 4.2 组装历史时按 Extra 增强 content

改动位置：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go:1050` 的 `GetMessages()`

新增逻辑：

1. 遍历历史消息时，不直接 append 原 `msg`，而是先做浅拷贝（必要字段复制）。  
2. 如果满足：
   - `msg.Role == schema.Assistant`
   - `msg.Extra != nil`
   - `msg.Extra["interrupt"] == true`
   - `msg.Content` 非空
   
   则将请求侧内容改为：
   - `newMsg.Content = msg.Content + " [用户打断]"`

3. 为避免重复追加，若内容已以 `" [用户打断]"` 结尾则不重复追加。

注意：
- 只在“请求组装副本”里改 `content`，不改原历史。

---

### 4.3 历史尾部 user 过滤（避免污染当前轮 user）

改动位置：`/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go:1050` 的 `GetMessages()`

新增逻辑：

1. 在 `messageList := l.clientState.GetMessages(count)` 后，先检查“历史消息最后一条”：
   - 若最后一条 `Role == schema.User`，则从 `messageList` 中移除该条。

2. 仅过滤“尾部连续 user”：
   - 推荐循环从尾部回退，删除连续 `user`，直到尾部不是 `user` 或列表为空。

目的：
- 防止历史里残留的上一轮 user 文本与本轮 `userMessage` 混在一起，污染当前会话意图。

注意：
- 这是“组装请求时过滤”，不改内存中的原始历史数据。

---

## 5. 建议补充的辅助函数

建议放在 `llm.go` 私有方法区域：

1. `isInterruptedMessage(msg *schema.Message) bool`  
   统一判断 `Extra.interrupt`（支持 bool/字符串 `"true"` 容错）。

2. `decorateInterruptedContent(content string) string`  
   统一追加逻辑，避免重复 `" [用户打断]"`。

3. `cloneMessageForRequest(msg *schema.Message) *schema.Message`  
   复制 `Role/Content/Name/ToolCalls/ToolCallID/Extra/ResponseMeta`（最少保证 `Content` 与 `Extra` 可安全改写）。

---

## 6. 兼容性与风险

1. `Extra` 对模型是否直接生效：  
   当前 OpenAI 适配层组请求时不透传 `Extra`，所以模型行为主要由我们追加到 `content` 的 `" [用户打断]"` 决定。

2. 历史存储差异：  
   - `redis` 模式下，`schema.Message` 直接 JSON 入库，`Extra` 可保留。  
   - `manager` 模式当前只存 `role/content/tool_calls`，`Extra` 可能丢失。  
   因此若未来在 manager 模式也需要该能力，需要同步扩展 manager history 协议。

3. 文案影响：  
   `" [用户打断]"` 是显式提示，会影响模型续写风格；这是本需求预期行为。

---

## 7. 验收标准（确认后实施）

1. 场景：user 已入历史，assistant 流式中途打断  
   - 历史中新增 assistant 一条，`Extra.interrupt=true`。

2. 下一轮发 LLM 前查看请求消息  
   - 对应 assistant 内容变为 `"<原文片段> [用户打断]"`。

3. 非打断完成消息  
   - `Extra.interrupt` 不存在，`content` 不加前缀。

4. 当历史尾部为 user 时，请求中该尾部 user 被过滤，不与当前轮 user 重复/混杂。

5. 不出现重复标记、不出现空 assistant 记录。

---

## 8. 实施文件清单（确认后）

- `/Users/shijingbo/git/xiaozhi-esp32-server-golang/internal/app/server/chat/llm.go`
- （可选）`/Users/shijingbo/git/xiaozhi-esp32-server-golang/test/interrupt_history/main.go` 用于验证演示
