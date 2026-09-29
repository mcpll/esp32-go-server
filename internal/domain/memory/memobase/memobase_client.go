package memobase

import (
	"context"
	"fmt"
	"strings"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/memodb-io/memobase/src/client/memobase-go/blob"
	"github.com/memodb-io/memobase/src/client/memobase-go/core"
)

var (
	clientInstance *MemobaseClient
	once           sync.Once
	configOnce     sync.Once
	// Fixed namespace UUID used to derive device-ID UUID v5
	// so the same device ID always maps to the same UUID
	deviceNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8") // DNS namespace
)

// MemobaseClient Memobase client manager
type MemobaseClient struct {
	client *core.MemoBaseClient
	users  sync.Map // cached user objects
	sync.RWMutex
	EnableSearch    bool
	SearchThreshold float64
	SearchTopk      int
}

// GetWithConfig returns a Memobase client singleton from config
func GetWithConfig(config map[string]interface{}) (*MemobaseClient, error) {
	var initErr error
	configOnce.Do(func() {
		iClient := &MemobaseClient{
			users: sync.Map{},
		}
		// Read memobase settings from config		// Read required settings
		projectUrlInterface, ok := config["base_url"]
		if !ok {
			initErr = fmt.Errorf("memobase.base_url 配置缺失")
			return
		}
		baseUrl, ok := projectUrlInterface.(string)
		if !ok {
			initErr = fmt.Errorf("memobase.base_url 必须是字符串")
			return
		}

		apiKeyInterface, ok := config["api_key"]
		if !ok {
			initErr = fmt.Errorf("memobase.api_key 配置缺失")
			return
		}
		apiKey, ok := apiKeyInterface.(string)
		if !ok {
			initErr = fmt.Errorf("memobase.api_key 必须是字符串")
			return
		}

		if baseUrl == "" || apiKey == "" {
			initErr = fmt.Errorf("Memobase 配置不完整: base_url 或 api_key 为空")
			log.Log().Errorf("Memobase init failed: %v", initErr)
			return
		}

		// Read optional search settings
		enableSearchInterface, ok := config["enable_search"]
		if ok {
			enableSearch, ok := enableSearchInterface.(bool)
			if ok {
				iClient.EnableSearch = enableSearch
			}
		}

		thresholdInterface, ok := config["search_threshold"]
		if ok {
			threshold, ok := thresholdInterface.(float64)
			if ok {
				iClient.SearchThreshold = threshold
			}
		}

		topKInterface, ok := config["search_topk"]
		if ok {
			topK, ok := topKInterface.(int)
			if ok {
				iClient.SearchTopk = topK
			}
		}

		// Create client
		client, err := core.NewMemoBaseClient(baseUrl, apiKey)
		if err != nil {
			initErr = fmt.Errorf("创建 Memobase 客户端失败: %v", err)
			log.Log().Errorf("Memobase init failed: %v", initErr)
			return
		}

		iClient.client = client
		clientInstance = iClient

		log.Log().Infof("Memobase client initialized, project_url: %s", baseUrl)
	})

	if initErr != nil {
		return nil, initErr
	}
	return clientInstance, nil
}

// deviceIDToUUID converts a device ID to UUID v5
// UUID v5 ensures the same device ID always yields the same UUID
func deviceIDToUUID(deviceID string) string {
	return uuid.NewSHA1(deviceNamespace, []byte(deviceID)).String()
}

func IsEnableSearch() bool {
	return clientInstance.EnableSearch
}

// AddMessage adds a message to Memobase
func (m *MemobaseClient) AddMessage(ctx context.Context, agentID string, msg schema.Message) error {
	memobaseUserID := deviceIDToUUID(agentID)
	// Build message
	messages := []blob.OpenAICompatibleMessage{
		{
			Role:    string(msg.Role),
			Content: msg.Content,
		},
	}

	// If there are tool calls, add them to the message
	if len(msg.ToolCalls) > 0 {
		return nil
		/*for _, toolCall := range msg.ToolCalls {
			messages = append(messages, blob.OpenAICompatibleMessage{
				Role:    "tool",
				Content: fmt.Sprintf("Tool: %s, Args: %v", toolCall.Function.Name, toolCall.Function.Arguments),
			})
		}*/
	}

	// Create ChatBlob
	chatBlob := &blob.ChatBlob{
		BaseBlob: blob.BaseBlob{
			Type: blob.ChatType,
		},
		Messages: messages,
	}

	// Get or create user instance (UUID-format userID)
	user, err := m.getUser(memobaseUserID)
	if err != nil {
		log.Log().Errorf("failed to get or create user, agentID: %s, memobaseUserID: %s, error: %v", agentID, memobaseUserID, err)
		return fmt.Errorf("获取或创建用户失败: %v", err)
	}

	// Insert message (async)
	blobID, err := user.Insert(chatBlob, false)
	if err != nil {
		log.Log().Errorf("failed to add message to Memobase, deviceID: %s, error: %v", agentID, err)
		return fmt.Errorf("添加消息到Memobase失败: %v", err)
	}

	//user.Flush(blob.ChatType, false)

	log.Log().Debugf("added message to Memobase, deviceID: %s, blobID: %s", agentID, blobID)
	return nil
}

func (m *MemobaseClient) Flush(ctx context.Context, agentID string) error {
	memobaseUserID := deviceIDToUUID(agentID)
	user, err := m.getUser(memobaseUserID)
	if err != nil {
		log.Log().Errorf("failed to flush user memory, agentID: %s, memobaseUserID: %s, error: %v", agentID, memobaseUserID, err)
		return fmt.Errorf("刷新用户记忆失败: %v", err)
	}
	user.Flush(blob.ChatType, false)
	return nil
}

// GetContext returns user context
func (m *MemobaseClient) GetContext(ctx context.Context, agentID string, maxToken int) (string, error) {

	// Convert device ID to UUID format (required by Memobase)
	memobaseUserID := deviceIDToUUID(agentID)

	// Get user instance (no HTTP GET; create instance only)
	user, err := m.getUser(memobaseUserID)
	if err != nil {
		log.Log().Errorf("failed to get user instance, agentID: %s, memobaseUserID: %s, error: %v", agentID, memobaseUserID, err)
		return "", fmt.Errorf("获取用户实例失败: %v", err)
	}

	// Get context with default options
	context, err := user.Context(&core.ContextOptions{
		MaxTokenSize: maxToken,
	})
	if err != nil {
		log.Log().Errorf("failed to get context from Memobase, agentID: %s, memobaseUserID: %s, error: %v", agentID, memobaseUserID, err)
		return "", fmt.Errorf("从Memobase获取上下文失败: %v", err)
	}

	log.Log().Debugf("got context from Memobase, agentID: %s, context_len: %d", agentID, len(context))
	return context, nil
}

func (m *MemobaseClient) Search(ctx context.Context, agentID string, query string, topK int, timeRangeDays int64) (string, error) {
	if !m.EnableSearch {
		return "", nil
	}
	topK = m.SearchTopk
	// Convert device ID to UUID format (required by Memobase)
	memobaseUserID := deviceIDToUUID(agentID)

	// Get user instance (no HTTP GET; create instance only)
	user, err := m.getUser(memobaseUserID)
	if err != nil {
		log.Log().Errorf("failed to get user instance, agentID: %s, memobaseUserID: %s, error: %v", agentID, memobaseUserID, err)
		return "", fmt.Errorf("获取用户实例失败: %v", err)
	}

	topK = 2

	// Search events
	userEventList, err := user.SearchEvent(query, topK, 0.2, int(timeRangeDays))
	if err != nil {
		log.Log().Errorf("failed to search events from Memobase, agentID: %s, error: %v", agentID, err)
		return "", fmt.Errorf("从Memobase搜索事件失败: %v", err)
	}

	var eventList []string
	for _, event := range userEventList {
		eventList = append(eventList, fmt.Sprintf("- %s: %s", event.CreatedAt, event.EventData.EventTip))
	}

	// Convert to string
	userEventStr := strings.Join(eventList, "\n")

	log.Log().Debugf("searched events from Memobase, agentID: %s, event_count: %d", agentID, len(eventList))
	return userEventStr, nil
}

// AddBatchMessages batch-adds messages to Memobase
func (m *MemobaseClient) AddBatchMessages(ctx context.Context, userID string, messages []schema.Message) error {
	m.Lock()
	defer m.Unlock()

	if len(messages) == 0 {
		return nil
	}

	// Convert message format
	blobMessages := make([]blob.OpenAICompatibleMessage, 0, len(messages))
	for _, msg := range messages {
		blobMessages = append(blobMessages, blob.OpenAICompatibleMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	// Create ChatBlob
	chatBlob := &blob.ChatBlob{
		BaseBlob: blob.BaseBlob{
			Type: blob.ChatType,
		},
		Messages: blobMessages,
	}

	// Convert device ID to UUID format (required by Memobase)
	memobaseUserID := deviceIDToUUID(userID)

	// Get or create user instance (UUID-format userID)
	user, err := m.getUser(userID)
	if err != nil {
		log.Log().Errorf("batch add messages: failed to get or create user, deviceID: %s, memobaseUserID: %s, error: %v", userID, memobaseUserID, err)
		return fmt.Errorf("获取或创建用户失败: %v", err)
	}

	// Insert message (async)
	blobID, err := user.Insert(chatBlob, false)
	if err != nil {
		log.Log().Errorf("failed to batch-add messages to Memobase, deviceID: %s, error: %v", userID, err)
		return fmt.Errorf("批量添加消息到Memobase失败: %v", err)
	}

	log.Log().Debugf("batch-added %d messages to Memobase, deviceID: %s, blobID: %s", len(messages), userID, blobID)
	return nil
}

// GetMessages returns user history messages
// Implements BaseMemoryProvider
// Note: Memobase is for long-term memory and context; it does not retrieve history messages
func (m *MemobaseClient) GetMessages(ctx context.Context, agentID string, count int) ([]*schema.Message, error) {
	return []*schema.Message{}, nil
}

// ResetMemory resets user memory
// Implements MemoryProvider
// Note: Memobase memory reset requires deleting user data via API
func (m *MemobaseClient) ResetMemory(ctx context.Context, userID string) error {
	// TODO: call Memobase SDK user-data delete API here if available
	// Currently returns nil as success even if nothing was deleted
	log.Log().Infof("Memobase reset memory request: userID=%s (note: Memobase does not support direct reset)", userID)
	return nil
}

// Close shuts down the client if needed
func (m *MemobaseClient) Close() error {
	log.Log().Info("Memobase client closed")
	return nil
}

// todo: add user object cache
func (m *MemobaseClient) getUser(userID string) (*core.User, error) {
	if user, ok := m.users.Load(userID); ok {
		return user.(*core.User), nil
	}

	memobaseUserID := deviceIDToUUID(userID)
	user, err := m.client.GetOrCreateUser(memobaseUserID)
	if err != nil {
		return nil, fmt.Errorf("获取用户实例失败: %v", err)
	}

	m.users.Store(userID, user)
	return user, nil
}
