# OTA接口MQTT认证配置

## 概述

OTA接口现在支持基于HMAC-SHA256签名的MQTT密码验证机制，提供更安全的认证方式。同时MQTT服务器也支持相应的验证逻辑。

## 配置结构

### 配置文件 (config/config.yaml)

```yaml
mqtt_server:
  signature_key: "your_ota_signature_key_here"
ota:
  signature_key: "your_ota_signature_key_here"
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

### 配置说明

- `mqtt_server.signature_key`: MQTT签名密钥，用于生成MQTT密码签名
- `ota.signature_key`: OTA下发MQTT 密码 时使用的key，需要与mqtt_server.signature_key对应
- `ota.test`: 测试环境配置（内网IP使用）
- `ota.external`: 外部环境配置（外网IP使用）

### 与 xiaozhi-mqtt-gateway 集成

本系统与虾哥官方的 [xiaozhi-mqtt-gateway](https://github.com/78/xiaozhi-mqtt-gateway) 项目配合使用，实现完整的MQTT认证流程：

1. **配置一致性要求**: `ota.signature_key` 必须与 xiaozhi-mqtt-gateway 项目中的签名密钥完全一致
2. **认证流程**: 
   - xiaozhi-mqtt-gateway 负责生成MQTT连接凭据
   - 本系统负责验证MQTT连接凭据
   - 双方使用相同的签名算法和密钥确保认证成功
3. **部署建议**: 建议将两个项目部署在同一网络环境中，确保配置同步更新

## 工具函数

### 1. 密码签名生成

```go
// 生成HMAC-SHA256密码签名
password := util.GeneratePasswordSignature(data, key)
```

### 2. MQTT凭据生成

```go
// 生成完整的MQTT连接凭据
credentials, err := util.GenerateMqttCredentials(deviceId, clientId, ip, signatureKey)
if err != nil {
    // 处理错误
}
// credentials包含: ClientId, Username, Password
```

### 3. MQTT凭据验证

```go
// 验证MQTT连接凭据
credentialInfo, err := util.ValidateMqttCredentials(clientId, username, password, signatureKey)
if err != nil {
    // 验证失败
}
// credentialInfo包含: GroupId, MacAddress, UUID, UserData
```

## MQTT认证逻辑

### 1. Client ID格式

```
GID_test@@@{deviceId}@@@{clientId}
```

示例：
```
GID_test@@@02_4A_7D_E3_89_BF@@@e3b0c442-98fc-4e1a-8c3d-6a5b6a5b6a5b
```

### 2. Username格式

Base64编码的JSON，包含客户端IP信息：

```yaml
ip: "1.202.193.194"
```

Base64编码后：
```
eyJpcCI6IjEuMjAyLjE5My4xOTQifQ==
```

### 3. Password生成

使用HMAC-SHA256算法生成密码签名：

```go
signatureData := clientId + "|" + username
password := HMAC-SHA256(signatureData, signature_key)
```

### 4. 验证逻辑

客户端验证时需要：

1. 解析clientId，提取groupId、macAddress、uuid
2. 解码username，获取IP信息
3. 使用相同的签名密钥和算法验证密码

## MQTT服务器认证

### 认证流程

1. **超级管理员验证**
   - 用户名: `admin` (可配置)
   - 密码: `shijingbo!@#` (可配置)

2. **普通用户验证**
   - 优先使用HMAC-SHA256签名验证
   - 如果未配置签名密钥，回退到AES验证方式

### 认证钩子实现

```go
func (h *AuthHook) OnConnectAuthenticate(cl *mqttServer.Client, pk packets.Packet) bool {
    username := string(pk.Connect.Username)
    password := string(pk.Connect.Password)
    clientId := string(pk.Connect.ClientIdentifier)

    // 超级管理员校验
    if username == adminUsername && password == adminPassword {
        return true
    }

    // 普通用户校验 - 使用新的签名验证逻辑
    signatureKey := viper.GetString("mqtt_server.signature_key")
    if signatureKey != "" {
        credentialInfo, err := util.ValidateMqttCredentials(clientId, username, password, signatureKey)
        if err != nil {
            return false
        }
        return true
    }

    // 回退到AES验证逻辑
    return h.validateWithAes(username, password)
}
```

## 兼容性

- 如果未配置`mqtt_server.signature_key`，系统会回退到原来的SHA256/AES密码生成方式
- 保持向后兼容性，不会影响现有功能
- MQTT服务器支持多种认证方式并存

## 安全建议

1. 使用强随机字符串作为签名密钥
2. 定期轮换签名密钥
3. 在生产环境中使用HTTPS/WSS连接
4. 监控异常登录尝试
5. 启用日志记录，跟踪认证成功/失败情况
6. **确保 xiaozhi-mqtt-gateway 与本系统的签名密钥同步更新**

## 数据结构

### MqttCredentials
```go
type MqttCredentials struct {
    ClientId string `json:"client_id"`
    Username string `json:"username"`
    Password string `json:"password"`
}
```

### MqttCredentialInfo
```go
type MqttCredentialInfo struct {
    GroupId    string                 `json:"groupId"`
    MacAddress string                 `json:"macAddress"`
    UUID       string                 `json:"uuid"`
    UserData   map[string]interface{} `json:"userData"`
}
``` 

# 虾哥官方 xiaozhi-mqtt-gateway 使用说明

本系统可以与虾哥官方的 [xiaozhi-mqtt-gateway](https://github.com/78/xiaozhi-mqtt-gateway) 项目配合使用。

只需ota接口中MQTT的用户名密码与xiaozhi-mqtt-gateway认证通过，为确保MQTT认证正常工作，**`ota.signature_key` 配置必须与 xiaozhi-mqtt-gateway 中的签名密钥保持一致**。

配置如下:
1. 不启用mqtt server (使用 xiaozhi-mqtt-gateway)
2. `ota.signature_key` 配置必须与 xiaozhi-mqtt-gateway 中的签名密钥保持一致
3. 配置 xiaozhi-mqtt-gateway 的websocket后端为本项目地址

```yaml
mqtt_server:
  enable: false
ota:
  signature_key: "your_ota_signature_key_here"
  test:  # 内网测试的返回
    websocket:
      url: "ws://192.168.208.214:8989/xiaozhi/v1/"
    mqtt:
      enable: true
      endpoint: "192.168.208.214:1883"  # xiaozhi-mqtt-gateway中的mqtt server地址
  external:  # 外网的返回
    websocket:
      url: "wss://www.tb263.cn:55555/go_ws/xiaozhi/v1/"
    mqtt:
      enable: true
      endpoint: "mqtt.youdomain.com:1883"  # xiaozhi-mqtt-gateway中的mqtt server地址
```