
#### 延迟测试结果

可以做到1-1.3s内回复，如果用更小的模型应该可以更快

asr: funasr
llm: 阿里云api qwen2.5-72b-instruct
tts: cosyvoice 

```
time="2025-05-22 19:33:09.940" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1394 ms" caller="client.go:428"
time="2025-05-22 19:33:33.458" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1237 ms" caller="client.go:428"
time="2025-05-22 19:33:52.596" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1190 ms" caller="client.go:428"
time="2025-05-22 19:34:12.272" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1361 ms" caller="client.go:428"
time="2025-05-22 19:34:31.598" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1347 ms" caller="client.go:428"
time="2025-05-22 19:35:00.281" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1194 ms" caller="client.go:428"
time="2025-05-22 19:35:24.418" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 975 ms" caller="client.go:428"
time="2025-05-22 19:35:49.868" level=debug msg="从接收音频结束 asr->llm->tts首帧 整体 耗时: 1150 ms" caller="client.go:428"
```

---

## 管理后台测试

一键启动包和Docker部署均内置Web管理后台，提供可视化测试界面。

支持以下测试类型：

| 测试类型 | 说明 |
|---------|------|
| VAD | 语音活动检测连通性与响应时间 |
| ASR | 语音识别连通性与首包延迟 |
| LLM | 大模型推理连通性与首包延迟 |
| TTS | 语音合成连通性与首包延迟 |
| OTA | MQTT/UDP 连通性测试 |

详细使用方式请参考：**[管理后台使用指南 →](manager_console_guide.md)**