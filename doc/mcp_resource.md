# MCP 工具调用返回内容类型文档

## 概述

本文档详细描述了程序支持的工具调用返回内容类型。程序采用**结构化响应系统**，支持多种内容类型的处理和渲染。

## 🔧 核心处理流程

### 工具调用响应处理

工具调用响应的核心处理器负责：

1. **工具调用执行**: 遍历所有工具调用请求
2. **结果解析**: 解析工具返回的结果
3. **内容类型识别**: 根据内容类型进行不同的处理
4. **资源渲染**: 处理音频、文本、资源链接等不同类型的内容

## 📋 支持的内容类型

### 1. 音频内容 (AudioContent)

**类型**: `mcp_go.AudioContent`

**特征**:
- 包含 Base64 编码的音频数据
- 支持多种音频格式 (MIME Type)
- 直接播放，终止后续 LLM 处理

**处理流程**:
```go
if audioContent, ok := content.(mcp_go.AudioContent); ok {
    // 解码 Base64 音频数据
    rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Data)
    // 使用 music_player 播放音频
    audioChan, err := play_music.PlayMusicFromAudioData(ctx, rawAudioData, ...)
    // 发送播放状态消息
    l.serverTransport.SendSentenceStart(playText)
    // 通过 TTS 管理器播放音频
    l.ttsManager.SendTTSAudio(ctx, audioChan, true)
}
```

**使用场景**:
- 音乐播放工具
- 语音合成工具
- 音频文件播放

### 2. 资源链接 (ResourceLink)

**类型**: `mcp_go.ResourceLink`

**特征**:
- 包含资源 URI 和元数据
- 支持分页读取大型资源
- 流式处理，适合大文件
- 使用 Pipe 机制实现实时音频流播放

**处理流程**:
```go
if resourceLink, ok := content.(mcp_go.ResourceLink); ok {
    // 创建 Pipe 用于流式传输
    pipeReader, pipeWriter = io.Pipe()
    
    // 启动分页读取协程
    go func() {
        // 分页读取资源
        resourceResult, err := client.ReadResource(readCtx, mcp_go.ReadResourceRequest{
            Params: mcp_go.ReadResourceParams{
                URI: resourceLink.URI,
                Arguments: map[string]any{
                    "url": resourceLink.Description, 
                    "start": start, 
                    "end": start + page
                },
            },
        })
        
        // 处理 BlobResourceContents
        for _, content := range resourceResult.Contents {
            if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
                // 解码并发送到音频流通道
                rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Blob)
                streamChan <- rawAudioData
            }
        }
    }()
    
    // 使用 music_player 播放音频流
    audioChan, err := play_music.PlayMusicFromPipe(ctx, pipeReader, ...)
}
```

**分页读取参数详解**:

#### 请求参数格式
```go
Arguments: map[string]any{
    "url": resourceLink.Description,  // 实际资源URL
    "start": start,                   // 起始字节位置
    "end": start + page,              // 结束字节位置
}
```

#### 参数说明
- **url**: 实际资源的 URL 地址，来自 `resourceLink.Description`
- **start**: 起始字节位置，从0开始计数
- **end**: 结束字节位置（不包含），即读取范围 [start, end)
- **分页大小**: 由 `McpReadResourcePageSize` 常量定义，默认 100KB

#### 分页读取流程
```go
start := 0
page := McpReadResourcePageSize  // 100 * 1024
totalRead := 0
pageCount := 0

for {
    // 创建带超时的上下文
    readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    
    // 发送分页读取请求
    resourceResult, err := client.ReadResource(readCtx, mcp_go.ReadResourceRequest{
        Params: mcp_go.ReadResourceParams{
            URI: resourceLink.URI,
            Arguments: map[string]any{
                "url": resourceLink.Description, 
                "start": start, 
                "end": start + page
            },
        },
    })
    cancel()
    
    // 处理返回的 BlobResourceContents
    for _, content := range resourceResult.Contents {
        if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
            // 解码Base64数据
            rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Blob)
            
            // 检查是否为结束标志
            if string(rawAudioData) == McpReadResourceStreamDoneFlag {
                return nil // 读取完成
            }
            
            // 发送到音频流通道
            streamChan <- rawAudioData
            totalRead += len(rawAudioData)
        }
    }
    
    // 检查读取完成条件
    if len(rawAudioData) < page || !hasData {
        return nil // 读取完成
    }
    
    // 更新起始位置
    start += page
    pageCount++
}
```

#### 流式处理机制

**Pipe 传输架构**:
```go
// 创建 Pipe 用于音频流传输
pipeReader, pipeWriter = io.Pipe()

// 启动数据写入协程
go func() {
    for {
        select {
        case audioData, ok := <-streamChan:
            if !ok {
                pipeWriter.Close()
                return
            }
            pipeWriter.Write(audioData)
        case <-ctx.Done():
            return
        }
    }
}()

// 使用 music_player 从 Pipe 播放音频
audioChan, err := play_music.PlayMusicFromPipe(ctx, pipeReader, ...)
```

#### 错误处理机制

**超时重试**:
```go
if err != nil {
    // 如果是超时错误，尝试重试
    if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
        log.Warnf("资源读取超时，尝试重试...")
        time.Sleep(1 * time.Second)
        continue
    }
    return fmt.Errorf("读取资源失败: %v", err)
}
```

**上下文取消**:
```go
select {
case <-ctx.Done():
    log.Debugf("资源读取被取消")
    return nil
case streamChan <- rawAudioData:
    // 正常发送数据
}
```

#### 分页机制特性
- **内存优化**: 分页读取避免一次性加载大文件到内存
- **流式处理**: 边读取边播放，支持实时音频流
- **自动结束**: 检测 `McpReadResourceStreamDoneFlag` 标志判断读取完成
- **错误恢复**: 支持超时重试和上下文取消
- **实时播放**: 使用 Pipe 机制实现边读取边播放
- **超时控制**: 每次分页读取都有30秒超时限制

#### 配置参数
- **McpReadResourcePageSize**: 分页大小，默认 100KB (100 * 1024)
- **McpReadResourceStreamDoneFlag**: 流结束标志，为 `"[DONE]"`
- **读取超时**: 每次分页读取的超时时间，默认30秒
- **重试机制**: 超时错误自动重试，间隔1秒

**使用场景**:
- 大型音频文件播放
- 流媒体资源处理
- 网络资源访问
- 实时音频流播放

### 3. 文本内容 (TextContent)

**类型**: `mcp_go.TextContent`

**特征**:
- 纯文本内容
- 累积到响应消息中
- 不终止后续处理

**处理流程**:
```go
if textContent, ok := content.(mcp_go.TextContent); ok {
    mcpContent += textContent.Text
}
```

**使用场景**:
- 查询结果返回
- 状态信息显示
- 错误消息展示

### 4. Blob 资源内容 (BlobResourceContents)

**类型**: `mcp_go.BlobResourceContents`

**特征**:
- 二进制数据内容
- Base64 编码
- 支持流式处理

**处理流程**:
```go
if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
    rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Blob)
    // 检查是否为结束标志
    if string(rawAudioData) == McpReadResourceStreamDoneFlag {
        return nil
    }
    // 发送到音频流通道
    streamChan <- rawAudioData
}
```

## 🏗️ 结构化响应系统

### 响应类型分类

程序支持四种主要的响应类型：

#### 1. 动作类响应 (MCPActionResponse)
- **用途**: 执行特定动作，如播放音乐、退出对话
- **终止性**: 可配置，通常终止后续 LLM 处理
- **控制标志**: `FinalAction`, `NoFurtherResponse`, `SilenceLLM`

#### 2. 音频类响应 (MCPAudioResponse)
- **用途**: 音频资源播放
- **终止性**: 通常终止后续处理
- **特征**: 包含音频数据和播放信息

#### 3. 内容类响应 (MCPContentResponse)
- **用途**: 返回查询数据、状态信息
- **终止性**: 不终止后续处理
- **特征**: 包含数据和显示提示

#### 4. 错误类响应 (MCPErrorResponse)
- **用途**: 统一错误处理
- **终止性**: 不终止后续处理
- **特征**: 包含错误码和建议

### 响应处理接口

```go
type MCPResponse interface {
    GetType() MCPResponseType
    GetSuccess() bool
    IsTerminal() bool // 关键：判断是否终止后续LLM处理
    ToJSON() (string, error)
    GetContent() []mcp_go.Content
}
```

## 🔄 处理流程详解

### 1. 工具调用执行
```go
fcResult, err := tool.InvokableRun(toolCtx, toolCall.Function.Arguments)
```

### 2. 结果解析
```go
// 尝试解析本地工具结果
if mcpResp, ok := l.handleLocalToolResult(fcResult); ok {
    contentList = mcpResp.GetContent()
} else if toolCallResult, ok := l.handleToolResult(fcResult); ok {
    contentList = toolCallResult.Content
}
```

> `handleToolResult` **不再要求工具返回值必须是 JSON**。  
> - 如果返回的是标准 MCP `CallToolResult` JSON，会按结构化内容解析。  
> - 如果返回的是普通字符串，会自动包装成 `TextContent` 继续后续流程。  
> 这样普通文本工具和结构化 MCP 工具都可以被统一处理。

### 3. 内容类型处理
```go
for _, content := range contentList {
    switch content.(type) {
    case mcp_go.AudioContent:
        // 处理音频内容
    case mcp_go.ResourceLink:
        // 处理资源链接
    case mcp_go.TextContent:
        // 处理文本内容
    }
}
```

### 4. 后续处理控制
```go
if invokeToolSuccess && !shouldStopLLMProcessing {
    l.DoLLmRequest(ctx, nil, l.einoTools, true)
}
```

## 📊 内容类型对比表

| 内容类型 | 终止性 | 处理方式 | 使用场景 | 示例工具 |
|----------|--------|----------|----------|----------|
| **AudioContent** | 终止 | 直接播放 | 小音频文件 | play_music |
| **ResourceLink** | 终止 | 分页读取+流式播放 | 大文件/流媒体 | music_player |
| **TextContent** | 不终止 | 累积文本 | 信息查询 | get_datetime |
| **BlobResourceContents** | 终止 | 流式处理 | 音频流数据 | audio_stream |

## 🎯 最佳实践

### 1. 工具实现建议
- **音频工具**: 返回 `AudioContent` 或 `ResourceLink`
- **查询工具**: 返回 `TextContent`
- **动作工具**: 使用结构化响应系统

### 2. 性能优化
- 大文件使用 `ResourceLink` 进行分页处理，支持流式播放
- 小音频文件直接使用 `AudioContent`，减少网络开销
- 文本内容避免过长，影响响应速度
- 使用 Pipe 机制实现边读取边播放，提升用户体验

### 3. 错误处理
- 使用 `MCPErrorResponse` 统一错误格式
- 提供有意义的错误码和建议
- 保持向后兼容性

## 🔧 配置参数

### 分页配置
- `McpReadResourcePageSize`: 资源读取分页大小，默认 100KB (100 * 1024)
- `McpReadResourceStreamDoneFlag`: 流结束标志，为 `"[DONE]"`
- **读取超时**: 每次分页读取的超时时间，默认30秒
- **重试机制**: 超时错误自动重试，间隔1秒

### 音频配置
- `OutputAudioFormat.SampleRate`: 输出音频采样率
- `OutputAudioFormat.FrameDuration`: 输出音频帧时长
- **音频格式**: 根据 `resourceLink.MIMEType` 自动识别

## 📝 扩展指南

### 添加新的内容类型
1. 在 `mcp_go` 包中定义新的内容类型
2. 在 `handleToolCallResponse` 中添加类型处理逻辑
3. 实现相应的处理函数
4. 更新文档和测试

### 自定义响应类型
1. 继承 `MCPResponseBase`
2. 实现 `MCPResponse` 接口
3. 在 `ParseMCPResponse` 中添加解析逻辑
4. 提供便利构造函数

## 🎵 MCP Audio Server 独立仓库

### 概述

MCP Audio Server 已经拆分为独立仓库，推荐通过独立项目运行和调试音频类 MCP Server。当前文档中的这一节主要说明它与主服务的协议兼容方式。

### 核心功能

#### 1. 音乐播放工具
- **工具名称**: `musicPlayer`
- **功能**: 搜索并播放音乐
- **返回**: `ResourceLink` 类型的音频资源链接

#### 2. 音频资源模板
- **URI 格式**: `resource://read_from_http`
- **功能**: 支持分页读取音频数据，通过 Arguments 传递参数
- **参数**: url (实际音乐URL), start (起始位置), end (结束位置)
- **返回**: `BlobResourceContents` 类型的音频数据

### 关键特性

- **分页读取**: 支持大文件的流式处理
- **HTTP Range 请求**: 实现音频数据的分段获取
- **错误处理**: 处理 416 状态码等异常情况
- **超时重试**: 自动重试超时错误，间隔1秒
- **上下文取消**: 支持优雅的资源读取取消
- **Base64 编码**: 安全传递音乐 URL 参数
- **多传输支持**: stdio 和 HTTP 两种传输方式
- **实时播放**: 使用 Pipe 机制实现边读取边播放

### 使用方式

```bash
# 获取并进入独立仓库
git clone https://github.com/hackers365/mcp_audio_server.git
cd mcp_audio_server

# 启动服务器
go run .

# 工具调用
{
  "name": "musicPlayer",
  "arguments": {"query": "周杰伦"}
}
```

这个独立项目展示了如何构建支持音频资源处理的 MCP 工具，可作为开发其他音频相关工具的参考模板。更完整的使用说明可参考 `doc/mcp_audio_example.md`。

---

*本文档反映了程序当前支持的所有工具调用返回内容类型。* 
