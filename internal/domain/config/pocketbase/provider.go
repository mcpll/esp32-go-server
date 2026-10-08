package pocketbase

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/config/store"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/google/uuid"
)

const (
	devicesCollection  = "devices"
	settingsCollection = "settings"

	// redisAvailableKey is the settings record the console reads to know
	// whether short memory can be chosen. It is not the Redis client config.
	redisAvailableKey = "redis_available"

	// activationTimeoutMs is how long the device keeps showing its code (five minutes).
	activationTimeoutMs = 300000
	// createAttempts bounds the code-collision retries when creating a device record.
	createAttempts = 8

	startupAttemptTimeout = 5 * time.Second
	retryAttemptTimeout   = 10 * time.Second
	defaultRetryBase      = time.Second
	defaultRetryMax       = 30 * time.Second

	// pocketBaseDateLayout is how PocketBase writes and reads date fields (UTC).
	pocketBaseDateLayout = "2006-01-02 15:04:05.000Z"
)

// Provider implements the user-config seam on top of PocketBase.
type Provider struct {
	client *Client

	// newCode makes a six-digit activation code. Tests replace it.
	newCode func() string

	// retryBase and retryMax bound the background startup retry.
	retryBase time.Duration
	retryMax  time.Duration

	// watchBase and watchMax bound the realtime reconnect backoff.
	watchBase time.Duration
	watchMax  time.Duration

	createMu sync.Mutex // one device record is created at a time, so concurrent OTA polls do not race

	handlersMu sync.RWMutex
	handlers   map[string]types.EventHandler
}

// NewProvider returns a provider that reads and writes through client.
func NewProvider(client *Client) *Provider {
	return &Provider{
		client:    client,
		newCode:   randomSixDigits,
		retryBase: defaultRetryBase,
		retryMax:  defaultRetryMax,
		watchBase: time.Second,
		watchMax:  30 * time.Second,
		handlers:  map[string]types.EventHandler{},
	}
}

func randomSixDigits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		// crypto/rand does not fail on supported platforms; a clock-based code still beats none.
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// ---- activation ----

// IsDeviceActivated reports whether a devices record exists with activated = true.
// A device that is not in the table is simply not activated.
func (p *Provider) IsDeviceActivated(ctx context.Context, deviceId string, clientId string) (bool, error) {
	rec, err := p.client.First(ctx, devicesCollection, textEquals("device_id", deviceId), "")
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rec.Bool("activated"), nil
}

// GetActivationInfo finds or creates the devices record and returns its code, challenge, an Italian
// message and the display timeout. The pair stays the same until the owner binds the device.
// On any error it returns an empty code, which callers treat as "no activation info".
func (p *Provider) GetActivationInfo(ctx context.Context, deviceId string, clientId string) (string, string, string, int) {
	rec, err := p.findOrCreateDevice(ctx, deviceId, clientId)
	if err != nil {
		log.Errorf("pocketbase: activation info for device %s: %v", deviceId, err)
		return "", "", "", 0
	}
	code := rec.String("code")
	message := fmt.Sprintf("Codice di attivazione\n%s", code)
	return code, rec.String("challenge"), message, activationTimeoutMs
}

func (p *Provider) findOrCreateDevice(ctx context.Context, deviceId, clientId string) (Record, error) {
	if strings.TrimSpace(deviceId) == "" {
		return nil, errors.New("empty device id")
	}
	filter := textEquals("device_id", deviceId)

	rec, err := p.client.First(ctx, devicesCollection, filter, "")
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	p.createMu.Lock()
	defer p.createMu.Unlock()

	var lastErr error
	for i := 0; i < createAttempts; i++ {
		// Look again on every round: a concurrent poll, or another server, may have created it.
		rec, err := p.client.First(ctx, devicesCollection, filter, "")
		if err == nil {
			return rec, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		rec, err = p.client.Create(ctx, devicesCollection, map[string]any{
			"device_id": deviceId,
			"client_id": clientId,
			"code":      p.newCode(),
			"challenge": uuid.NewString(),
			"activated": false,
		})
		if err == nil {
			log.Infof("pocketbase: new device %s waiting for activation, code %s", deviceId, rec.String("code"))
			return rec, nil
		}
		// A 400 here is a unique-index hit, almost always on the code. Try another one.
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 400 {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("could not create device record after %d attempts: %w", createAttempts, lastErr)
}

// VerifyChallenge is true only when the device is activated and the challenge matches.
// Stock firmware never reaches this endpoint (see the known limits in the spec).
func (p *Provider) VerifyChallenge(ctx context.Context, deviceId string, clientId string, activationPayload types.ActivationPayload) (bool, error) {
	rec, err := p.client.First(ctx, devicesCollection, textEquals("device_id", deviceId), "")
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !rec.Bool("activated") {
		return false, nil
	}
	stored, given := rec.String("challenge"), activationPayload.Challenge
	if stored == "" || given == "" {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(given)) == 1, nil
}

// ---- per-session config ----

// GetUserConfig builds the config of one session from the device record and its agent.
// Unknown, unactivated and agent-less devices are errors.
func (p *Provider) GetUserConfig(ctx context.Context, deviceID string) (types.UConfig, error) {
	rec, err := p.client.First(ctx, devicesCollection, textEquals("device_id", deviceID), "agent")
	if errors.Is(err, ErrNotFound) {
		return types.UConfig{}, fmt.Errorf("unknown device %s", deviceID)
	}
	if err != nil {
		return types.UConfig{}, err
	}
	if !rec.Bool("activated") {
		return types.UConfig{}, fmt.Errorf("device %s is not activated", deviceID)
	}
	agent := rec.Expanded("agent")
	if agent == nil {
		return types.UConfig{}, fmt.Errorf("device %s is activated but has no agent", deviceID)
	}

	asrProvider, asrConfig := mergeSection("asr", agent.String("asr_provider"), agent.Object("asr_config"))
	llmProvider, llmConfig := mergeSection("llm", agent.String("llm_provider"), agent.Object("llm_config"))
	ttsProvider, ttsConfig := mergeSection("tts", agent.String("tts_provider"), agent.Object("tts_config"))
	memoryProvider, memoryConfig := memorySection(agent.String("memory_mode"))
	vadProvider, vadConfig := mergeSection("vad", "", nil)

	openclaw := Record(agent.Object("openclaw"))

	cfg := types.UConfig{
		SystemPrompt:    agent.String("prompt"),
		Asr:             types.AsrConfig{Provider: asrProvider, Config: asrConfig},
		Llm:             types.LlmConfig{Provider: llmProvider, Config: llmConfig},
		Tts:             types.TtsConfig{Provider: ttsProvider, Config: ttsConfig},
		Memory:          types.MemoryConfig{Provider: memoryProvider, Config: memoryConfig},
		Vad:             types.VadConfig{Provider: vadProvider, Config: vadConfig},
		VoiceIdentify:   map[string]types.SpeakerGroupInfo{},
		MemoryMode:      normalizeMemoryMode(agent.String("memory_mode")),
		SpeakerChatMode: normalizeSpeakerChatMode(agent.String("speaker_chat_mode")),
		AgentId:         agent.String("id"),
		MCPServiceNames: strings.Join(agent.Strings("mcp_service_names"), ","),
		OpenClaw: types.OpenClawConfig{
			Allowed:       openclaw.Bool("allowed"),
			EnterKeywords: openclaw.Strings("enter_keywords"),
			ExitKeywords:  openclaw.Strings("exit_keywords"),
		},
	}
	return cfg, nil
}

// mergeSection returns the provider name and its config for one stage. The base is the yaml
// section viper holds for that provider; the agent's JSON keys are laid over a copy of it, so
// secrets and defaults stay in the yaml and nothing is written back into viper.
func mergeSection(stage, agentProvider string, overrides map[string]any) (string, map[string]any) {
	provider := strings.TrimSpace(agentProvider)
	if provider == "" {
		provider = store.GetString(stage + ".provider")
	}
	merged := map[string]any{}
	if provider != "" {
		for k, v := range store.GetStringMap(stage + "." + provider) {
			merged[k] = cloneValue(v)
		}
	}
	for k, v := range overrides {
		merged[k] = cloneValue(v)
	}
	return provider, merged
}

// cloneValue copies nested maps and slices so a session never shares memory with viper.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	default:
		return v
	}
}

// memorySection follows memory_mode. long uses the system mem0 config.
// none and short do not open a long-memory provider.
func memorySection(mode string) (string, map[string]any) {
	if normalizeMemoryMode(mode) != "long" {
		return "nomemo", map[string]any{}
	}
	_, cfg := mergeSection("memory", "mem0", nil)
	return "mem0", cfg
}

func normalizeMemoryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "short":
		return "short"
	case "long":
		return "long"
	default:
		return "none"
	}
}

func normalizeSpeakerChatMode(mode string) string {
	if strings.ToLower(strings.TrimSpace(mode)) == "identified_only" {
		return "identified_only"
	}
	return "off"
}

// ---- system config ----

// LoadSystemConfig reads every settings record into one object: key -> value.
func (p *Provider) LoadSystemConfig(ctx context.Context) (map[string]interface{}, error) {
	records, err := p.client.List(ctx, settingsCollection, "", "")
	if err != nil {
		return nil, err
	}
	cfg := make(map[string]interface{}, len(records))
	for _, rec := range records {
		key := rec.String("key")
		if key == "" {
			continue
		}
		cfg[key] = rec["value"]
	}
	return cfg, nil
}

// GetSystemConfig returns the settings blocks as a JSON object, the form the viper merge expects.
func (p *Provider) GetSystemConfig(ctx context.Context) (string, error) {
	cfg, err := p.LoadSystemConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("load settings: %w", err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode settings: %w", err)
	}
	return string(raw), nil
}

// ---- events ----

// NotifyDeviceEvent records a device going online or offline. Devices that are not in the
// table (auth disabled, or never seen by OTA) are ignored.
func (p *Provider) NotifyDeviceEvent(ctx context.Context, eventType string, eventData map[string]interface{}) {
	var online bool
	switch eventType {
	case types.EventDeviceOnline:
		online = true
	case types.EventDeviceOffline:
		online = false
	default:
		return
	}
	deviceID, _ := eventData["device_id"].(string)
	if deviceID == "" {
		return
	}
	rec, err := p.client.First(ctx, devicesCollection, textEquals("device_id", deviceID), "")
	if errors.Is(err, ErrNotFound) {
		return
	}
	if err != nil {
		log.Warnf("pocketbase: cannot find device %s for event %s: %v", deviceID, eventType, err)
		return
	}
	err = p.client.Update(ctx, devicesCollection, rec.String("id"), map[string]any{
		"online":         online,
		"last_active_at": time.Now().UTC().Format(pocketBaseDateLayout),
	})
	if err != nil {
		log.Warnf("pocketbase: cannot update device %s for event %s: %v", deviceID, eventType, err)
	}
}

// RegisterMessageEventHandler keeps the handler for a downlink event. The commands channel
// that fires them arrives in a later ticket.
func (p *Provider) RegisterMessageEventHandler(ctx context.Context, eventType string, eventHandler types.EventHandler) {
	p.handlersMu.Lock()
	defer p.handlersMu.Unlock()
	p.handlers[eventType] = eventHandler
}

// ---- startup ----

// Start does the work that needs PocketBase: it loads the settings and marks every device offline.
// The first attempt runs before Start returns, so the settings are in place before the servers
// start. If PocketBase does not answer, Start returns anyway and keeps retrying in the background
// until the work is done or ctx ends. onSystemConfig receives the settings object on each load.
func (p *Provider) Start(ctx context.Context, onSystemConfig func(map[string]interface{})) {
	go p.watchSettings(ctx, onSystemConfig)

	first, cancel := context.WithTimeout(ctx, startupAttemptTimeout)
	err := p.startup(first, onSystemConfig)
	cancel()
	if err == nil {
		return
	}
	log.Warnf("pocketbase: not reachable at startup, retrying in the background: %v", err)
	go p.retryStartup(ctx, onSystemConfig)
}

// watchSettings subscribes to settings and reloads them on every event and
// after every reconnect. Events are lost while the stream is down, so a
// reconnect always reloads the whole collection.
func (p *Provider) watchSettings(ctx context.Context, onSystemConfig func(map[string]interface{})) {
	base, max := p.watchBase, p.watchMax
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	delay := base
	for {
		if ctx.Err() != nil {
			return
		}
		subscribed, err := p.client.Watch(ctx, []string{settingsCollection + "/*"}, func() {
			p.reloadSettings(ctx, onSystemConfig)
		}, func(event string) {
			if strings.HasPrefix(event, settingsCollection) {
				p.reloadSettings(ctx, onSystemConfig)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warnf("pocketbase: settings realtime dropped: %v", err)
		}
		if subscribed {
			delay = base
		} else {
			delay *= 2
			if delay > max {
				delay = max
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *Provider) reloadSettings(ctx context.Context, onSystemConfig func(map[string]interface{})) {
	if onSystemConfig == nil {
		return
	}
	cfg, err := p.LoadSystemConfig(ctx)
	if err != nil {
		log.Warnf("pocketbase: reload settings: %v", err)
		return
	}
	onSystemConfig(cfg)
}

func (p *Provider) retryStartup(ctx context.Context, onSystemConfig func(map[string]interface{})) {
	delay := p.retryBase
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		attempt, cancel := context.WithTimeout(ctx, retryAttemptTimeout)
		err := p.startup(attempt, onSystemConfig)
		cancel()
		if err == nil {
			log.Infof("pocketbase: reachable again, startup work done")
			return
		}
		log.Warnf("pocketbase: still not ready: %v", err)
		delay *= 2
		if delay > p.retryMax {
			delay = p.retryMax
		}
	}
}

func (p *Provider) startup(ctx context.Context, onSystemConfig func(map[string]interface{})) error {
	if err := p.publishRedisAvailable(ctx); err != nil {
		return err
	}
	cfg, err := p.LoadSystemConfig(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	if onSystemConfig != nil {
		onSystemConfig(cfg)
	}
	return p.markAllDevicesOffline(ctx)
}

// publishRedisAvailable records whether Redis is on so the console can disable short memory.
// The value is copied from redis.enable. A later settings merge must not turn the client on.
func (p *Provider) publishRedisAvailable(ctx context.Context) error {
	enabled := store.GetBool("redis.enable")
	value := map[string]any{"enabled": enabled}
	rec, err := p.client.First(ctx, settingsCollection, textEquals("key", redisAvailableKey), "")
	if errors.Is(err, ErrNotFound) {
		_, err = p.client.Create(ctx, settingsCollection, map[string]any{
			"key":   redisAvailableKey,
			"value": value,
		})
		return err
	}
	if err != nil {
		return err
	}
	current := rec.Object("value")
	if current != nil && current["enabled"] == enabled {
		return nil
	}
	return p.client.Update(ctx, settingsCollection, rec.String("id"), map[string]any{"value": value})
}

// markAllDevicesOffline clears the online flag that a previous run may have left behind.
func (p *Provider) markAllDevicesOffline(ctx context.Context) error {
	records, err := p.client.List(ctx, devicesCollection, "online = true", "")
	if err != nil {
		return fmt.Errorf("list online devices: %w", err)
	}
	for _, rec := range records {
		if err := p.client.Update(ctx, devicesCollection, rec.String("id"), map[string]any{"online": false}); err != nil {
			return fmt.Errorf("set device %s offline: %w", rec.String("device_id"), err)
		}
	}
	return nil
}
