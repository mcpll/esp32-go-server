# MCP 市场功能说明

本文档介绍管理后台中的 **MCP 市场** 功能：如何接入第三方 MCP 市场、聚合发现服务、导入服务配置并纳入系统全局 MCP 服务列表。

相关文档：

- [MCP 架构说明](./mcp.md)
- [管理后台使用指南](./manager_console_guide.md)

---

## 1. 功能定位

MCP 市场用于解决“远程 MCP 服务接入效率低”的问题，支持：

- 配置多个 MCP 市场连接（如 ModelScope 等）
- 聚合多个市场的服务目录
- 查看服务详情（端点、传输协议等）
- 一键导入服务配置到本系统
- 对已导入服务进行启用/禁用/编辑/删除

导入后的服务会参与系统全局 MCP 服务配置的合并（与手工配置的 MCP 服务共同生效）。

---

## 2. 角色权限与入口

角色权限：

- 仅管理员可操作

管理后台入口：

- `管理员 -> MCP市场`

页面包含两个页签：

- `市场发现`
- `已导入服务`

---

## 3. 核心概念

### 3.1 MCP 市场（Market）

表示一个“可被访问的 MCP 市场目录源”，包含：

- 市场名称
- 提供商标识（provider）
- 目录 URL（catalog_url）
- 详情 URL 模板（detail_url_template，可选）
- 鉴权 Token（可选）
- 启用状态

### 3.2 聚合服务列表

系统会从已启用的市场拉取服务目录并聚合展示，支持：

- 搜索服务名/描述/Service ID
- 查看详情
- 导入配置

当部分市场拉取失败时，页面会显示“部分市场拉取失败”的告警列表，不影响其他市场结果显示。

### 3.3 已导入服务

导入后的服务在本系统中形成独立配置项，可直接参与运行时 MCP 服务连接。支持配置：

- 名称
- 传输类型（`sse` / `streamablehttp`）
- URL
- Headers（JSON）
- 来源市场与 provider 标识（可选元信息）
- 启用状态

---

## 4. 常用操作流程（管理员）

## 4.1 新增 MCP 市场连接

在 `市场发现` 页签点击 `新增连接`，填写：

- `提供商`：优先选择内置 provider 预设（会自动填充目录 URL 模板）
- `名称`
- `目录URL`
- `详情URL模板`（可选）
- `启用`
- `Token`（如市场需要）

建议先执行连接测试（见下文）再保存使用。

## 4.2 测试市场连接

在市场列表操作菜单中点击 `测试`：

- 成功会返回“可发现服务数量”
- 失败会提示目录连接/鉴权错误

适合用于排查：

- Token 无效
- 目录 URL 错误
- 市场临时不可用

## 4.3 浏览与搜索聚合服务

在 `聚合服务列表` 区域可：

- 输入关键词搜索服务
- 分页查看聚合结果
- 点击 `详情` 查看服务端点信息

服务详情页通常包括：

- 服务名
- 来源市场
- Service ID
- 描述
- 端点列表（传输协议 + URL）

## 4.4 一键导入服务配置（推荐）

在服务详情弹窗点击 `导入服务配置并热更新`：

- 系统会根据服务详情生成一个或多个导入服务配置
- 导入成功后会刷新“已导入服务”列表
- 页面会切换到 `已导入服务` 页签

“热更新”表示导入配置完成后可立即参与运行时服务集合（无需重启后台）。

## 4.5 手动新增/编辑导入服务

在 `已导入服务` 页签可点击 `新增服务` 手动录入，也可编辑导入项。

关键字段说明：

- `传输`：当前支持 `SSE`、`StreamableHTTP`
- `URL`：远程 MCP 服务入口
- `Headers(JSON)`：用于携带鉴权信息，例如 `Authorization`
- `启用`：禁用后不会参与运行时可用服务集合

`Headers(JSON)` 必须是 JSON 对象，例如：

```json
{
  "Authorization": "Bearer <token>"
}
```

---

## 5. 与全局 MCP 配置的关系

MCP 市场并不是替代 `MCP配置` 页面，而是补充来源。

运行时可用的全局 MCP 服务集合来自两部分合并：

- 管理员在 `MCP配置` 页面手工维护的全局服务
- MCP 市场导入且已启用的服务

因此推荐做法是：

1. 使用 MCP 市场快速发现与导入
2. 在 `MCP配置` / 智能体中按需启用与选择服务

---

## 6. API（后台接口）

以下为管理端相关接口（需管理员权限）：

### 6.1 市场连接管理

- `GET /admin/mcp-markets`
- `POST /admin/mcp-markets`
- `PUT /admin/mcp-markets/:id`
- `DELETE /admin/mcp-markets/:id`
- `POST /admin/mcp-markets/:id/test`

### 6.2 市场发现与详情

- `GET /admin/mcp-market/providers`
- `GET /admin/mcp-market/services`
- `GET /admin/mcp-market/services/:market_id/*service_id`
- `POST /admin/mcp-market/import`

### 6.3 已导入服务管理

- `GET /admin/mcp-market/imported-services`
- `POST /admin/mcp-market/imported-services`
- `PUT /admin/mcp-market/imported-services/:id`
- `DELETE /admin/mcp-market/imported-services/:id`

---

## 7. 常见问题与排查

### 7.1 聚合列表为空

排查顺序：

1. 检查市场连接是否启用
2. 对该市场执行“测试”
3. 检查 Token 是否有效
4. 检查目录 URL / 详情 URL 模板是否正确

### 7.2 导入成功但运行时看不到服务

常见原因：

- 导入服务被禁用
- 全局 MCP 总开关关闭
- 智能体配置了 `mcp_service_names`，且未包含该服务名

### 7.3 编辑市场时 Token 留空会怎样？

编辑弹窗中 Token 留空通常表示“不修改现有 Token”（界面会显示当前脱敏状态提示）。

---

## 8. 使用建议

- 优先使用内置 provider 预设，减少目录接口字段差异导致的问题
- 将需要长期稳定使用的服务导入后统一命名，便于智能体按名称选择
- 对生产环境的远程服务建议使用 `Headers(JSON)` 配置鉴权，并做好 token 轮换

