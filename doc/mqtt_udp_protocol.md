# 🚦 数据流程

1. **调用 OTA 接口**
   - 获取 **MQTT**、**WebSocket** 地址

2. **连接 MQTT**
   - 内置 `mqtt_server` 会发布一条生命周期事件到 `/p2p/device_public/_server/lifecycle`
   - 主程序根据 `device_id` 创建或复用 MQTT transport，并最佳努力预热设备侧 MCP

3. **发送 `hello` 消息**
   - 获取：
     - 🎵 `audio_params`
     - 🌐 UDP 服务器地址
     - 🔑 `aes_key`
     - 🧩 `nonce`

4. **连接 UDP 服务器**
   - 进行语音数据的发送与接收

5. **发送 `listen`、`abort` 等后续信令**
   - 信令语义保持不变，仍基于 `hello` 完成后的聊天级初始化

---

# 🧭 生命周期 Topic

- **Topic**：`/p2p/device_public/_server/lifecycle`
- **用途**：仅供服务端内部使用，用于传递设备 MQTT 上下线事件
- **消息体示例**：
  ```json
  {
    "type": "mqtt_lifecycle",
    "device_id": "11:22:33:44:55:66",
    "state": "online",
    "client_id": "GID_test@@@11_22_33_44_55_66@@@uuid",
    "ts": 1710000000000
  }
  ```

- **状态定义**
  - `online`：设备刚连上 `mqtt_server`，主程序可提前准备 transport 和 MCP
  - `offline`：设备与 `mqtt_server` 断开，主程序立即映射离线状态，但 transport 会保留一段时间用于短时重连复用

- **边界说明**
  - 生命周期事件不替代 `hello`
  - 生命周期事件只维护连接级资源，不承载 `audio_params`、UDP 协商等聊天级信息

---

# 🛠️ 服务端流程

| 步骤 | 说明 |
| :--- | :--- |
| 1. MQTT 生命周期监听 | 收到 `online` 事件时，创建或复用 transport，并最佳努力预热设备侧 MCP |
| 2. `hello` 处理 | 返回 `audio_params`、UDP 地址、密钥和 `nonce`，并准备聊天级会话状态 |
| 3. MQTT 消息监听 | 收到 `type: listen, state: start` 时，初始化 `clientState` 结构，状态为 `start` |
| 4. UDP 服务 | 收到包后解析 `nonce`，查找对应 `clientState`，填充远程地址，状态为 `recv` |
| 5. 停止接收 | 收到 `type: listen, state: stop` 或自动检测无声音时，停止接收 |
| 6. MQTT 生命周期离线 | 收到 `offline` 事件时，立即映射离线状态，并在保留期后再回收 transport |

---

# 🔗 关联关系

- OTA 验证 **MAC 地址** 和 **clientId**，并关联到 **uid**
- OTA 下发的 **MQTT 地址** 和 **mqtt_clientId** 关联 **MAC 地址** 和 **clientId**
- 通过 **MQTT 连接生命周期消息** 可提前关联 **MAC 地址**、`device_id`、`client_id`
- 通过 **MQTT `hello` 消息** 可关联到 `audio_params`、`aes_key`、`nonce`
- 通过 **UDP 音频消息** 可关联到 `nonce`

---

> **说明：**
> - `clientState` 结构用于维护每个客户端的聊天级会话状态和资源。
> - transport 与 MCP 可在 MQTT 上线阶段提前准备，但真正的聊天级协商仍以 `hello` 为准。
> - `nonce` 是客户端与服务端之间的唯一标识，用于安全关联和数据路由。
