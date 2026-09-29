package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	mcp_manager "xiaozhi-esp32-server-golang/internal/domain/mcp"
	log "xiaozhi-esp32-server-golang/logger"

	//"github.com/scroot/music-sd/pkg/netease"
	//"github.com/scroot/music-sd/pkg/qq"
	"github.com/spf13/viper"
)

type LocalMcpTool struct {
	Name        string
	Description string
	Params      any
	Handle      mcp_manager.LocalToolHandler
}

// InitChatLocalMCPTools initializes chat-related local MCP tools
func InitChatLocalMCPTools() {
	manager := mcp_manager.GetLocalMCPManager()

	log.Info("Initializing chat-related local MCP tools...")

	localTools := map[string]LocalMcpTool{
		/*"get_current_datetime": {
			Name:        "get_current_datetime",
			Description: "Get current date and time information",
			Params:      struct{}{},
			Handle:      getCurrentDateTimeHandler,
		},*/
		"exit_conversation": {
			Name:        "exit_conversation",
			Description: "当用户明确表示要结束对话、退出系统或告别时使用，用于优雅地关闭当前聊天会话",
			Params:      struct{}{},
			Handle:      exitConversationHandler,
		},
		"clear_conversation_history": {
			Name:        "clear_conversation_history",
			Description: "当用户要求清空、清除或重置历史对话记录时使用，用于清空当前会话的所有历史对话内容",
			Params:      struct{}{},
			Handle:      clearConversationHistoryHandler,
		},
		"switch_device_role": {
			Name:        "switch_device_role",
			Description: "当用户要求把当前设备切换到某个角色时使用，参数 role_name 支持模糊匹配（会在全局角色和该设备所属用户角色中匹配）",
			Params:      SwitchDeviceRoleParams{},
			Handle:      switchDeviceRoleHandler,
		},
		"restore_device_default_role": {
			Name:        "restore_device_default_role",
			Description: "当用户要求恢复设备默认角色、取消当前设备角色覆盖时使用",
			Params:      struct{}{},
			Handle:      restoreDeviceDefaultRoleHandler,
		},
		"search_knowledge": {
			Name:        "search_knowledge",
			Description: "当用户问题需要事实依据、流程规则、参数细节、文档条款时，检索当前智能体关联知识库并返回相关片段；可选传 knowledge_base_ids 仅查指定知识库；闲聊或纯创作场景不要调用",
			Params:      SearchKnowledgeParams{},
			Handle:      searchKnowledgeHandler,
		},
		/*"play_music": {
			Name:        "play_music",
			Description: "Use when the user wants music or to unwind; play a named track; suggest a concrete song if they want anything; prefer this tool when multiple music tools exist; **slow — send a friendly transition first**",
			Params:      PlayMusicParams{},
			Handle:      playMusicHandler,
		},*/
	}

	for toolName, localTool := range localTools {
		// Skip only when config is explicitly false; missing or true enables
		if viper.IsSet("local_mcp."+toolName) && !viper.GetBool("local_mcp."+toolName) {
			continue
		}
		err := manager.RegisterToolFunc(
			localTool.Name,
			localTool.Description,
			localTool.Params,
			localTool.Handle,
		)
		if err != nil {
			log.Errorf("Failed to register local MCP tool %s: %+v", toolName, err)
		}
	}

	log.Info("Chat-related local MCP tools init done")
}

func RegisterLocalMcpFunc(name string, description string, params any, handle mcp_manager.LocalToolHandler) error {
	manager := mcp_manager.GetLocalMCPManager()

	err := manager.RegisterToolFunc(
		name,
		description,
		params,
		handle,
	)
	if err != nil {
		log.Errorf("Failed to register local MCP tool %s: %+v", name, err)
		return err
	}
	return nil
}

type SwitchDeviceRoleParams struct {
	RoleName string `json:"role_name" description:"目标角色名称，支持模糊匹配" required:"true"`
}

type SearchKnowledgeParams struct {
	Query            string `json:"query" description:"要检索的查询内容" required:"true"`
	TopK             int    `json:"top_k,omitempty" description:"返回条数，默认5"`
	KnowledgeBaseIDs []uint `json:"knowledge_base_ids,omitempty" description:"可选：仅在这些知识库ID内检索（当前智能体已关联）"`
}

// playMusicHandler music playback handler
func playMusicHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing play-music tool")

	// Parse arguments
	var params PlayMusicParams

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
			response := NewErrorResponse("play_music", "参数解析失败", "PARSE_ERROR", "请检查参数格式是否正确")
			return response.ToJSON()
		}
	}

	log.Infof("Found ChatSessionOperator, calling LocalMcpPlayMusic to play music: %s", params.Name)
	audioData, realMusicName, err := GetMusicAudioData(ctx, &params)
	if err != nil {
		log.Errorf("Failed to get music data: %v", err)
		response := NewErrorResponse("play_music", fmt.Sprintf("获取音乐数据失败: %v", err), "PLAYBACK_ERROR", "请检查音乐名称或网络连接")
		return response.ToJSON()
	} else {
		// Play succeeded — action response; stop further processing
		response := NewAudioResponse("play_music", "play_music", fmt.Sprintf("开始播放音乐: %s", realMusicName), true, audioData)
		response.MusicName = realMusicName
		return response.ToJSON()
	}

}

/*
// getCurrentDateTimeHandler current date/time handler
func getCurrentDateTimeHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing get-current-datetime tool")

	// Parse arguments
	var params map[string]interface{}
	timezone := "Local" // Default timezone

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if tz, ok := params["timezone"].(string); ok && tz != "" {
				timezone = tz
			}
		}
	}

	now := time.Now()

	// Try loading the given timezone
	if timezone != "Local" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			now = now.In(loc)
		} else {
			log.Warnf("Failed to load timezone %s, using local timezone", timezone)
		}
	}

	// Build response data
	data := map[string]interface{}{
		"datetime": map[string]interface{}{
			"formatted":     now.Format("2006-01-02 15:04:05"),
			"iso8601":       now.Format(time.RFC3339),
			"chinese":       formatChineseDateTime(now),
			"unix":          now.Unix(),
			"year":          now.Year(),
			"month":         int(now.Month()),
			"day":           now.Day(),
			"hour":          now.Hour(),
			"minute":        now.Minute(),
			"second":        now.Second(),
			"weekday":       now.Weekday().String(),
			"weekday_zh":    getWeekdayChinese(now.Weekday()),
			"week_number":   getWeekNumber(now),
			"timezone":      timezone,
			"timezone_name": now.Location().String(),
		},
	}

	// Create content response
	response := NewContentResponse("get_current_datetime", data, fmt.Sprintf("Current time: %s", formatChineseDateTime(now)))
	// response.Format = "datetime"
	// response.DisplayHint = "Can be used to display current date/time"

	log.Infof("Got current datetime successfully: %s", now.Format("2006-01-02 15:04:05"))
	return response.ToJSON(),nil
}
*/
// exitConversationHandler exit-conversation handler
func exitConversationHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing exit-dialog tool")

	// Parse arguments
	var params map[string]interface{}
	reason := "用户主动退出" // Default reason

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if r, ok := params["reason"].(string); ok && r != "" {
				reason = r
			}
		}
	}

	// Create action response — terminating operation
	response := NewActionResponse("exit_conversation", "exit_conversation", "对话即将结束，感谢您的使用！", "exiting", true)
	response.UserState = "conversation_ended"
	response.Instruction = "对话已结束，请不要生成额外的文本回复"
	response.Metadata = map[string]string{
		"reason":           reason,
		"exit_code":        "0",
		"farewell_chinese": "再见！期待下次与您交流。",
		"farewell_english": "Goodbye! Looking forward to our next conversation.",
	}

	log.Infof("Exit dialog handling done, reason: %s", reason)

	// Get ChatSessionOperator from context and call Close
	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			log.Info("Found ChatSessionOperator, calling Close to shut down session")
			defer chatSessionOperator.LocalMcpCloseChat()
		} else {
			log.Warn("chat_session_operator from context is not ChatSessionOperator type")
		}
	} else {
		log.Warn("chat_session_operator not found in context")
	}

	responseStr, err := response.ToJSON()
	if err != nil {
		return "", err
	}

	return responseStr, nil
}

// clearConversationHistoryHandler clear-history handler
func clearConversationHistoryHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing clear-history tool")

	// Parse arguments
	var params map[string]interface{}
	reason := "用户主动清空历史" // Default reason

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if r, ok := params["reason"].(string); ok && r != "" {
				reason = r
			}
		}
	}

	// Get ChatSessionOperator from context and call LocalMcpClearHistory
	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			log.Info("Found ChatSessionOperator, calling LocalMcpClearHistory to clear history")
			if err := chatSessionOperator.LocalMcpClearHistory(); err != nil {
				log.Errorf("Failed to clear dialog history: %v", err)
				return "", err
			} else {
				// Cleared successfully — action response; do not end conversation
				response := NewActionResponse("clear_conversation_history", "clear_history", "历史对话已成功清空，您可以开始全新的对话。", "completed", false)
				response.Metadata = map[string]string{
					"reason": reason,
					"status": "cleared",
				}
				log.Info("Dialog history cleared successfully")

				return response.ToJSON()
			}
		} else {
			log.Warn("chat_session_operator from context is not ChatSessionOperator type")
			return "", fmt.Errorf("从context中获取的chat_session_operator不是ChatSessionOperator类型")
		}
	}
	log.Warn("chat_session_operator not found in context")
	return "", fmt.Errorf("从context中未找到chat_session_operator")
}

// switchDeviceRoleHandler switch-device-role handler
func switchDeviceRoleHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing switch-device-role tool")

	var params SwitchDeviceRoleParams
	if argumentsInJSON == "" {
		response := NewErrorResponse("switch_device_role", "缺少参数 role_name", "MISSING_ROLE_NAME", "请提供要切换的角色名称")
		return response.ToJSON()
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		response := NewErrorResponse("switch_device_role", "参数解析失败", "PARSE_ERROR", "请检查 role_name 参数格式")
		return response.ToJSON()
	}
	params.RoleName = strings.TrimSpace(params.RoleName)
	if params.RoleName == "" {
		response := NewErrorResponse("switch_device_role", "角色名称不能为空", "INVALID_ROLE_NAME", "请提供有效的 role_name")
		return response.ToJSON()
	}

	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			matchedRoleName, err := chatSessionOperator.LocalMcpSwitchDeviceRole(ctx, params.RoleName)
			if err != nil {
				log.Errorf("Failed to switch device role: %v", err)
				response := NewErrorResponse("switch_device_role", fmt.Sprintf("切换角色失败: %v", err), "SWITCH_ROLE_FAILED", "请尝试更换角色名称或稍后重试")
				return response.ToJSON()
			}

			response := NewActionResponse(
				"switch_device_role",
				"switch_device_role",
				fmt.Sprintf("已切换到角色：%s", matchedRoleName),
				"completed",
				false,
			)
			response.Metadata = map[string]string{
				"requested_role_name": params.RoleName,
				"matched_role_name":   matchedRoleName,
			}
			return response.ToJSON()
		}
		return "", fmt.Errorf("从context中获取的chat_session_operator不是ChatSessionOperator类型")
	}

	return "", fmt.Errorf("从context中未找到chat_session_operator")
}

// restoreDeviceDefaultRoleHandler restore-default-role handler
func restoreDeviceDefaultRoleHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing restore-device-default-role tool")

	if chatSessionOperatorValue := ctx.Value("chat_session_operator"); chatSessionOperatorValue != nil {
		if chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator); ok {
			if err := chatSessionOperator.LocalMcpRestoreDeviceDefaultRole(ctx); err != nil {
				log.Errorf("Failed to restore device default role: %v", err)
				response := NewErrorResponse("restore_device_default_role", fmt.Sprintf("恢复默认角色失败: %v", err), "RESTORE_ROLE_FAILED", "请稍后重试")
				return response.ToJSON()
			}

			response := NewActionResponse(
				"restore_device_default_role",
				"restore_device_default_role",
				"已恢复设备默认角色",
				"completed",
				false,
			)
			return response.ToJSON()
		}
		return "", fmt.Errorf("从context中获取的chat_session_operator不是ChatSessionOperator类型")
	}

	return "", fmt.Errorf("从context中未找到chat_session_operator")
}

func searchKnowledgeHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing knowledge-base search tool")

	var params SearchKnowledgeParams
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
			response := NewErrorResponse("search_knowledge", "参数解析失败", "PARSE_ERROR", "请检查 query 参数格式")
			return response.ToJSON()
		}
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		response := NewErrorResponse("search_knowledge", "query 不能为空", "INVALID_QUERY", "请提供要检索的内容")
		return response.ToJSON()
	}
	if params.TopK <= 0 {
		params.TopK = 5
	}

	chatSessionOperatorValue := ctx.Value("chat_session_operator")
	if chatSessionOperatorValue == nil {
		return "", fmt.Errorf("从context中未找到chat_session_operator")
	}
	chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator)
	if !ok {
		return "", fmt.Errorf("从context中获取的chat_session_operator不是ChatSessionOperator类型")
	}

	hits, err := chatSessionOperator.LocalMcpSearchKnowledge(ctx, params.Query, params.TopK, params.KnowledgeBaseIDs)
	if err != nil {
		response := NewErrorResponse("search_knowledge", fmt.Sprintf("信息检索失败: %v", err), "SEARCH_FAILED", "请稍后重试")
		return response.ToJSON()
	}

	data := map[string]interface{}{
		"query": params.Query,
		"hits":  hits,
		"count": len(hits),
	}
	if len(hits) == 0 {
		response := NewContentResponse("search_knowledge", data, "未找到足够相关信息")
		return response.ToJSON()
	}

	var builder strings.Builder
	for i, hit := range hits {
		content := strings.TrimSpace(hit.Content)
		if content == "" {
			continue
		}
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, content))
	}
	msg := strings.TrimSpace(builder.String())
	if msg == "" {
		msg = "已获取相关信息"
	}
	response := NewContentResponse("search_knowledge", data, msg)
	return response.ToJSON()
}

// getWeekNumber returns week number
func getWeekNumber(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

// formatChineseDateTime formats Chinese date/time
func formatChineseDateTime(t time.Time) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "星期日",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}

	return fmt.Sprintf("%d年%d月%d日 %s %02d:%02d:%02d",
		t.Year(), int(t.Month()), t.Day(),
		weekdays[t.Weekday()],
		t.Hour(), t.Minute(), t.Second(),
	)
}

// getWeekdayChinese returns Chinese weekday name
func getWeekdayChinese(weekday time.Weekday) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "星期日",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}
	return weekdays[weekday]
}

// RegisterChatMCPTools public helper to register chat MCP tools
func RegisterChatMCPTools() {
	InitChatLocalMCPTools()
}

// Play music
func GetMusicAudioData(ctx context.Context, musicParams *PlayMusicParams) ([]byte, string, error) {
	musicName := musicParams.Name
	//welcome := musicParams.Welcome
	welcome := ""
	log.Infof("Searching music: %s, welcome: %s", musicName, welcome)
	// Resolve music URL from the name here
	// Simplified: assume musicName is a URL or comes from config
	musicURL, realMusicName, ierr := getMusicURL(musicName)
	if ierr != nil {
		log.Errorf("Failed to get music URL: %v", ierr)
		return nil, "", fmt.Errorf("获取音乐URL失败: %v", ierr)
	}

	log.Infof("Music search succeeded URL: %s, title: %s", musicURL, realMusicName)

	client := getHTTPClient()
	req, err := http.NewRequest("GET", musicURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("创建请求失败: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("API请求失败: %v", err)
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("读取响应失败: %v", err)
	}

	log.Infof("Fetched music %s data successfully, audio length: %d", realMusicName, len(audioData))

	return audioData, realMusicName, nil
}

/*
func GetMusicAudioData(ctx context.Context, musicParams *PlayMusicParams) ([]byte, string, error) {
	musicName := musicParams.Name
	//welcome := musicParams.Welcome
	welcome := ""
	log.Infof("Searching music: %s, welcome: %s", musicName, welcome)
	// Resolve music URL from the name here
	// Simplified: assume musicName is a URL or comes from config
	musicList := netease.Search(musicName)
	musicList = append(musicList, qq.Search(musicName)...)
	for id, music := range musicList {
		log.Infof("[%2d] %7s | %s %5sMB - %s - %s - %s\n", id, music.Source, music.Duration, music.Size, music.Title, music.Singer, music.Album)
	}

	if len(musicList) <= 0 {
		return nil, "", fmt.Errorf("No music found")
	}
	m := musicList[0]
	m.ParseMusic()
	rc, err := m.ReadCloser()
	if err != nil {
		return nil, "", fmt.Errorf("Failed to get music data: %v", err)
	}
	defer rc.Close()

	audioData, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", fmt.Errorf("Failed to read response: %v", err)
	}

	log.Infof("Fetched music %s data successfully, audio length: %d", m.Name, len(audioData))

	return audioData, m.Name, nil

}
*/
