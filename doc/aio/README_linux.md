# 小智服务 Linux 使用说明

欢迎使用小智服务 Linux aio 包。本文档包含依赖安装、启动和配置说明。

## 目录结构

```
xiaozhi_server-linux-amd64-<version>/
├── xiaozhi_server              # 主程序
├── ten-vad/
│   └── lib/Linux/x64/
│       ├── libten_vad.so       # VAD 依赖库
│       ├── libsherpa-onnx-c-api.so
│       ├── libsherpa-onnx-cxx-api.so
│       └── libonnxruntime.so   # ONNX Runtime 依赖库
├── main_config.yaml            # 主配置文件
├── manager.json                # 管理后台配置
├── asr_server.json             # ASR 服务配置
├── models/                     # 模型文件目录
├── data/                       # 数据目录
└── logs/                       # 日志目录
```

## 运行依赖

### 系统要求

| 系统 | 最低版本 | 测试状态 |
|------|----------|----------|
| Ubuntu | 18.04 LTS | ✅ 已测试 |
| Debian | 10 (Buster) | ⚠️ 预期兼容，未测试 |
| CentOS / RHEL | 8 | ⚠️ 预期兼容，未测试 |

**运行时要求**：
- **架构**：x86_64 (amd64)

### 安装依赖

#### Debian / Ubuntu

```bash
sudo apt update
sudo apt install -y libc++1 libc++abi1
```

#### CentOS / RHEL / Fedora

```bash
sudo dnf install -y libcxx libcxxabi
# 或
sudo yum install -y libcxx libcxxabi
```

#### 其他发行版

请安装以下库的对应包：
- `libc++.so.1` — LLVM C++ 标准库
- `libc++abi.so.1` — LLVM C++ ABI

## 快速启动

```bash
# 添加执行权限
chmod +x xiaozhi_server

# 启动服务
./xiaozhi_server
```

### 后台运行

使用 nohup：

```bash
nohup ./xiaozhi_server > logs/output.log 2>&1 &
```

或使用 systemd（推荐生产环境），见下文。

## 端口与服务

| 端口 | 配置来源 | 说明 |
|------|----------|------|
| **8080** | `manager.json` → `server.port` | **管理后台**：Web 控制台 + HTTP API |
| **8989** | `main_config.yaml` → `websocket.port` | **主服务 WebSocket**：设备/客户端连接 |
| **9000** | `asr_server.json` → `server.port` | **ASR/声纹服务**：语音识别内部接口 |
| **2883** | 控制台配置 | **MQTT 服务**：设备 MQTT 连接 |
| **8990** | 控制台配置 | **UDP 服务**：设备 UDP 通信 |
| **6060** | 控制台配置 | **pprof**：性能分析（默认关闭） |

## 访问地址

### 管理后台

- **本地访问**：`http://localhost:8080/`
- **局域网访问**：`http://<服务器IP>:8080/`

### 设备/客户端连接

- **WebSocket**：`ws://<服务器IP>:8989/`
- **MQTT**：`<服务器IP>:2883`
- **UDP**：`<服务器IP>:8990`

## 修改配置

### 需在配置文件中修改的端口

以下端口修改后需重启服务生效：

| 端口 | 配置文件 | 配置项 |
|------|----------|--------|
| 8080 | `manager.json` | `server.port` |
| 8989 | `main_config.yaml` | `websocket.port` |
| 9000 | `asr_server.json` | `server.port` |

### 控制台配置

以下端口及所有其他配置通过管理后台控制台进行变更：

- **端口配置**：MQTT (2883)、UDP (8990)、pprof (6060)
- **功能配置**：LLM、TTS、ASR、声纹识别等
- 访问 `http://localhost:8080/` 进入管理后台
- 配置变更实时生效，无需重启服务

## 生产环境部署（systemd）

创建服务文件 `/etc/systemd/system/xiaozhi.service`：

```ini
[Unit]
Description=Xiaozhi Server
After=network.target

[Service]
Type=simple
User=YOUR_USER
WorkingDirectory=/path/to/xiaozhi_server-linux-amd64
ExecStart=/path/to/xiaozhi_server-linux-amd64/xiaozhi_server
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
# 重载配置
sudo systemctl daemon-reload

# 启用开机自启
sudo systemctl enable xiaozhi

# 启动服务
sudo systemctl start xiaozhi

# 查看状态
sudo systemctl status xiaozhi

# 查看日志
sudo journalctl -u xiaozhi -f
```

## 防火墙配置

如果服务器启用了防火墙，需要开放相应端口：

```bash
# Ubuntu/Debian (ufw)
sudo ufw allow 8080/tcp  # 管理后台
sudo ufw allow 8989/tcp  # WebSocket
sudo ufw allow 2883/tcp  # MQTT
sudo ufw allow 8990/udp  # UDP

# CentOS/RHEL (firewalld)
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=8989/tcp
sudo firewall-cmd --permanent --add-port=2883/tcp
sudo firewall-cmd --permanent --add-port=8990/udp
sudo firewall-cmd --reload
```

## 常见问题

### 提示缺少共享库

使用 `ldd` 命令检查缺失的库：

```bash
ldd xiaozhi_server
ldd ten-vad/lib/Linux/x64/libten_vad.so
```

根据输出安装对应的系统包。

### glibc 版本过低

如果出现 `version 'GLIBC_2.xx' not found`，说明系统 glibc 版本过旧。建议：
- 升级系统到较新版本
- 或使用 Docker 容器运行

### 端口被占用

```bash
# 查看端口占用
sudo lsof -i :端口号
# 或
sudo netstat -tulpn | grep 端口号

# 修改配置文件中的端口号或结束占用进程
```
