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
			Description: "Use when the user clearly wants to end the conversation, leave, or say goodbye; closes the current chat session gracefully",
			Params:      struct{}{},
			Handle:      exitConversationHandler,
		},
		"clear_conversation_history": {
			Name:        "clear_conversation_history",
			Description: "Use when the user asks to clear, erase or reset the conversation history; clears all history of the current session",
			Params:      struct{}{},
			Handle:      clearConversationHistoryHandler,
		},
		"search_knowledge": {
			Name:        "search_knowledge",
			Description: "Use when the question needs facts, procedure rules, parameter details or document clauses: searches the knowledge bases linked to the current agent and returns the relevant passages; pass knowledge_base_ids to search only some of them; do not call for small talk or purely creative requests",
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

type SearchKnowledgeParams struct {
	Query            string `json:"query" description:"Text to search for" required:"true"`
	TopK             int    `json:"top_k,omitempty" description:"Number of results, default 5"`
	KnowledgeBaseIDs []uint `json:"knowledge_base_ids,omitempty" description:"Optional: search only these knowledge base ids (they must be linked to the current agent)"`
}

// playMusicHandler music playback handler
func playMusicHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing play-music tool")

	// Parse arguments
	var params PlayMusicParams

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
			response := NewErrorResponse("play_music", "failed to parse arguments", "PARSE_ERROR", "check that the arguments are well formed")
			return response.ToJSON()
		}
	}

	log.Infof("Found ChatSessionOperator, calling LocalMcpPlayMusic to play music: %s", params.Name)
	audioData, realMusicName, err := GetMusicAudioData(ctx, &params)
	if err != nil {
		log.Errorf("Failed to get music data: %v", err)
		response := NewErrorResponse("play_music", fmt.Sprintf("failed to get music data: %v", err), "PLAYBACK_ERROR", "check the music name or the network connection")
		return response.ToJSON()
	} else {
		// Play succeeded — action response; stop further processing
		response := NewAudioResponse("play_music", "play_music", fmt.Sprintf("Playing music: %s", realMusicName), true, audioData)
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
			"local":         formatLocalDateTime(now),
			"unix":          now.Unix(),
			"year":          now.Year(),
			"month":         int(now.Month()),
			"day":           now.Day(),
			"hour":          now.Hour(),
			"minute":        now.Minute(),
			"second":        now.Second(),
			"weekday":       now.Weekday().String(),
			"weekday_local": getWeekdayName(now.Weekday()),
			"week_number":   getWeekNumber(now),
			"timezone":      timezone,
			"timezone_name": now.Location().String(),
		},
	}

	// Create content response
	response := NewContentResponse("get_current_datetime", data, fmt.Sprintf("Current time: %s", formatLocalDateTime(now)))
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
	reason := "user requested exit" // Default reason

	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err == nil {
			if r, ok := params["reason"].(string); ok && r != "" {
				reason = r
			}
		}
	}

	// Create action response — terminating operation
	response := NewActionResponse("exit_conversation", "exit_conversation", "The conversation is about to end. Thank you!", "exiting", true)
	response.UserState = "conversation_ended"
	response.Instruction = "The conversation has ended, do not produce any extra text reply"
	response.Metadata = map[string]string{
		"reason":           reason,
		"exit_code":        "0",
		"farewell_italian": "Arrivederci! A presto.",
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
	reason := "user cleared the history" // Default reason

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
				response := NewActionResponse("clear_conversation_history", "clear_history", "The conversation history has been cleared. You can start a fresh conversation.", "completed", false)
				response.Metadata = map[string]string{
					"reason": reason,
					"status": "cleared",
				}
				log.Info("Dialog history cleared successfully")

				return response.ToJSON()
			}
		} else {
			log.Warn("chat_session_operator from context is not ChatSessionOperator type")
			return "", fmt.Errorf("chat_session_operator from context is not a ChatSessionOperator")
		}
	}
	log.Warn("chat_session_operator not found in context")
	return "", fmt.Errorf("chat_session_operator not found in context")
}

func searchKnowledgeHandler(ctx context.Context, argumentsInJSON string) (string, error) {
	log.Info("Executing knowledge-base search tool")

	var params SearchKnowledgeParams
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
			response := NewErrorResponse("search_knowledge", "failed to parse arguments", "PARSE_ERROR", "check the format of the query parameter")
			return response.ToJSON()
		}
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		response := NewErrorResponse("search_knowledge", "query must not be empty", "INVALID_QUERY", "provide the text to search for")
		return response.ToJSON()
	}
	if params.TopK <= 0 {
		params.TopK = 5
	}

	chatSessionOperatorValue := ctx.Value("chat_session_operator")
	if chatSessionOperatorValue == nil {
		return "", fmt.Errorf("chat_session_operator not found in context")
	}
	chatSessionOperator, ok := chatSessionOperatorValue.(ChatSessionOperator)
	if !ok {
		return "", fmt.Errorf("chat_session_operator from context is not a ChatSessionOperator")
	}

	hits, err := chatSessionOperator.LocalMcpSearchKnowledge(ctx, params.Query, params.TopK, params.KnowledgeBaseIDs)
	if err != nil {
		response := NewErrorResponse("search_knowledge", fmt.Sprintf("knowledge search failed: %v", err), "SEARCH_FAILED", "try again later")
		return response.ToJSON()
	}

	data := map[string]interface{}{
		"query": params.Query,
		"hits":  hits,
		"count": len(hits),
	}
	if len(hits) == 0 {
		response := NewContentResponse("search_knowledge", data, "No sufficiently relevant information found")
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
		msg = "Relevant information retrieved"
	}
	response := NewContentResponse("search_knowledge", data, msg)
	return response.ToJSON()
}

// getWeekNumber returns week number
func getWeekNumber(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

// formatLocalDateTime formats a date/time with the Italian weekday name
func formatLocalDateTime(t time.Time) string {
	return fmt.Sprintf("%s %d/%d/%d %02d:%02d:%02d",
		getWeekdayName(t.Weekday()),
		t.Day(), int(t.Month()), t.Year(),
		t.Hour(), t.Minute(), t.Second(),
	)
}

// getWeekdayName returns the Italian weekday name
func getWeekdayName(weekday time.Weekday) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "domenica",
		time.Monday:    "lunedì",
		time.Tuesday:   "martedì",
		time.Wednesday: "mercoledì",
		time.Thursday:  "giovedì",
		time.Friday:    "venerdì",
		time.Saturday:  "sabato",
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
		return nil, "", fmt.Errorf("failed to get music URL: %v", ierr)
	}

	log.Infof("Music search succeeded URL: %s, title: %s", musicURL, realMusicName)

	client := getHTTPClient()
	req, err := http.NewRequest("GET", musicURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %v", err)
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
