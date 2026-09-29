# WeKnora 知识库接入方案（待确认）

## 1. 目标与约束
- 在现有 `dify/ragflow` 基础上新增第三个 provider：`weknora`。
- 管理员可配置 WeKnora 连接参数；普通用户侧知识库/文档 CRUD 继续走异步同步。
- 主程序本地 RAG 检索链路支持 `weknora`（不回调控制台检索接口）。
- 保持现有数据结构：不新增数据库列，文档同步仅使用 `sync_status + sync_error`。
- 继续沿用现有删除策略：删文档；若知识库为系统自动创建且远端为空则删知识库。

## 2. 官方 API 依据
- API 总览与鉴权（`X-API-Key`，Base URL `/api/v1`）：
  - <https://github.com/Tencent/WeKnora/blob/main/docs/api/README.md>
- 知识库管理：
  - <https://github.com/Tencent/WeKnora/blob/main/docs/api/knowledge-base.md>
- 知识管理（文件/URL/手工知识、解析状态）：
  - <https://github.com/Tencent/WeKnora/blob/main/docs/api/knowledge.md>
- 知识搜索：
  - <https://github.com/Tencent/WeKnora/blob/main/docs/api/knowledge-search.md>
- 模型管理（用于拿默认 Embedding 模型）：
  - <https://github.com/Tencent/WeKnora/blob/main/docs/api/model.md>

## 3. 接口映射（系统动作 -> WeKnora API）
1. 创建远端知识库（external_kb_id）  
`POST /api/v1/knowledge-bases`

2. 更新远端知识库元信息（名称/描述/分块配置）  
`PUT /api/v1/knowledge-bases/:id`

3. 删除远端知识库  
`DELETE /api/v1/knowledge-bases/:id`

4. 创建文档（上传文件）  
`POST /api/v1/knowledge-bases/:id/knowledge/file`（multipart）

5. 查询文档解析状态  
`GET /api/v1/knowledge/:id`（使用 `parse_status`，`pending/processing/failed/completed`）

6. 删除文档  
`DELETE /api/v1/knowledge/:id`

7. 判断知识库是否空（用于自动删库）  
`GET /api/v1/knowledge-bases/:id/knowledge?page=1&page_size=1`

8. 检索（召回测试 + 主程序 RAG）  
`POST /api/v1/knowledge-search`

## 4. 数据与状态映射
1. 字段映射
- 本地 `knowledge_bases.external_kb_id` <-> WeKnora `knowledge_base.id`
- 本地 `knowledge_base_documents.external_doc_id` <-> WeKnora `knowledge.id`
- `sync_provider = "weknora"`

2. 文档同步状态（单字段）
- 入队后：`pending`
- 发起上传：`uploading`
- 上传成功：`uploaded`
- 解析中：`parsing`
- 解析成功：`synced`
- 上传失败：`upload_failed`
- 解析失败：`parse_failed`
- 入队等内部失败：`failed`

3. 解析轮询策略（建议）
- `parse_poll_interval_ms`: 默认 `1000`
- `parse_timeout_ms`: 默认 `120000`
- 超时按 `parse_failed` 处理并写入 `sync_error`

## 5. 后端改造方案（manager/backend）
1. `manager/backend/controllers/knowledge_sync.go`
- 新增 `weknoraKnowledgeSyncConfig` 及 `parseWeknoraKnowledgeSyncConfig`。
- `syncKnowledgeBaseWithProvider/syncKnowledgeBaseDeleteBestEffort/syncKnowledgeDocumentBestEffort/syncKnowledgeDocumentDeleteBestEffort` 增加 `weknora` 分支。
- 新增 WeKnora HTTP 封装（鉴权头 `X-API-Key`，请求/响应日志格式与现有一致）。
- 文档上传统一走 `/knowledge/file`：
  - 文件型文档：直接转发上传。
  - 文本型文档：转为 UTF-8 `.md` 临时字节流后上传。
- 更新文档采用“新建后删旧”策略，避免依赖不稳定的更新请求体。
- 删除文档后查询远端知识库是否空，满足条件删远端知识库。

2. `manager/backend/controllers/knowledge.go`
- `CreateKnowledgeBaseDocumentByUpload` 的 provider 校验增加 `weknora`。
- 召回测试 `TestKnowledgeBaseSearch` 增加 `queryKnowledgeTestByWeknora` 分支。
- 阈值处理沿用当前规则：请求阈值 > 知识库阈值 > 全局阈值。
- WeKnora 搜索接口若无原生阈值参数，则在本地按 `score` 二次过滤。

3. `manager/backend/controllers/admin.go`
- 现有 `knowledge_search` 汇总结构已支持多 provider，无需改 schema。
- 保持 `knowledge.default_provider + knowledge.providers` 输出结构不变。

## 6. 主程序改造方案（internal/domain/rag）
1. 新增 `internal/domain/rag/weknora_searcher.go`
- 实现 `Searcher` 接口。
- 调用 `POST /api/v1/knowledge-search`，按 `knowledge_base_ids` 精确检索。
- 复用现有并发、超时、容错聚合机制。
- 命中结果映射到 `KnowledgeSearchHit`：
  - `Content <- content`
  - `Title <- knowledge_title`（为空时退化到本地知识库名）
  - `Score <- score`

2. 修改 `internal/domain/rag/manager.go`
- `getSearcher()` 增加 `weknora` 分支。
- provider config 读取逻辑保持不变（从 `knowledge.providers.weknora` 读取）。

## 7. 管理台前端改造（manager/frontend）
1. `manager/frontend/src/views/admin/KnowledgeSearchConfig.vue`
- provider 下拉增加 `weknora`。
- 新增配置项（建议）：
  - `base_url`（默认 `http://127.0.0.1:8080`）
  - `api_key`
  - `score_threshold`（默认 `0.2`）
  - `chunk_size`（默认 `1000`）
  - `chunk_overlap`（默认 `200`）
  - `separators`（默认 `["\\n\\n","\\n","。","！","？",";","；"]`）
  - `enable_multimodal`（默认 `true`）
  - `embedding_model_id`（建议必填）
  - `summary_model_id`（可选）
  - `rerank_model_id`（可选）
  - `vlm_model_id`（可选）
  - `parse_poll_interval_ms`、`parse_timeout_ms`（可选）

2. `manager/frontend/src/views/user/KnowledgeBases.vue`
- provider 展示无需新增列（已有 provider 字段）。
- 上传文件 `accept` 增加 `weknora` 分支。
- 说明文案增加 WeKnora 上传路径与异步解析说明。

## 8. 关键实现决策（建议确认）
1. 文本文档是否强制走手工接口
- 建议首版统一走 `/knowledge/file`（文本封装 `.md`），降低接口差异与兼容风险。

2. Embedding 模型来源
- 建议 `embedding_model_id` 先作为管理员必填。
- 可选增强：若为空，启动时调用 `/api/v1/models` 自动选默认 `Embedding` 模型。

3. 文件格式限制
- WeKnora 文档未给出严格白名单；建议首版采用“较宽松前端限制 + 后端/远端兜底报错”。
- 若你需要严格白名单，可在二期根据实测稳定格式收敛。

## 9. 验证清单
1. 管理员新增 `weknora` 配置并设为默认。
2. 普通用户创建知识库后，自动创建远端 knowledge-base，回写 `external_kb_id`。
3. 新增文本文档/上传文件文档后，状态按 `uploading -> uploaded -> parsing -> synced` 变化。
4. 解析失败时 `sync_status=parse_failed`，并写入 `sync_error`。
5. 删除文档后远端文档被删除；远端空库时按策略自动删库。
6. 召回测试可返回 WeKnora 命中结果，阈值在本地生效。
7. 主程序聊天链路在关联 `weknora` 知识库时可无感触发检索。

## 10. 风险与回滚
1. 风险
- WeKnora 版本差异导致请求体字段变化（尤其知识库创建配置字段）。
- 文档解析耗时长，需轮询与超时策略配合。
- 搜索接口若缺少原生阈值参数，需要本地二次过滤。

2. 回滚
- 仅禁用/删除 `weknora` 配置即可停用，不影响现有 `dify/ragflow`。
- 代码层面 provider 分支可独立回退，不涉及数据库结构变更。

