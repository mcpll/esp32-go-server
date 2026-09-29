# MCP Audio Server 独立仓库使用说明

## 概述

MCP Audio Server 已经拆分为独立仓库，推荐直接使用独立项目进行运行、调试和二次开发。

独立项目名称：

- `mcp_audio_server`
- `github.com/hackers365/mcp_audio_server`

核心目标是演示：

- 如何通过 `musicPlayer` 工具返回 `ResourceLink`
- 如何通过 `resource/read` 分页读取音频数据
- 如何用 `BlobResourceContents` 返回 base64 编码的音频片段

这个独立仓库既可以直接运行，也适合作为接入模板。

## 推荐使用方式

推荐在独立仓库中使用 MCP Audio Server。

推荐先获取独立仓库，再进入项目目录：

```bash
git clone https://github.com/hackers365/mcp_audio_server.git
cd mcp_audio_server
```

## 服务暴露的能力

当前服务只暴露两类能力：

1. 工具 `musicPlayer`
2. 资源 `resource://read_from_http`

### `musicPlayer`

- 作用：根据用户输入的歌曲名搜索音乐并返回可播放资源
- 入参：`query`
- 返回：`ResourceLink`

返回的 `ResourceLink` 关键字段含义如下：

- `URI`: `resource://read_from_http`
- `Name`: 实际歌曲名
- `Description`: 实际音频 URL
- `MIMEType`: `audio/mpeg`

### `resource://read_from_http`

- 作用：按分页读取远端音频数据
- 调用方式：通过 `resource/read`
- 参数通过 `Arguments` 传递

请求参数格式：

```json
{
  "url": "实际音频URL",
  "start": 0,
  "end": 102400
}
```

参数说明：

- `url`: 真实音频地址，来自 `ResourceLink.Description`
- `start`: 起始字节偏移
- `end`: 结束字节偏移，不包含该位置

返回内容为 `BlobResourceContents`：

- `MIMEType`: `audio/mpeg`
- `Blob`: base64 编码后的音频二进制数据

当数据读完时，服务端会返回 base64 编码后的 `[DONE]` 作为结束标记。

## 调用流程

完整流程如下：

1. 客户端调用 `musicPlayer`
2. 工具搜索歌曲并返回 `ResourceLink`
3. 客户端对 `resource://read_from_http` 发起 `resource/read`
4. 每次通过 `Arguments` 传 `url`、`start`、`end`
5. Server 返回 base64 编码的 `BlobResourceContents`
6. 客户端解码后按音频流持续播放，直到收到 `[DONE]`

## 运行方式

独立仓库支持两种传输方式：

- 默认：`stdio`
- 可选：HTTP Streamable MCP

### stdio 模式

直接启动：

```bash
git clone https://github.com/hackers365/mcp_audio_server.git
cd mcp_audio_server
go run .
```

### HTTP 模式

显式指定 HTTP 传输：

```bash
cd mcp_audio_server
go run . -t http
```

或：

```bash
cd mcp_audio_server
go run . --transport http
```

HTTP 模式下监听信息为：

- 端口：`3001`
- 路径：`/mcp`
- 完整地址：`http://localhost:3001/mcp`

## 当前使用注意事项

独立仓库可以直接构建和运行，使用前建议注意以下几点：

- 歌曲搜索和真实 URL 获取依赖 `github.com/scroot/music-sd/pkg/netease` 和 `github.com/scroot/music-sd/pkg/qq`
- 音乐搜索结果和可播放链接的稳定性取决于外部站点能力
- 如果把这个独立项目移植到其他项目中，通常需要同步补齐上述依赖和搜索逻辑

如果你的目标是快速接入自己的音频工具，建议优先复用协议和数据流，而不是直接复用歌曲搜索实现。

## 作为模板接入时应保持不变的部分

如果要把这个独立项目改造成你自己的音频 MCP Server，建议保留下面这些协议约定：

- 工具返回 `ResourceLink`
- `resource/read` 使用 `Arguments` 分页读取
- 音频数据通过 `BlobResourceContents.Blob` 返回
- `Blob` 内容保持为 base64 编码
- 音频 MIME 类型与真实数据一致；当前独立仓库为 `audio/mpeg`
- 流结束时返回 `[DONE]`

这样可以与当前主服务里的音频消费逻辑保持兼容。

## 与当前主服务的兼容性

当前主服务对音频类 MCP 工具的消费逻辑已经按以下方式处理：

- 识别 `ResourceLink`
- 使用 `Arguments` 方式分页调用 `resource/read`
- 解码 `BlobResourceContents.Blob`
- 按 MIME 类型解析音频格式
- 持续播放直到读取完成

因此，这个独立项目的协议形态可以继续作为音频类 MCP 工具的参考模板。
