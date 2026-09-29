# 知识库功能说明

本文档介绍项目中的 **知识库（Knowledge Base / RAG）** 功能，包括管理员侧 provider 配置、普通用户侧知识库与文档管理、召回测试，以及主程序聊天链路中的知识库检索集成。

相关文档：

- [管理后台使用指南](./manager_console_guide.md)
- [MCP 架构说明](./mcp.md)（知识库检索工具 `search_knowledge` 会通过本地工具链路触发）

---

## 1. 功能概览

知识库功能用于为智能体提供“文档依据型回答”能力，包含三层：

1. 管理员配置知识库检索 provider（Dify / RAGFlow / WeKnora）
2. 普通用户创建知识库与文档，并异步同步到 provider
3. 智能体关联知识库，在对话时触发本地 `search_knowledge` 工具完成召回

当前前端管理页已支持的 provider：

- `dify`
- `ragflow`
- `weknora`

---

## 2. 角色分工

## 2.1 管理员

负责：

- 配置知识库检索 provider（全局）
- 维护 provider 连接参数与默认阈值
- （可选）代用户管理知识库

入口：

- `管理员 -> 知识库检索配置`

## 2.2 普通用户

负责：

- 创建/编辑/删除自己的知识库
- 管理知识库文档（文本录入 / 文件上传）
- 发起手动同步与重试
- 使用“召回测试”验证关键词命中效果
- 在智能体中选择关联知识库

入口：

- `普通用户 -> 我的知识库`
- `普通用户 -> 智能体编辑（关联知识库）`

---

## 3. 管理员：知识库检索配置（Provider 配置）

管理端支持维护多个 provider 配置，并指定默认 provider。

常见配置项（按 provider 不同有所差异）：

- `Base URL`
- `API Key / Token`
- 默认检索阈值
- provider 特定参数（如 RAGFlow 相似度阈值、WeKnora 分块参数等）

### 3.1 Dify

典型配置项：

- `base_url`
- `api_key`
- `score_threshold`
- 其他 provider 参数

### 3.2 RAGFlow

典型配置项：

- `base_url`
- `api_key`
- `similarity_threshold`

### 3.3 WeKnora

典型配置项：

- `base_url`
- `api_key`
- `score_threshold`
- 分块参数（`chunk_size` / `chunk_overlap` / `separators`）
- 解析轮询参数（`parse_poll_interval_ms` / `parse_timeout_ms`）

管理页还支持拉取 WeKnora 模型列表（embedding / llm / rerank）辅助填写配置。

---

## 4. 普通用户：我的知识库（KB 管理）

入口：

- `普通用户 -> 我的知识库`

支持操作：

- 新增/编辑知识库
- 设置状态（`active` / `inactive`）
- 设置检索阈值（可继承全局）
- 文档管理
- 手动重试同步
- 召回测试
- 删除知识库

### 4.1 知识库字段（用户可见）

常见展示列：

- ID
- 名称
- 描述
- 提供商
- 状态
- 同步状态
- 最近同步时间
- 操作

说明：

- 同步失败时，错误信息会以“提示（tooltip）”形式显示在“同步状态”列中，避免表格横向过宽

### 4.2 同步状态（常见）

知识库与文档都可能出现类似状态：

- 待同步
- 上传中 / 已上传 / 解析中
- 已同步
- 失败（含上传失败、解析失败等）

如失败可点击 `重试同步` 重新入队异步任务。

---

## 5. 文档管理（知识库下）

每个知识库可包含多条文档，支持：

- 文本型文档（在线编辑）
- 文件上传创建文档（按 provider 限制格式）

页面功能：

- 新增文档
- 编辑文档（文件型文档通常不支持在线编辑）
- 删除文档
- 重试同步
- 文件上传

### 5.1 文件上传格式

前端会根据当前知识库 provider 展示不同的 `accept` 提示与上传说明：

- Dify：支持常见文本/文档格式（如 txt/md/pdf/html/xlsx/docx/csv 等）
- RAGFlow：支持更广文件类型（含图片、日志、配置文件等）
- WeKnora：支持较广文件类型（含 Office、图片、邮件等）

具体可上传格式请以页面提示为准。

---

## 6. 召回测试（用户侧）

知识库列表中可对单个知识库执行 `召回测试`，用于直接验证 provider 检索效果。

测试项：

- `query`：测试关键词或问题
- `top_k`
- `threshold`（仅本次测试生效，可为空）

返回内容：

- 命中条数
- 命中来源（title）
- score
- 命中文本片段
- 响应耗时

### 6.1 阈值优先级（逻辑说明）

通常按以下优先级取阈值：

1. 本次测试请求阈值（若填写）
2. 知识库自身阈值
3. provider 全局默认阈值

### 6.2 WeKnora 参数说明（重要）

当前 WeKnora 召回测试已按知识库维度使用：

- `knowledge_base_ids`（知识库 ID 列表）

用于精确限制检索范围到当前知识库。

---

## 7. 智能体关联知识库

在智能体编辑页可为智能体选择多个知识库（多选）。

行为说明：

- 支持多库关联
- 对话时会根据模型判断是否触发知识库检索
- 若可判断具体知识库，工具调用会传 `knowledge_base_ids`
- 检索失败时会降级为普通 LLM 对话（前端有提示文案）

---

## 8. 主程序对话链路中的知识库检索

主程序通过本地工具 `search_knowledge` 实现知识库检索。

工具调用参数核心字段：

- `query`
- `top_k`
- `knowledge_base_ids`（可选，知识库 ID 列表）

行为说明：

- 不传 `knowledge_base_ids`：在当前智能体关联的所有可用知识库中检索
- 传入 `knowledge_base_ids`：仅在指定知识库内检索

这使得模型可以在已知问题归属时缩小检索范围，提升相关性并减少无关召回。

### 8.1 WeKnora 主程序检索参数

当前 WeKnora 主程序检索请求已使用：

- `knowledge_base_ids`

与控制台召回测试保持一致。

---

## 9. 接口清单（用户侧）

### 9.1 知识库 CRUD

- `GET /user/knowledge-bases`
- `POST /user/knowledge-bases`
- `GET /user/knowledge-bases/:id`
- `PUT /user/knowledge-bases/:id`
- `DELETE /user/knowledge-bases/:id`
- `POST /user/knowledge-bases/:id/sync`

### 9.2 召回测试

- `POST /user/knowledge-bases/:id/test-search`

### 9.3 文档管理

- `GET /user/knowledge-bases/:id/documents`
- `POST /user/knowledge-bases/:id/documents`
- `POST /user/knowledge-bases/:id/documents/upload`
- `PUT /user/knowledge-bases/:id/documents/:doc_id`
- `DELETE /user/knowledge-bases/:id/documents/:doc_id`
- `POST /user/knowledge-bases/:id/documents/:doc_id/sync`

### 9.4 智能体关联知识库

- `GET /user/agents/:id/knowledge-bases`
- `PUT /user/agents/:id/knowledge-bases`

---

## 10. 接口清单（管理员侧）

### 10.1 provider 配置管理

- `GET /admin/knowledge-search-configs`
- `POST /admin/knowledge-search-configs`
- `PUT /admin/knowledge-search-configs/:id`
- `DELETE /admin/knowledge-search-configs/:id`

### 10.2 WeKnora 模型拉取（配置辅助）

- `POST /admin/knowledge-search-configs/weknora/models`

### 10.3 管理员代用户管理知识库（按用户维度）

- `GET /admin/users/:id/knowledge-bases`
- `POST /admin/users/:id/knowledge-bases`
- `PUT /admin/users/:id/knowledge-bases/:kb_id`
- `DELETE /admin/users/:id/knowledge-bases/:kb_id`

---

## 11. 常见问题与排查

### 11.1 知识库创建后一直未命中

优先排查：

1. 知识库/文档是否已同步成功
2. 外部 provider 是否已完成索引构建
3. 检索阈值是否过高
4. `query` 是否过于宽泛或偏离文档内容

### 11.2 文件上传后文档不能编辑

文件上传创建的文档通常作为“文件型文档”处理，前端会限制在线编辑，建议删除后重新上传。

### 11.3 WeKnora 检索范围不对

确认：

- 控制台召回测试是否使用当前知识库发起测试
- 智能体工具调用中是否正确传入 `knowledge_base_ids`

---

## 12. 使用建议

- 为不同业务域拆分多个知识库（如售后、产品、合同）
- 使用“召回测试”先调好阈值，再接入智能体
- 在智能体说明中明确何时需要知识库回答，可提升触发质量

