# Chat Hook / Plugin V2 设计文档

## 1. 文档目标

本文档用于回答三个问题：

1. 当前聊天链路 Hook 架构的合理边界是什么；
2. V2 应该优先补哪些能力；
3. 在尽量不扰动现有业务主链路的前提下，如何分阶段演进。

本文档的定位不是“构建一个完整插件市场”，而是为当前仓库落地一套**可治理、可扩展、可观测**的 Chat Hook / Plugin V2 方案。

---

## 2. TL;DR

### 2.1 一句话结论

当前实现已经具备一个可用的 **Chat Hook Framework** 雏形，但还不适合直接定义为“完整 Plugin Platform”。

### 2.2 V2 核心主张

V2 的核心不是“推翻重写”，而是：

- 保留现有业务接入点；
- 将 **Interceptor（可改写主流程）** 与 **Observer（只做观测）** 明确拆分；
- 为异步执行补齐 **有界队列、超时、丢弃策略、指标**；
- 引入 **PluginMeta + Registry + Lifecycle**；
- 为 ASR / LLM / TTS / Metric 事件补齐契约。

### 2.3 推荐优先级

| 优先级 | 建议项 | 目标 |
| --- | --- | --- |
| P0 | Async Runtime 治理 | 防止慢插件拖垮系统 |
| P0 | Interceptor / Observer 语义拆分 | 降低语义混用 |
| P0 | Payload 契约文档 | 降低插件误用 |
| P1 | PluginMeta / Registry | 让插件注册可管理 |
| P1 | Lifecycle / Config | 支持带状态插件 |
| P2 | 多队列 / 多 worker / tracing 集成 | 支持更复杂扩展 |

---

## 3. 现状判断

## 3.1 当前设计已经做对的部分

当前实现已经具备以下优点：

1. **接入点选得对**
   - Hook 已经接在 ASR 最终输出、LLM 输入/输出、TTS 输入/输出起止、Metric 阶段；
   - 都是聊天主链路中最有业务价值的位置。

2. **分层基本正确**
   - `internal/pkg/hooks` 负责通用执行框架；
   - `internal/domain/chat/hooks` 负责 chat 领域上下文、事件和 typed payload；
   - `internal/app/server/chat/*` 只负责在合适的位置 emit。

3. **Typed façade 已经建立**
   - 业务侧已经不是直接操作 `any`；
   - 这为 V2 继续增强契约与治理提供了良好基础。

4. **可插拔性已经可用**
   - 当前已经能支撑统计插件、文本改写、流程拦截等内建能力。

## 3.2 当前主要短板

当前最需要解决的不是“抽象不对”，而是“治理能力不足”。

### A. 语义混用

当前统一用一套 Hook 模型承载两类完全不同的需求：

- 可改写主流程的拦截器；
- 只做观测的 metric / audit / telemetry。

这会带来以下问题：

- `stop` 语义并不适合所有事件；
- `payload 改写` 只适合一部分阶段；
- 插件作者不容易理解哪些事件能做什么。

### B. 异步执行治理不足

当前 async 执行的痛点：

- 队列无上限；
- 缺少 timeout；
- 缺少 dropped 指标；
- 所有 async handler 共用单线程串行消费；
- 对慢插件缺少隔离。

### C. 插件注册方式偏硬编码

当前主要通过 `RegisterBuiltinPlugins` 在代码中注册。这样虽然简单，但不利于：

- 查看当前加载了哪些插件；
- 做启停开关；
- 做按环境配置；
- 做插件级别调试。

### D. 契约不清晰

目前虽然已经有 `ASROutputData` / `LLMInputData` / `LLMOutputData` / `TTSInputData` / `MetricData`，但仍缺少以下约束：

- 哪些字段允许修改；
- 哪些字段不允许置空；
- `stop` 的业务语义是什么；
- `Err` 是否允许覆盖；
- 插件最大允许耗时是多少。

---

## 4. V2 定位与设计原则

## 4.1 架构定位

V2 建议将系统正式命名为：

> **Chat Interceptor & Observer Framework**

而不是直接叫：

> Plugin Platform

这样命名更贴近当前阶段的真实能力，也更有利于控制团队预期。

## 4.2 设计原则

V2 应遵循以下原则：

1. **保持业务主链路稳定**
   - 不大规模改 ASR / LLM / TTS 现有逻辑；
   - 优先在 Hook Runtime 和 Domain Facade 层增强。

2. **先做治理，再做平台化**
   - 先解决语义、边界、监控、稳定性；
   - 再考虑更复杂的插件生态。

3. **先区分 Interceptor 与 Observer**
   - 所有可改写主流程的行为必须显式归类为 Interceptor；
   - 所有只读观测行为必须显式归类为 Observer。

4. **先明确契约，再扩展插件数量**
   - 没有契约时，插件越多，维护成本越高。

5. **增量演进，不做一次性重写**
   - 能兼容现有 emit 接口的设计优先；
   - 迁移要分阶段推进。

---

## 5. V2 总体架构

## 5.1 三层结构

V2 延续当前三层结构，但强化职责边界。

### 第一层：业务主链路层

职责：

- 在 ASR / LLM / TTS / Session Metric 的关键节点 emit；
- 不直接感知插件注册、调度策略、生命周期。

不负责：

- 插件注册；
- 插件执行治理；
- 插件元数据管理。

### 第二层：Chat Domain Hook 层

职责：

- 定义 chat 领域事件、typed payload、领域 context；
- 对业务代码暴露统一而稳定的入口；
- 约束字段契约与 stop/error 语义。

### 第三层：Generic Runtime 层

职责：

- 插件注册；
- 排序与执行；
- async 调度；
- timeout / drop / metrics；
- 生命周期管理。

## 5.2 请求路径中的 Hook 角色

```text
ASR final text
  -> ASR Output Interceptors
  -> LLM Input Interceptors
  -> LLM 执行
  -> LLM Output Interceptors
  -> TTS Input Interceptors
  -> TTS Output Observers

同时：
  Metric Observers 在 turn_start / asr_first / asr_final / llm_start /
  llm_first / llm_end / tts_start / tts_first / tts_stop 等阶段观测
```

这个拆法的核心是：

- 业务主链路只关心“何时 emit”；
- Interceptor 负责改写；
- Observer 负责观测；
- Runtime 负责治理。

---

## 6. 事件模型设计

## 6.1 事件分层

### A. Interceptor 类事件

用于同步改写与流程控制。

建议保留：

- `chat.asr.output`
- `chat.llm.input`
- `chat.llm.output`
- `chat.tts.input`

这类事件应具备：

- 按 priority 有序执行；
- 可修改 payload；
- 可 `stop`；
- 可返回 error；
- 必须快速返回。

### B. Observer 类事件

用于观测、埋点、日志、trace、审计等。

建议归类为 Observer：

- `chat.metric`
- `chat.tts.output.start`
- `chat.tts.output.stop`
- 后续扩展的 audit / trace / debug event

这类事件应具备：

- 默认只读；
- 不允许 stop 主流程；
- 不参与主流程 payload 变更；
- 可以异步执行；
- 错误只影响观测链路。

## 6.2 事件命名建议

继续沿用现有分层命名即可，不建议马上大改命名体系。推荐保持：

- `chat.asr.output`
- `chat.llm.input`
- `chat.llm.output`
- `chat.tts.input`
- `chat.tts.output.start`
- `chat.tts.output.stop`
- `chat.metric`

原因：

- 当前命名已经直观；
- 与现有实现兼容；
- 迁移成本最低。

---

## 7. 运行时设计

## 7.1 PluginMeta

V2 引入统一元数据，便于展示、启停、诊断和排序。

```go
package hooks

type PluginKind string

const (
    PluginKindInterceptor PluginKind = "interceptor"
    PluginKindObserver    PluginKind = "observer"
)

type PluginMeta struct {
    Name        string
    Version     string
    Description string
    Priority    int
    Enabled     bool
    Kind        PluginKind
    Stage       string
}
```

### 设计说明

- `Name`：全局唯一标识；
- `Version`：便于后续兼容与灰度；
- `Priority`：排序依据；
- `Enabled`：运行期开关；
- `Kind`：区分 interceptor / observer；
- `Stage`：声明所挂载的阶段。

## 7.2 Registry

V2 推荐引入显式注册中心，而不是让 Runtime 自己决定“有哪些插件”。

```go
package hooks

type Registration struct {
    Meta     PluginMeta
    Register func(*Hub)
}

type Registry interface {
    Add(reg Registration)
    List() []Registration
}
```

### Registry 负责什么

- 保存插件定义；
- 暴露可枚举的注册清单；
- 支持按配置过滤是否启用；
- 为调试和观测提供基础数据。

### Registry 不负责什么

- 不直接执行插件；
- 不直接承载业务状态；
- 不替代 Runtime 的执行逻辑。

## 7.3 Lifecycle

为带状态插件提供最小生命周期。

```go
package hooks

type Lifecycle interface {
    Init(context.Context) error
    Close() error
}
```

建议：

- 无状态插件可不实现；
- 有缓存、后台任务、连接池的插件实现该接口；
- Runtime 统一管理调用时机。

## 7.4 Interceptor 接口

```go
package hooks

type Interceptor[T any] interface {
    Meta() PluginMeta
    Handle(Context, T) (T, bool, error)
}
```

设计意图：

- 保留“改写 + stop + error”三种能力；
- 利用泛型提升编译期约束；
- 降低 `any` 造成的误用风险。

## 7.5 Observer 接口

```go
package hooks

type Observer[T any] interface {
    Meta() PluginMeta
    Handle(Context, T)
}
```

设计意图：

- 明确“只观察，不改写”；
- 从语义上消除对 `stop` 的误用；
- 方便后续对 observer 做独立调度策略。

## 7.6 Async Runtime

### 当前问题

当前 async 执行模型最大的问题不是“不能工作”，而是“缺少边界”。

### V2 设计目标

为 async observer 增加以下能力：

- bounded queue；
- timeout；
- dropped 统计；
- per-plugin 执行指标；
- 后续可扩展为多队列 / 多 worker。

### 建议配置

```go
package hooks

type AsyncConfig struct {
    QueueSize    int
    WorkerCount  int
    DropWhenFull bool
    Timeout      time.Duration
}
```

### 推荐默认值

- `QueueSize = 1024`
- `WorkerCount = 1`
- `DropWhenFull = true`
- `Timeout = 200ms`

### 推荐策略

1. 默认先保持单 worker，保证顺序语义；
2. 队列满时优先丢弃 observer 事件，而不是拖慢主链路；
3. 记录 dropped count 和 timeout count；
4. 若未来出现高负载 observer，再按事件或插件拆分队列。

---

## 8. 领域契约

V2 必须把 payload 契约显式写清楚。

## 8.1 ASROutputData

用途：ASR 最终文本与说话人结果改写。

| 字段 | 是否可改 | 说明 |
| --- | --- | --- |
| `Text` | 是 | 可做清洗、归一化、过滤 |
| `SpeakerResult` | 是 | 可做说话人修正或增强 |

约束：

- 插件不得长时间阻塞；
- `stop=true` 表示本轮文本不再继续进入 LLM；
- 若返回空文本，应明确由插件自行承担后果。

## 8.2 LLMInputData

用途：在发起 LLM 请求前，对消息与工具做改写。

| 字段 | 是否可改 | 说明 |
| --- | --- | --- |
| `UserMessage` | 是 | 不允许置空 |
| `RequestMessages` | 是 | 可裁剪、重排、注入 system prompt |
| `Tools` | 是 | 可做过滤或附加 |

约束：

- `UserMessage` 不允许为 `nil`；
- 插件必须保证输出仍满足下游 LLM Provider 的最小输入要求；
- `stop=true` 表示本次 LLM 请求被拦截终止。

## 8.3 LLMOutputData

用途：在 LLM 输出完成后，改写展示文本或补充错误语义。

| 字段 | 是否可改 | 说明 |
| --- | --- | --- |
| `FullText` | 是 | 可做安全改写、格式整理、语音友好化 |
| `Err` | 谨慎 | 建议附加上下文，不建议直接吞掉底层错误 |

约束：

- 不建议插件无痕覆盖底层真实错误；
- `stop=true` 表示后续不再继续进入 TTS 或消息更新；
- 未来可将 `Err` 拆成 `OriginErr` / `DisplayErr`。

## 8.4 TTSInputData

用途：在文本进入 TTS 前做可朗读化处理。

| 字段 | 是否可改 | 说明 |
| --- | --- | --- |
| `Text` | 是 | 可做数值、标点、emoji 可朗读化 |
| `IsStart` | 默认否 | 视为协议边界字段 |
| `IsEnd` | 默认否 | 视为协议边界字段 |

约束：

- 普通插件建议只改 `Text`；
- `IsStart` / `IsEnd` 应保留给更高权限或专用插件；
- `stop=true` 表示当前片段不进入 TTS。

## 8.5 MetricData

用途：链路观测。

| 字段 | 是否可改 | 说明 |
| --- | --- | --- |
| `Stage` | 否 | 只读 |
| `Ts` | 否 | 只读 |
| `Err` | 否 | 只读，仅用于观测 |

约束：

- 不允许 stop 主流程；
- 不允许改写后再反馈给主链路；
- 仅用于日志、指标、追踪、调试。

---

## 9. 配置模型

推荐为 Hook 系统增加最小配置模型：

```yaml
chat_hooks:
  enabled: true
  async:
    queue_size: 1024
    worker_count: 1
    drop_when_full: true
    timeout_ms: 200
  plugins:
    statistic_plugin:
      enabled: true
      priority: 100
```

配置设计目标：

- 支持插件启停；
- 支持 priority 覆盖；
- 支持 async 运行参数控制；
- 为未来插件级 config schema 预留空间。

---

## 10. 可观测性要求

Runtime 至少应采集以下指标：

- plugin 调用次数；
- plugin 耗时；
- error 次数；
- stop 次数（interceptor）；
- dropped 次数（observer async）；
- timeout 次数；
- 当前 async queue 长度。

建议日志/指标中包含：

- `plugin_name`
- `plugin_kind`
- `stage`
- `priority`
- `duration_ms`
- `result`

如果后续接 tracing，可进一步记录：

- session_id
- device_id
- turn_id
- correlation_id

---

## 11. 迁移计划

## 11.1 阶段 1：Runtime 增强（P0）

目标：在不改业务接入点的前提下提升稳定性。

工作项：

- 为 async observer 增加 bounded queue；
- 增加 timeout 与 dropped 统计；
- 增加 Runtime 基础指标；
- 保持现有 `Emit` / `RegisterSync` / `RegisterAsync` 接口兼容。

产出：

- 稳定性提升；
- 为后续 observer 扩展提供边界。

## 11.2 阶段 2：语义分层（P0）

目标：显式区分 Interceptor 与 Observer。

工作项：

- 在 Domain Hook 层新增清晰 façade；
- 将 `Metric` 事件转为标准 observer 语义；
- 明确不允许 stop 的事件集合。

产出：

- 语义更清晰；
- 插件作者更不容易误用。

## 11.3 阶段 3：Registry + Meta（P1）

目标：将“有哪些插件”从硬编码逻辑中抽离。

工作项：

- 引入 `PluginMeta`；
- 引入 `Registration` / `Registry`；
- 支持按配置启停插件；
- 支持列出当前已加载插件。

产出：

- 注册透明；
- 便于调试、观测和配置治理。

## 11.4 阶段 4：契约与生命周期（P1）

目标：让插件边界正式化。

工作项：

- 固化 payload 契约；
- 引入 `Lifecycle`；
- 为有状态插件增加初始化与关闭流程。

产出：

- 更适合承载复杂内建插件；
- 便于演进到更完整的插件体系。

## 11.5 阶段 5：高级能力（P2）

目标：支撑更复杂插件生态。

工作项：

- 按事件拆分队列；
- 多 worker observer runtime；
- tracing / metrics 深度集成；
- 针对重插件的隔离执行策略。

---

## 12. 非目标

V2 当前**不追求**：

- 第三方不受信插件沙箱；
- 进程外 RPC 插件体系；
- 热加载复杂插件生态；
- 完整插件市场。

这些能力应在未来更高版本评估，不应提前引入复杂度。

---

## 13. 落地建议

如果只允许当前迭代做 3 件事，推荐顺序如下：

1. **先做 Async Runtime 治理**
   - 这是稳定性收益最高的一步。

2. **再做 Interceptor / Observer 语义拆分**
   - 这是降低误用风险最有效的一步。

3. **补齐契约与 Meta/Registry**
   - 这是把系统从“能用”推进到“可管理”的关键一步。

---

## 14. 总结

V2 的目标不是把当前 Hook 体系包装成一个听起来更大的“Plugin Platform”，而是把它演进成一个真正：

- 语义清晰；
- 执行可控；
- 可观测；
- 可渐进扩展；
- 与当前聊天主链路兼容的扩展框架。

因此，V2 最重要的不是“增加更多插件”，而是先完成以下三件事：

- 把 **Interceptor** 与 **Observer** 分清；
- 把 **async runtime 边界** 补齐；
- 把 **payload 契约与插件元数据** 建立起来。

完成这三步后，当前仓库的 Hook 体系才算真正具备长期演进的基础。
