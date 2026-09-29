# 独立 Mock ASR/LLM/TTS 服务（不改主程序）

本方案提供一个**独立运行**的 mock 服务进程，用于在压测时替代真实 ASR/LLM/TTS 云服务。

## 1. 启动

```bash
go run ./cmd/mock_ai_server \
  -addr :18080 \
  -asr-text "你好，这是压测mock识别结果" \
  -llm-reply "这是mock llm回复" \
  -tts-mode silence
```

健康检查：

```bash
curl http://127.0.0.1:18080/healthz
```

## 2. 暴露接口

- `ws://127.0.0.1:18080/asr/`
  - 兼容 FunASR 风格 ws 输入（接收音频二进制帧）
  - 收到 `{"is_speaking": false}` 后返回最终识别结果

- `POST http://127.0.0.1:18080/v1/chat/completions`
  - OpenAI Chat Completions 兼容接口
  - 支持 `stream=false/true`

- `POST http://127.0.0.1:18080/v1/audio/speech`
  - OpenAI TTS 兼容接口
  - 返回 `audio/wav`（静音或beep）

## 3. 主程序配置建议（仅改配置，不改代码）

### ASR（FunASR）

- `host=127.0.0.1`
- `port=18080`
- 协议路径按当前实现使用 `ws://host:port/`，若你的配置层要求路径，请使用 `/asr/`。

> 如果你当前 ASR 适配器强依赖 `ws://host:port/` 根路径，也可以在网关层把 `/` 转发到 `/asr/`。

### LLM（OpenAI 兼容）

- provider 选择 `eino`（`type=openai`）
- `base_url=http://127.0.0.1:18080/v1`
- `api_key` 任意非空值
- `model_name` 任意值（例如 `mock-gpt`）

### TTS（OpenAI 兼容）

- provider 选择 `openai`
- `api_url=http://127.0.0.1:18080/v1/audio/speech`
- `response_format=wav`
- `api_key` 任意非空值

## 4. 可调参数

```bash
-asr-delay-ms         # ASR最终返回延迟
-llm-first-delay-ms   # LLM首token延迟
-llm-chunk-delay-ms   # LLM流式chunk间延迟
-tts-first-delay-ms   # TTS首包延迟
-tts-mode             # silence|beep
-tts-duration-ms      # 返回音频时长
```

## 5. 压测建议

1. 先本地单连接验通（确保设备能走完整链路并收到音频）。
2. 再用 `ws_multi` 做并发阶梯（如 50/100/200/500）。
3. 用不同 delay 组合模拟真实外部依赖波动，观测 P95/P99 与错误率。


## 6. ws_multi 是否需要更改优化（评估）

结论：**建议做小幅优化，非必须重构**。当前可直接用于压测，但为了更真实衡量“主服务性能”而不是“压测客户端瓶颈”，建议补以下能力：

1. **增加纯音频回放模式（推荐优先）**
   - 现在常见做法是先本地TTS再推音频，这会把客户端TTS耗时混进结果。
   - 建议加 `-audio_file`/`-audio_dir`，直接发送预编码opus或wav转opus后的帧。

2. **延迟统计结构化输出**
   - 增加首帧RT、全链路完成RT、错误码分类统计。
   - 建议输出 JSONL，便于后处理聚合 P95/P99。

3. **连接与发送节流控制**
   - 增加分批建连（例如每秒启动N个客户端），避免瞬时建连放大客户端侧抖动。
   - 增加发包抖动参数，模拟真实设备网络。

4. **失败重试与超时策略可配置**
   - 如 `-dial_timeout`、`-read_timeout`、`-retry`，提升长压测稳定性。

5. **资源指标采集（可选）**
   - 记录客户端自身CPU/内存，便于区分“服务端瓶颈”与“压测机瓶颈”。

在你这个“独立mock服务”方案下，`ws_multi` **不改也能跑**，但建议至少做第1和第2项，压测结论会明显更可信。
