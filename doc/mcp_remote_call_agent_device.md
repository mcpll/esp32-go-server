# 设备/智能体维度 MCP 远程调用说明

本文档介绍管理控制台中的 **MCP 远程调用调试能力**，包括：

- 智能体维度 MCP 接入点（Endpoint）生成
- 智能体维度工具列表获取与远程调用
- 设备维度工具列表获取与远程调用
- 管理员与普通用户的权限差异

相关文档：

- [MCP 架构说明](./mcp.md)
- [MCP 市场功能说明](./mcp_market.md)
- [管理后台使用指南](./manager_console_guide.md)

---

## 1. 功能定位

该功能主要用于“调试与验证”：

- 快速查看当前智能体/设备暴露了哪些 MCP 工具
- 在控制台中直接构造参数并调用工具
- 获取智能体维度 MCP endpoint，供外部 MCP 客户端接入测试

适合场景：

- 验证某个远程 MCP 服务是否已生效
- 检查工具 schema / 参数样例
- 排查智能体与设备的 MCP 行为差异

---

## 2. 两种调用维度的区别

## 2.1 智能体维度（Agent）

特点：

- 面向“智能体配置”视角
- 支持获取该智能体的 MCP endpoint（带 token）
- 支持拉取工具列表、直接发起工具调用
- 受智能体配置（如 `mcp_service_names`）影响

常见用途：

- 验证智能体筛选后的可用 MCP 工具集合
- 复制 endpoint 给外部调试客户端使用

## 2.2 设备维度（Device）

特点：

- 面向“具体设备连接”视角
- 直接通过设备当前连接上下文请求工具详情/调用工具
- 通常依赖设备在线与 WebSocket 控制器可用

常见用途：

- 排查“同一个智能体在不同设备上工具表现不一致”
- 验证设备当前在线会话侧的 MCP 能力

---

## 3. 页面入口（管理员 / 普通用户）

### 3.1 管理员

- `管理员 -> 智能体管理`（智能体维度 endpoint / tools / call）
- `管理员 -> 设备管理`（设备维度 tools / call）

### 3.2 普通用户

- `我的智能体`（智能体维度 tools / call）
- `我的设备` / `智能体设备`（设备维度 tools / call）
- `智能体编辑`（配置 `mcp_service_names`，影响智能体维度可见服务范围）

---

## 4. 智能体维度：完整调试流程

## 4.1 配置智能体可用 MCP 服务（可选但推荐）

在智能体编辑页可设置 `mcp_service_names`（服务名列表，逗号分隔）：

- 留空：使用全部已启用的全局 MCP 服务
- 填写：仅使用指定服务名（必须是系统中已启用且存在的服务）

系统会对该字段做：

- 去重
- 去空格
- 合法性校验（服务名必须存在于已启用全局服务集合）

## 4.2 获取智能体 MCP Endpoint

控制台可获取智能体专属 MCP 接入点 URL，格式类似：

```text
ws(s)://<host>/mcp?token=<jwt>
```

说明：

- endpoint 基于默认 OTA 配置中的 `external.websocket.url` 推导域名与协议
- token 中包含当前用户与智能体上下文（用于权限校验/绑定用途）
- 适合外部 MCP 客户端临时调试，不建议公开分享

## 4.3 获取工具列表

控制台会请求智能体维度 MCP 工具详情，返回内容通常包含：

- `name`
- 工具描述
- 参数 schema
- 参数样例（若设备端/服务端提供）

如果无法获取（例如控制器未初始化或客户端暂不可达），后端会返回空列表而不是报错，便于页面继续操作。

## 4.4 直接调用工具

在控制台中填写：

- `tool_name`
- `arguments`（JSON）

发起调用后可在结果框查看完整返回体（JSON 格式）。

---

## 5. 设备维度：完整调试流程

## 5.1 获取设备工具列表

选择设备后，控制台会使用设备标识（内部会映射到设备名）向 WebSocket 控制器请求 MCP 工具详情。

常见失败情况：

- 设备不在线
- 设备不属于当前用户（用户视角）
- WebSocket 控制器暂不可用

在这些情况下，接口通常返回空工具列表或权限错误。

## 5.2 调用设备 MCP 工具

与智能体维度类似，填写：

- `tool_name`
- `arguments`（JSON）

区别在于调用体使用的是 `device_id`（实际后端会传设备名）上下文，因此更接近“当前设备会话”的真实执行环境。

---

## 6. 权限与接口差异（管理员 vs 普通用户）

### 6.1 普通用户接口

智能体维度：

- `GET /user/agents/:id/mcp-endpoint`
- `GET /user/agents/:id/mcp-tools`
- `POST /user/agents/:id/mcp-call`

设备维度：

- `GET /user/devices/:id/mcp-tools`
- `POST /user/devices/:id/mcp-call`

智能体服务筛选辅助：

- `GET /user/agents/:id/mcp-services/options`

普通用户仅能操作属于自己的智能体/设备。

### 6.2 管理员接口

智能体维度：

- `GET /admin/agents/:id/mcp-endpoint`
- `GET /admin/agents/:id/mcp-tools`
- `POST /admin/agents/:id/mcp-call`

设备维度：

- `GET /admin/devices/:id/mcp-tools`
- `POST /admin/devices/:id/mcp-call`

管理员可跨用户调试任意智能体/设备（前提是记录存在且连接链路正常）。

---

## 7. Endpoint 生成逻辑（智能体维度）

智能体 endpoint 生成依赖：

1. 默认 OTA 配置（`type=ota` 且 `is_default=true`）
2. OTA 配置中的 `external.websocket.url`
3. 基于当前用户 ID + 智能体 ID 生成的稳定 token

生成结果会使用：

- 同协议（`ws` / `wss`）
- 同 host（域名/IP + 端口）
- 固定路径 `/mcp`

因此如果无法生成 endpoint，请优先检查 OTA 外网 WebSocket 配置。

---

## 8. 常见问题与排查

### 8.1 工具列表为空

可能原因：

- 设备不在线（设备维度）
- WebSocket 控制器未初始化
- 客户端未返回工具详情
- 智能体维度被 `mcp_service_names` 过滤后无可用服务

建议排查顺序：

1. 确认设备在线状态
2. 检查全局 MCP 服务是否启用
3. 检查智能体 `mcp_service_names` 配置
4. 在控制台重试获取工具

### 8.2 调用时报参数 JSON 错误

控制台参数区要求合法 JSON 对象，例如：

```json
{
  "query": "hello"
}
```

常见错误：

- 单引号
- 尾逗号
- 顶层不是对象

### 8.3 获取智能体 endpoint 失败

通常是 OTA 默认配置缺失或 `external.websocket.url` 未配置。

### 8.4 明明导入了 MCP 服务，但智能体调用看不到

检查：

1. 导入服务是否启用
2. 全局 MCP 配置总开关与服务启用状态
3. 智能体是否通过 `mcp_service_names` 排除了该服务

---

## 9. 最佳实践

- 先在管理员侧验证“设备维度”工具可用，再验证“智能体维度”工具筛选结果
- 对生产智能体建议显式配置 `mcp_service_names`，避免无关工具暴露给模型
- 将 endpoint 视为敏感调试入口，避免在公共渠道传播带 token 的 URL

