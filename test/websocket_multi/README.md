# websocket_multi 压测客户端说明

## 新增优化参数

- `-audio_wav`：使用本地 wav 文件（逗号分隔）作为音频输入，避免调用云端 TTS 生成测试音频。
- `-metrics_jsonl`：输出结构化延迟指标（JSONL），包含 `first_frame`、`tts_stop` 事件。
- `-ramp_ms`：客户端启动间隔毫秒，减少瞬时建连抖动。

## 示例

```bash
go run ./test/websocket_multi/xiaozhi_ws_client_multi.go \
  -server ws://127.0.0.1:8989/xiaozhi/v1/ \
  -count 100 \
  -audio_wav ./test/websocket_client/test.wav \
  -ramp_ms 20 \
  -metrics_jsonl ./ws_metrics.jsonl
```

如果不传 `-audio_wav`，会沿用原行为（通过 cosyvoice 生成音频）。

---

## 并发阶梯压测模板（推荐）

> 先确保你已经启动：
> 1) 主服务；
> 2) 独立 mock 服务（如 `go run ./cmd/mock_ai_server -addr :18080`）；
> 3) 主服务配置已指向 mock ASR/LLM/TTS。

### 1) 50 并发

```bash
go run ./test/websocket_multi/xiaozhi_ws_client_multi.go \
  -server ws://127.0.0.1:8989/xiaozhi/v1/ \
  -count 50 \
  -audio_wav ./test/websocket_client/test.wav \
  -ramp_ms 30 \
  -metrics_jsonl ./metrics_50.jsonl
```

### 2) 100 并发

```bash
go run ./test/websocket_multi/xiaozhi_ws_client_multi.go \
  -server ws://127.0.0.1:8989/xiaozhi/v1/ \
  -count 100 \
  -audio_wav ./test/websocket_client/test.wav \
  -ramp_ms 20 \
  -metrics_jsonl ./metrics_100.jsonl
```

### 3) 300 并发

```bash
go run ./test/websocket_multi/xiaozhi_ws_client_multi.go \
  -server ws://127.0.0.1:8989/xiaozhi/v1/ \
  -count 300 \
  -audio_wav ./test/websocket_client/test.wav \
  -ramp_ms 10 \
  -metrics_jsonl ./metrics_300.jsonl
```

### 4) 500 并发

```bash
go run ./test/websocket_multi/xiaozhi_ws_client_multi.go \
  -server ws://127.0.0.1:8989/xiaozhi/v1/ \
  -count 500 \
  -audio_wav ./test/websocket_client/test.wav \
  -ramp_ms 5 \
  -metrics_jsonl ./metrics_500.jsonl
```

---

## 指标汇总脚本

新增：`test/websocket_multi/summarize_metrics.py`

```bash
python3 ./test/websocket_multi/summarize_metrics.py ./metrics_100.jsonl
```

输出包含：
- `first_frame`：avg/p50/p95/p99/max
- `tts_stop`：avg/p50/p95/p99/max
- `approx_success_rate(first_frame/tts_stop)`

