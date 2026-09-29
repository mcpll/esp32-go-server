# MQTT 生命周期驱动的 Transport 预创建方案

## 目标

在设备连接 / 断开 `mqtt_server` 时，由 `mqtt_server` 通过回调向主程序已监听的 MQTT topic 发布生命周期消息。主程序收到后：

1. 设备上线时提前创建 `mqtt udp transport`
2. 设备上线时最佳努力预热 MCP
3. 设备下线时立即映射设备离线状态
4. 设备下线后保留一段时间 transport，避免短时重连频繁创建 / 销毁
5. 不改变现有 `hello` / `listen` / `abort` / `goodbye` 的信令语义

## Topic 设计

不新增新的根前缀，复用现有 `"/p2p/device_public/"` 前缀。

新增生命周期 topic：

`/p2p/device_public/_server/lifecycle`

对应代码常量建议：

- `MDeviceLifecycleTopic = MDevicePubTopicPrefix + "_server/lifecycle"`

## 生命周期消息格式

消息体使用 JSON：

```json
{
  "type": "mqtt_lifecycle",
  "device_id": "ba:8f:17:de:94:94",
  "state": "online",
  "client_id": "GID_test@@@ba_8f_17_de_94_94@@@uuid",
  "ts": 1710000000000
}
```

字段说明：

- `type`: 固定为 `mqtt_lifecycle`
- `device_id`: 归一化后的设备 ID，统一用冒号格式
- `state`: `online` / `offline`
- `client_id`: 原始 MQTT client id，便于排障
- `ts`: 事件时间戳，毫秒

## 端到端流程

### 1. mqtt_server 发布生命周期消息

在 `DeviceHook` 的：

- `OnSessionEstablished`
- `OnDisconnect`

里，通过回调把生命周期事件发布到 `/p2p/device_public/_server/lifecycle`。

实现上仍然由 `mqtt_server` 负责发布，只是发布动作收敛为 hook 内调用的 callback，避免把 topic 拼接逻辑散在多个位置。

### 2. 主程序复用现有订阅

`MqttUdpAdapter` 继续只订阅现有的：

`/p2p/device_public/#`

收到消息后先判断 topic：

- 如果是 `/p2p/device_public/_server/lifecycle`，走生命周期处理分支
- 否则继续走现有设备业务消息分支

这样不会影响后续 `hello` / `listen` 等正常信令解析。

### 3. 设备上线时预创建 transport

收到 `online` 生命周期消息后：

1. 先做生命周期防抖
2. 若 transport 不存在，则立即创建 `MqttUdpConn + UdpSession`
3. 触发 `onNewConnection`，让主程序创建 `ChatManager`
4. 标记 broker online
5. 触发一次最佳努力的 MCP 预热
6. 映射设备在线状态

注意：

- 这里创建的是 `transport` 和 `ChatManager`
- `ChatSession` 仍然保持为 `hello` 后懒创建

### 4. 设备下线时延迟回收 transport

收到 `offline` 生命周期消息后：

1. 先标记 broker offline
2. 立即映射设备离线状态
3. 启动延迟清理 timer
4. 在 grace period 内保留 `transport + udp session`
5. grace period 内如果再次收到 `online`，取消 cleanup timer 并复用原 transport

默认保留时间建议为 `2m`，后续可配置化。

## 在线状态语义

MQTT-UDP 设备在线状态改为由 MQTT 生命周期驱动，而不是由 `ChatManager` 创建 / 销毁驱动。

也就是：

- MQTT `online` -> 设备在线
- MQTT `offline` -> 设备离线

为了避免重复通知：

- `App.OnNewConnection()` 对 `websocket` 维持原逻辑
- `mqtt udp` 的 `DeviceOnline / DeviceOffline` 改为由 `MqttUdpAdapter` 生命周期回调触发

## 与 hello / listen 的关系

现有聊天信令逻辑不改：

- transport 可以在 MQTT 连接建立后提前存在
- `ChatManager` 可以提前存在
- `ChatSession` 仍然在 `hello` 成功后创建
- `listen` 仍然要求 `hello` 已完成

这样可以做到“transport 预创建”而不改变会话层语义。

## MCP 预热策略

生命周期 `online` 到来后触发一次最佳努力 MCP 预热。

同时保留 `hello` 中现有的 MCP 初始化兜底逻辑。

两条链路并存时，依赖当前分支已有的 MCP 幂等与状态机能力避免重复初始化：

- 上线时优先预热，提升控制台工具可见性
- `hello` 时继续兜底，避免预热缺失影响业务

## 高并发与防抖

按设备维度维护生命周期状态：

- `brokerOnline`
- `lastEventTs`
- `cleanupTimer`
- `cleanupVersion`

防抖规则：

- 旧时间戳事件直接忽略
- 重复 `online` 不重复通知上线
- 重复 `offline` 只刷新 cleanup timer，不重复通知离线
- timer 回调执行时校验 `cleanupVersion`，避免旧 timer 误删新连接

## 需要一起修正的点

由于离线后 transport 会短暂保留，因此“当前在线 transport”解析不能只看 `ChatManager` 是否存在。

需要让 `MqttUdpConn` 暴露 broker online 状态，并让 `ChatManager.GetTransportType()` 在 MQTT transport 已离线时返回空字符串。这样设备维度的 MCP 查询 / 调用仍然严格依赖“当前在线 transport”。

## 涉及文件

- `internal/data/msg/message_types.go`
- `internal/app/mqtt_server/device_hook.go`
- `internal/app/mqtt_server/mqtt_server.go`
- `internal/app/server/mqtt_udp/mqtt_udp_adapter.go`
- `internal/app/server/mqtt_udp/mqtt_udp_conn.go`
- `internal/app/server/app.go`
- `internal/app/server/chat/chat.go`
