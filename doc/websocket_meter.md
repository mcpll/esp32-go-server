### 压测

```
root@hackers365-System-Product-Name:~# docker run -itd --name websocket_meter docker.jsdelivr.fyi/hackers365/xiaozhi_websocket_client                      
87311584e5fef592f32e0b7d7062d9053e956d5e0d50edb220370ff37d2293ac
root@hackers365-System-Product-Name:~# 
root@hackers365-System-Product-Name:~# docker exec -it websocket_meter /bin/bash                                                      
root@87311584e5fe:/workspace# 
root@87311584e5fe:/workspace# ./ws_multi  -h
Usage of ./ws_multi:
  -count int
        客户端数量 (default 10)
  -device string
        设备ID
  -server string
        服务器地址 (default "ws://localhost:8989/xiaozhi/v1/")
  -text string
        聊天内容, 多句以逗号分隔会依次发送 (default "你好")
root@87311584e5fe:/workspace# ./ws_multi -count 1 -server wss://joeyzhou.chat/ws/xiaozhi/v1/ -text "你好,在干什么,一起出去玩吧" 
运行小智客户端
服务器: wss://joeyzhou.chat/ws/xiaozhi/v1/
客户端数量: 1
发送内容: 你好,在干什么,一起出去玩吧
2025-05-27 09:54:51.095 [info] [audio_utils.go:199] tts云端首帧耗时: 532 ms
2025-05-27 09:54:51.098 [info] [audio_utils.go:269] tts云端->首帧解码完成耗时: 535 ms
2025-05-27 09:54:51.401 [info] [cosyvoice.go:306] tts耗时: 从输入至获取MP3数据结束耗时: 838 ms
2025-05-27 09:54:51.748 [info] [audio_utils.go:199] tts云端首帧耗时: 344 ms
2025-05-27 09:54:51.752 [info] [audio_utils.go:269] tts云端->首帧解码完成耗时: 347 ms
2025-05-27 09:54:51.901 [info] [cosyvoice.go:306] tts耗时: 从输入至获取MP3数据结束耗时: 497 ms
2025-05-27 09:54:52.292 [info] [audio_utils.go:199] tts云端首帧耗时: 387 ms
2025-05-27 09:54:52.296 [info] [audio_utils.go:269] tts云端->首帧解码完成耗时: 391 ms
2025-05-27 09:54:52.628 [info] [cosyvoice.go:306] tts耗时: 从输入至获取MP3数据结束耗时: 723 ms
0 客户端开始运行
0 客户端已连接到服务器: wss://joeyzhou.chat/ws/xiaozhi/v1/
收到消息: {Type:hello Text: State: SessionID:cafd2800-1979-06d5-19cf-b8bf53bb55dc Transport:websocket AudioFormat:<nil>}
发送Opus帧: 20
发送Opus帧: 50
发送Opus帧: 59
```

#### 整体说明
    1. 程序会根据用户输入的文本, 调用tts接口生成音频数据，依次发送给服务器
    2. 耗时统计从 type: listen, state: stop开始进行计时，直到收到服务器第一帧音频数据停止

#### 参数说明：
    -count: 并发数量
    -device: 默认会随机生成deviceId，如果使用此参数来指定设备，-count必须为1
    -server: websocket服务器地址
    -text: 要发送的内容, 以“,”号分隔，循环发送

#### 输出说明
    可以将输出重定向至日志文件, 然后tail -f xx.log | grep '平均响应时间'