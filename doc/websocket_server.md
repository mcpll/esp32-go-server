# WebSocket服务器与OTA配置流程说明

本说明面向零基础用户，详细介绍如何配置WebSocket服务器及OTA（固件升级）相关参数。

---

## 1. 配置文件位置

所有主要配置都在：

- `config/config.yaml`

如找不到该文件，也可参考 `config/config.json.git`。

---

## 2. WebSocket服务器配置

### 2.1 作用
WebSocket服务器用于设备与服务器之间的实时通信。

### 2.2 关键配置项
在 `config/config.yaml` 文件中找到如下内容：

```yaml
websocket:
  host: "0.0.0.0"
  port: 8989
```
- `host`：监听地址，通常保持 `0.0.0.0` 即可。
- `port`：监听端口，默认 `8989`，可根据需要修改。

### 2.3 修改方法
如需更改端口为 9000：
```yaml
websocket:
  host: "0.0.0.0"
  port: 9000
```

---

## 3. OTA（固件升级）配置

### 3.1 作用
OTA用于设备自动获取服务器下发的WebSocket/MQTT连接参数和固件升级信息。

### 3.2 关键配置项
在 `config/config.yaml` 文件中找到 `ota` 部分：

```yaml
ota:
  test:
    websocket:
      url: "ws://192.168.208.214:8989/xiaozhi/v1/"
    mqtt:
      enable: false
      endpoint: "192.168.208.214"
  external:
    websocket:
      url: "wss://www.tb263.cn:55555/go_ws/xiaozhi/v1/"
    mqtt:
      enable: false
      endpoint: "www.youdomain.cn"
```
- `test`：内网环境下设备获取的参数，在程序中判断条件是以 192.168或127.0开头。
- `external`：外网环境下设备获取的参数。
- `websocket.url`：设备应连接的WebSocket服务器地址。
- `mqtt.enable`：如果启用，会在ota接口中返回配置的mqtt地址，设备会优先选择mqtt+udp的方式。
- `mqtt.endpoint`：MQTT服务器地址，设备端默认是8883端口(tls连接)，如果带非8883的端口 则会使用非加密的tcp连接。

### 3.3 常见修改举例
- 修改内网WebSocket地址：
  ```yaml
  ota:
    test:
      websocket:
        url: "ws://192.168.1.100:8989/xiaozhi/v1/"
  ```
- 修改外网WebSocket地址：
  ```yaml
  ota:
    external:
      websocket:
        url: "wss://yourdomain.com:55555/go_ws/xiaozhi/v1/"
  ```

---

## 4. OTA接口说明（设备如何获取配置）

1. 设备通过HTTP POST请求 `http://服务器地址:端口/xiaozhi/ota/`。
2. 请求头需包含：
   - `Device-Id`：设备唯一ID（如MAC地址）
   - `Client-Id`：客户端唯一ID
3. 服务器会根据设备IP自动选择 `test` 或 `external` 配置，并返回WebSocket/MQTT等参数。
4. 设备解析返回内容，按 `websocket.url` 连接WebSocket服务器。

---

## 5. 常见问题

- **端口被占用？**
  - 修改 `websocket.port`，重启服务。
- **设备连不上服务器？**
  - 检查 `ota` 配置的 `websocket.url` 是否正确，服务器端口是否开放。
- **需要MQTT？**
  - 设置 `mqtt.enable` 为 `true`，并配置 `endpoint`。

---

如有疑问，建议先检查 `config/config.yaml` 配置项，再查阅本说明。
