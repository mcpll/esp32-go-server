# 小智服务 Windows 使用说明

欢迎使用小智服务 Windows aio 包。本文档包含启动、配置和端口说明。

## 目录结构

```
xiaozhi_server-windows-amd64-<version>/
├── xiaozhi_server.exe          # 主程序
├── onnxruntime.dll             # ONNX Runtime 依赖库
├── sherpa-onnx-c-api.dll       # Sherpa-ONNX 依赖库
├── sherpa-onnx-cxx-api.dll     # Sherpa-ONNX C++ 依赖库
├── ten_vad.dll                 # VAD 依赖库
├── start.bat                   # 启动脚本
├── main_config.yaml            # 主配置文件
├── asr_server.json             # ASR 服务配置
├── models/                     # 模型文件目录
├── data/                       # 数据目录
└── logs/                       # 日志目录
```

## 快速启动

双击 `start.bat` 即可启动服务。启动后可在 `logs/` 目录查看日志。

> 提示：首次启动时，程序会自动下载所需的模型文件（如果 models 目录为空）。

## 端口与服务

| 端口 | 配置来源 | 说明 |
|------|----------|------|
| **8989** | `main_config.yaml` → `websocket.port` | **主服务 WebSocket**：设备/客户端连接 |
| **9000** | `asr_server.json` → `server.port` | **ASR/声纹服务**：语音识别内部接口 |
| **2883** | 控制台配置 | **MQTT 服务**：设备 MQTT 连接 |
| **8990** | 控制台配置 | **UDP 服务**：设备 UDP 通信 |
| **6060** | 控制台配置 | **pprof**：性能分析（默认关闭） |

## 访问地址

### 设备/客户端连接

- **WebSocket**：`ws://<服务器IP>:8989/`
- **MQTT**：`<服务器IP>:2883`
- **UDP**：`<服务器IP>:8990`

## 修改配置

### 需在配置文件中修改的端口

以下端口修改后需重启服务生效：

| 端口 | 配置文件 | 配置项 |
|------|----------|--------|
| 8989 | `main_config.yaml` | `websocket.port` |
| 9000 | `asr_server.json` | `server.port` |

### 其他配置

MQTT、UDP、pprof 以及 LLM、TTS、ASR 写在 `main_config.yaml`。改端口后需要重启。

## 常见问题

### 防火墙提示

首次运行时，Windows 可能会弹出防火墙提示，请允许程序访问网络。

### 端口被占用

如果启动失败提示端口被占用，请：

1. 使用 `netstat -ano | findstr :端口号` 查看占用进程
2. 修改配置文件中的端口号
3. 或结束占用该端口的进程

### DLL 缺失

如果提示缺少 DLL 文件，请确保以下文件与 `xiaozhi_server.exe` 在同一目录：
- `onnxruntime.dll`
- `sherpa-onnx-c-api.dll`
- `sherpa-onnx-cxx-api.dll`
- `ten_vad.dll`

## 停止服务

在启动窗口按 `Ctrl + C` 或直接关闭窗口即可停止服务。
