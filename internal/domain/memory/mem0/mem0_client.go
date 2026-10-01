package mem0

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/hackers365/mem0-go/client"
	"github.com/hackers365/mem0-go/types"

	log "xiaozhi-esp32-server-golang/logger"
)

// Mem0Client implements MemoryProvider and EnhancedMemoryProvider
type Mem0Client struct {
	client          *client.MemoryClient
	config          Mem0Config
	mu              sync.RWMutex
	EnableSearch    bool    `mapstructure:"enable_search"`
	SearchThreshold float64 `mapstructure:"search_threshold"`
	SearchTopk      int     `mapstructure:"search_topk"`
}

// Mem0Config config struct
type Mem0Config struct {
	APIKey           string `mapstructure:"api_key"`
	BaseUrl          string `mapstructure:"base_url"`
	OrganizationName string `mapstructure:"organization_name"`
	ProjectName      string `mapstructure:"project_name"`
	OrganizationID   string `mapstructure:"organization_id"`
	ProjectID        string `mapstructure:"project_id"`
}

var (
	mem0Instance *Mem0Client
	mem0Once     sync.Once
	configOnce   sync.Once
)

// GetMem0ClientWithConfig returns the Mem0 client singleton from config
func GetMem0ClientWithConfig(config map[string]interface{}) (*Mem0Client, error) {
	var err error
	configOnce.Do(func() {
		var enableSearch bool = true
		var searchThreshold float64 = 0.5
		var searchTopk int = 3
		// parse config into the struct
		var mem0Cfg Mem0Config

		if enableSearchInterface, exists := config["enable_search"]; exists {
			if iEnableSearch, ok := enableSearchInterface.(bool); ok {
				enableSearch = iEnableSearch
			}
		}

		if searchThresholdInterface, exists := config["search_threshold"]; exists {
			if iSearchThreshold, ok := searchThresholdInterface.(float64); ok {
				searchThreshold = iSearchThreshold
			}
		}

		if searchTopkInterface, exists := config["search_topk"]; exists {
			if iSearchTopk, ok := searchTopkInterface.(int); ok {
				searchTopk = iSearchTopk
			}
		}

		// read API Key
		if apiKeyInterface, exists := config["api_key"]; exists {
			if apiKey, ok := apiKeyInterface.(string); ok {
				mem0Cfg.APIKey = apiKey
			} else {
				err = fmt.Errorf("mem0.api_key 必须是字符串")
				return
			}
		}

		// read Host
		if hostInterface, exists := config["base_url"]; exists {
			if host, ok := hostInterface.(string); ok {
				mem0Cfg.BaseUrl = host
			} else {
				err = fmt.Errorf("mem0.host 必须是字符串")
				return
			}
		}

		// validate required config
		if mem0Cfg.APIKey == "" {
			err = fmt.Errorf("mem0.api_key 配置缺失或为空")
			return
		}

		// set defaults
		if mem0Cfg.BaseUrl == "" {
			mem0Cfg.BaseUrl = "https://api.mem0.ai"
		}

		// create mem0 client
		clientOptions := client.ClientOptions{
			APIKey: mem0Cfg.APIKey,
			/*Host:             mem0Cfg.Host,
			OrganizationName: mem0Cfg.OrganizationName,
			ProjectName:      mem0Cfg.ProjectName,
			OrganizationID:   mem0Cfg.OrganizationID,
			ProjectID:        mem0Cfg.ProjectID,*/
		}

		mem0Client, clientErr := client.NewMemoryClient(clientOptions)
		if clientErr != nil {
			err = fmt.Errorf("failed to create mem0 client: %w", clientErr)
			return
		}

		mem0Instance = &Mem0Client{
			client:          mem0Client,
			config:          mem0Cfg,
			EnableSearch:    enableSearch,
			SearchThreshold: searchThreshold,
			SearchTopk:      searchTopk,
		}

		log.Log().Infof("Mem0 client initialized, base_url: %s", mem0Cfg.BaseUrl)
	})

	return mem0Instance, err
}

// Init initializes the client
func (m *Mem0Client) Init() error {
	// client was already initialized at creation
	log.Log().Info("Mem0 client initialized successfully")
	return nil
}

// Get fetches memories (internal)
func (m *Mem0Client) Get(userID string) (interface{}, error) {
	// search all memories for the user
	results, err := m.client.Search("", &types.SearchOptions{
		MemoryOptions: types.MemoryOptions{
			UserID: userID,
		},
		Limit: 100, // fetch more memories
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search memories for user %s: %w", userID, err)
	}

	return results, nil
}

// AddMessage adds a message to memory
func (m *Mem0Client) AddMessage(ctx context.Context, agentID string, msg schema.Message) error {
	message := types.Message{
		Role:    string(msg.Role),
		Content: msg.Content,
	}
	// add memory
	_, err := m.client.Add([]types.Message{message}, types.MemoryOptions{
		AgentID:   agentID,
		AsyncMode: true,
	})
	if err != nil {
		return fmt.Errorf("failed to add message to mem0 for user %s: %w", agentID, err)
	}

	log.Log().Debugf("Added message to mem0 for user %s: %s", agentID, message)
	return nil
}

// GetMessages returns the user's message history
func (m *Mem0Client) GetMessages(ctx context.Context, agentID string, count int) ([]*schema.Message, error) {
	var memoryOptions = types.MemoryOptions{
		AgentID: agentID,
	}

	results, err := m.client.GetAll(&types.SearchOptions{
		MemoryOptions: memoryOptions,
		Limit:         count,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get messages for user %s: %w", agentID, err)
	}

	// convert to schema.Message
	var messages []*schema.Message
	for _, result := range results {
		// extract role and content from metadata
		role := schema.Assistant // default role
		content := result.Memory

		if result.Metadata != nil {
			if r, ok := result.Metadata["role"].(string); ok {
				switch r {
				case "user":
					role = schema.User
				case "assistant":
					role = schema.Assistant
				case "system":
					role = schema.System
				}
			}
			if c, ok := result.Metadata["content"].(string); ok {
				content = c
			}
		}

		messages = append(messages, &schema.Message{
			Role:    role,
			Content: content,
		})
	}

	return messages, nil
}

// ResetMemory resets user memory
func (m *Mem0Client) ResetMemory(ctx context.Context, userID string) error {

	// delete all memories for the user
	err := m.client.DeleteUser(userID)
	if err != nil {
		return fmt.Errorf("failed to reset memory for user %s: %w", userID, err)
	}

	log.Log().Infof("Reset memory for user %s", userID)
	return nil
}

// GetContext returns context (EnhancedMemoryProvider)
func (m *Mem0Client) GetContext(ctx context.Context, agentID string, maxToken int) (string, error) {
	return "", nil
}

func (m *Mem0Client) IsEnableSearch() bool {
	return m.EnableSearch
}

func (m *Mem0Client) Search(ctx context.Context, agentId string, query string, topK int, timeRangeDays int64) (string, error) {
	if !m.EnableSearch {
		return "", nil
	}
	topK = m.SearchTopk
	results, err := m.actionSearch(ctx, agentId, query, topK, m.SearchThreshold)
	if err != nil {
		return "", err
	}

	// build the context string
	var msgList []string
	for _, result := range results {
		msgList = append(msgList, fmt.Sprintf("- %s [%s]", result.Memory, result.CreatedAt))
	}

	return strings.Join(msgList, "\n"), nil
}

func (m *Mem0Client) Flush(ctx context.Context, agentID string) error {
	return nil
}

func (m *Mem0Client) actionSearch(ctx context.Context, agentID string, query string, topK int, threshold float64) ([]types.Memory, error) {
	// search related memories
	results, err := m.client.Search(query, &types.SearchOptions{
		MemoryOptions: types.MemoryOptions{
			AgentID: agentID,
		},
		Limit:     topK,      // fetch topK memories
		Threshold: threshold, // set similarity threshold
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get context for user %s: %w", agentID, err)
	}

	log.Log().Debugf("got context from mem0, agentID: %s, results_len: %d", agentID, len(results))
	return results, nil
}

// AddBatchMessages adds messages in batch
func (m *Mem0Client) AddBatchMessages(ctx context.Context, agentID string, messages []schema.Message) error {

	// prepare batch messages
	var batchMessages []string
	for _, msg := range messages {
		message := fmt.Sprintf("%s: %s", msg.Role, msg.Content)
		batchMessages = append(batchMessages, message)
	}

	// add memories one by one (mem0-go may lack batch add)
	for _, message := range batchMessages {
		_, err := m.client.Add(message, types.MemoryOptions{
			AgentID: agentID,
			Metadata: map[string]interface{}{
				"source": "xiaozhi-esp32",
				"batch":  true,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add batch message to mem0 for user %s: %w", agentID, err)
		}
	}

	log.Log().Debugf("Added %d batch messages to mem0 for user %s", len(messages), agentID)
	return nil
}

// Close closes the client
func (m *Mem0Client) Close() error {
	// mem0-go client does not need an explicit close
	log.Log().Info("Mem0 client closed")
	return nil
}

// ensure Mem0Client implements the required interfaces
// Note: cannot import the memory package here (would create an import cycle)
// interface conformance is checked at compile time
