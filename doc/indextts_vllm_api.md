# IndexTTS vLLM 接口对接说明

本文档用于说明本项目接入 `indextts_vllm` 时对服务端接口的要求，适用于：

- 主程序 TTS 推理（`/audio/speech`）
- 管理员界面拉取音色（`/audio/voices`）
- 用户声音复刻（`/audio/clone`，用于本项目复刻流程）

## 1. 快速兼容清单

你的 IndexTTS 服务至少需要满足以下三点：

- 提供 `POST /audio/speech`，入参兼容 OpenAI TTS 风格：`input`、`voice`、`model`
- 提供 `GET /audio/voices`，返回可枚举音色列表（JSON 对象）
- 若使用本项目“声音复刻”能力，提供 `POST /audio/clone`（`multipart/form-data`）

推荐返回音频格式：`audio/wav`（16-bit PCM）。

## 2. 配置项映射（管理员 -> TTS配置 -> IndexTTS(vLLM)）

| 管理端字段 | 用途 | 发送位置 |
| --- | --- | --- |
| `api_url` | IndexTTS 服务地址 | 作为基础 URL，拼接端点 |
| `api_key` | 可选鉴权 | `Authorization: Bearer <api_key>` |
| `model` | 模型名 | `/audio/speech` 请求体 `model` |
| `voice` | 默认音色 | `/audio/speech` 请求体 `voice` |
| `frame_duration` | 帧时长（ms） | 本地音频切帧参数 |

说明：

- 管理员界面在点击“音色”下拉时，会使用当前输入框里的最新 `api_url` 拉取 `/audio/voices`。
- `api_url` 支持填写基础地址（如 `http://127.0.0.1:7860`），也兼容填写到具体路径（如 `/audio/speech`）。

## 3. 接口要求

### 3.1 `GET /audio/voices`

用途：管理员配置页“音色”下拉、用户侧音色选项。

请求头：

- `Accept: application/json`
- `Authorization: Bearer <api_key>`（可选）

返回示例（推荐）：

```json
{
  "demo_speaker": ["assets/speaker/demo.wav"],
  "narrator_cn_female": ["assets/speaker/narrator_cn_female.wav"]
}
```

要求：

- 返回类型建议为 JSON 对象（键名会被当作音色 ID）。
- 本项目会过滤掉前缀为 `indextts_vllm` 的系统音色，再追加用户复刻音色。

### 3.2 `POST /audio/speech`

用途：主程序 TTS 合成、复刻后试听。

请求头：

- `Content-Type: application/json`
- `Accept: audio/wav,application/octet-stream,*/*`
- `Authorization: Bearer <api_key>`（可选）

请求体示例：

```json
{
  "model": "indextts-vllm",
  "input": "你好，欢迎使用 IndexTTS。",
  "voice": "demo_speaker"
}
```

返回：

- 成功：二进制音频流（建议 `audio/wav`）
- 失败：HTTP 4xx/5xx，并返回可读错误信息

### 3.3 `POST /audio/clone`（本项目复刻功能需要）

用途：`/user/voice-clones` 提交复刻任务时调用。

请求类型：`multipart/form-data`

表单字段：

- `voice`：期望生成的音色 ID
- `audio`：参考音频文件（wav/mp3/m4a 等）

返回示例：

```json
{
  "voice": "demo_speaker_clone_001",
  "ok": true
}
```

要求：

- 建议响应中包含 `voice` 字段；若缺失，本项目会回退使用请求中的 `voice` 字段值。

## 4. 兼容参考（api_server.py）

可参考以下实现风格：

- `POST /audio/speech`：读取 `input`、`voice`、`model`
- `GET /audio/voices`：返回可用音色字典

参考链接：

- https://github.com/hackers365/index-tts-vllm/blob/master/api_server.py

## 5. 常见问题排查

### 5.1 管理端点击音色下拉报错

优先检查：

- `api_url` 是否可达（最新输入值）
- `/audio/voices` 是否返回 JSON 对象
- 是否需要 `api_key`

### 5.2 合成成功但播放异常

优先检查：

- 服务端是否返回标准 WAV（PCM16、采样率正确）
- 中间链路是否有转码或截断
- 响应头 `Content-Type` 是否正确

### 5.3 复刻任务失败

优先检查：

- `/audio/clone` 是否接受 `voice + audio` 的 multipart 请求
- 响应 JSON 是否可解析、是否包含可用 `voice`
