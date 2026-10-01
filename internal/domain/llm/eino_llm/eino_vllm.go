package eino_llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

func (p *EinoLLMProvider) ResponseWithVllm(ctx context.Context, file []byte, text string, mimeType string) (string, error) {
	log.Infof("[Eino-LLM] starting VLLM request - MIMEType: %s, file length: %d", mimeType, len(file))

	// base64-encode the image file and build a data URL
	base64Str := base64.StdEncoding.EncodeToString(file)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Str)

	msg := &schema.Message{
		Role: schema.User,
		MultiContent: []schema.ChatMessagePart{
			{
				Type: schema.ChatMessagePartTypeText,
				Text: text,
			},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				ImageURL: &schema.ChatMessageImageURL{
					URL: dataURL,
				},
			},
		},
	}

	dialogue := []*schema.Message{
		&schema.Message{
			Role:    schema.System,
			Content: "你是一个专业的图片识别专家，请根据图片内容使用中文回答用户的问题。",
		},
		msg,
	}
	responseChan := p.ResponseWithContext(ctx, "", dialogue, []*schema.ToolInfo{})
	if responseChan == nil {
		log.Errorf("[Eino-VLLM] vision API request failed - responseChan is nil")
		return "", fmt.Errorf("调用视觉api请求处理失败 - responseChan为nil")
	}

	var result bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			log.Errorf("[Eino-VLLM]  context done")
			return "", nil
		case response, ok := <-responseChan:
			if !ok {
				if response != nil && response.Content != "" {
					result.WriteString(response.Content)
				}
				responseText := result.String()
				return responseText, nil
			}
			result.WriteString(response.Content)
		}
	}
}
