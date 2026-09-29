# 知识库无感触发检索方案（设备聊天，v2）

## 背景
- 当前检索工具 `search_knowledge` 主要依赖模型是否主动调用。
- 需要支持“按命中知识库ID定向检索”，减少无关知识库请求。

## 核心变更
1. 工具入参升级
- `search_knowledge` 增加可选字段 `knowledge_base_ids: number[]`。
- 保留 `query`、`top_k`。
- 兼容逻辑：不传 `knowledge_base_ids` 时，按当前智能体所有可用知识库检索。

2. 定向检索语义
- 传入 `knowledge_base_ids` 时，仅在这些知识库内检索。
- 非法ID（未关联/不存在/inactive/缺少 external_kb_id）自动忽略（best effort）。

3. 并发执行策略
- 按“知识库维度”并发请求，每个命中知识库独立发起检索请求。
- provider 仍由知识库自身配置决定（dify/ragflow）。
- 聚合所有命中后按 score 全局排序，再截断 `top_k`。

4. 超时策略（已确认默认值）
- 单库超时：`2500ms`
- 总超时：`2500ms`
- 超时/部分失败不阻断主流程；若全部失败则返回错误。

5. LLM路由提示升级
- System Prompt 下发“可用知识库 id:名称”列表。
- 引导模型在可判断时传 `knowledge_base_ids`，不确定时可不传。

## 实施步骤
1. `search_knowledge` 参数结构新增 `knowledge_base_ids`。
2. `ChatSessionOperator -> LocalMcpSearchKnowledge -> rag.Search` 调用链透传 `knowledge_base_ids`。
3. `rag.Search` 增加 ID 过滤与总超时控制。
4. `dify_searcher` 与 `ragflow_searcher` 改为按知识库并发检索，并增加单库超时控制。
5. 调整系统提示词中的知识库检索规则，支持 `knowledge_base_ids` 引导。

## 兼容与回退
- 不传 `knowledge_base_ids` 的历史调用不受影响。
- 任一 provider 局部失败仅记日志并跳过，保留其他 provider 成果。

## 验收标准
- 工具可接收并生效 `knowledge_base_ids`。
- 多知识库场景下可并发检索并返回聚合结果。
- 单库与总超时均默认为 2500ms。
- 旧调用路径（不传 `knowledge_base_ids`）行为保持可用。
