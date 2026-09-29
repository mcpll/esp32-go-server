测试小智官方服务器mqtt+udp协议 响应速度
结果:
stt 166ms，llm 300ms左右，首帧音频642ms

## 使用方法

### 基本参数
- `-ota`: OTA服务器地址（默认: https://api.tenclass.net/xiaozhi/ota/）
- `-device`: 设备ID（默认: ba:8f:17:de:94:94）
- `-mode`: 拾音模式，支持 `manual`（手动）或 `auto`（自动），默认: `manual`
- `-tts_provider`: TTS 提供商，支持 `cosyvoice`、`edge`、`edge_offline`、`indextts_vllm`，默认: `cosyvoice`

### 拾音模式说明
- **manual 模式**：需要手动发送 listen stop 消息来停止拾音
- **auto 模式**：自动检测语音结束并停止拾音
- 当前测试程序已支持 MQTT 下发 `speak_request`，设备侧会按协议回 `speak_ready`（即主动播报响应消息）

### 使用示例
```bash
# 使用默认的 manual 模式
./main -device "ba:8f:17:de:94:94"

# 使用 auto 模式
./main -device "ba:8f:17:de:94:94" -mode auto

# 使用 edge_offline 做本地离线 TTS
./main -device "ba:8f:17:de:94:94" -tts_provider edge_offline

# 使用 indextts_vllm
./main -device "ba:8f:17:de:94:94" -tts_provider indextts_vllm
```
