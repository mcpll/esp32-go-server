package pocketbase

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/config/types"

	"github.com/spf13/viper"
)

const testDevice = "AA:BB:CC:DD:EE:01"

var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

func newTestProvider(t *testing.T, fake *fakePB) *Provider {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	p := NewProvider(NewClient(fake.URL(), fakeEmail, fakePassword))
	p.retryBase = 5 * time.Millisecond
	p.retryMax = 20 * time.Millisecond
	return p
}

// bind does what the owner does in the PocketBase dashboard: pick an agent and switch activated on.
func bind(fake *fakePB, device map[string]any, agentID string) {
	fake.patch("devices", device["id"].(string), map[string]any{"agent": agentID, "activated": true})
}

func seedItalianAgent(fake *fakePB) map[string]any {
	return fake.seed("agents", map[string]any{
		"name":         "Italiano",
		"prompt":       "Rispondi in italiano.",
		"asr_provider": "aliyun_qwen3",
		"asr_config":   map[string]any{"language": "it", "auto_end": false},
		"llm_provider": "aliyun",
		"llm_config":   map[string]any{"model_name": "qwen3.8-flash", "max_tokens": float64(300)},
		"tts_provider": "aliyun_qwen",
		"tts_config":   map[string]any{"voice": "Cherry", "language_type": "Italian"},
		"memory_mode":  "none",
		"openclaw": map[string]any{
			"allowed":        false,
			"enter_keywords": []any{"apri openclaw"},
			"exit_keywords":  []any{"chiudi openclaw"},
		},
		"mcp_service_names": []any{},
	})
}

func TestFreshDeviceGetsCodeAndChallenge(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()

	activated, err := p.IsDeviceActivated(ctx, testDevice, "client-1")
	if err != nil || activated {
		t.Fatalf("fresh device: activated=%v err=%v, want false/nil", activated, err)
	}
	if n := fake.count("devices"); n != 0 {
		t.Fatalf("checking activation must not create a record, got %d", n)
	}

	code, challenge, msg, timeoutMs := p.GetActivationInfo(ctx, testDevice, "client-1")
	if !sixDigits.MatchString(code) {
		t.Fatalf("code %q is not six digits", code)
	}
	if challenge == "" || msg == "" || timeoutMs <= 0 {
		t.Fatalf("incomplete activation info: challenge=%q msg=%q timeout=%d", challenge, msg, timeoutMs)
	}

	rec := fake.find("devices", "device_id", testDevice)
	if rec == nil || rec["code"] != code || rec["challenge"] != challenge || rec["activated"] != false {
		t.Fatalf("stored device does not match the answer: %v", rec)
	}
	if rec["client_id"] != "client-1" {
		t.Fatalf("client_id not stored: %v", rec["client_id"])
	}
}

func TestCodeDoesNotChangeUntilBound(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()

	code1, challenge1, _, _ := p.GetActivationInfo(ctx, testDevice, "c")
	code2, challenge2, _, _ := p.GetActivationInfo(ctx, testDevice, "c")
	if code1 != code2 || challenge1 != challenge2 {
		t.Fatalf("second poll changed the pair: %s/%s -> %s/%s", code1, challenge1, code2, challenge2)
	}
	if n := fake.count("devices"); n != 1 {
		t.Fatalf("want one device record, got %d", n)
	}
}

func TestBindingEndsActivation(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	agent := seedItalianAgent(fake)

	_, challenge, _, _ := p.GetActivationInfo(ctx, testDevice, "c")
	device := fake.find("devices", "device_id", testDevice)

	// Before binding the right challenge is still refused.
	if ok, err := p.VerifyChallenge(ctx, testDevice, "c", types.ActivationPayload{Challenge: challenge}); err != nil || ok {
		t.Fatalf("verify before binding: ok=%v err=%v, want false/nil", ok, err)
	}

	bind(fake, device, agent["id"].(string))

	if ok, err := p.IsDeviceActivated(ctx, testDevice, "c"); err != nil || !ok {
		t.Fatalf("after binding: activated=%v err=%v, want true/nil", ok, err)
	}
	if ok, _ := p.VerifyChallenge(ctx, testDevice, "c", types.ActivationPayload{Challenge: challenge}); !ok {
		t.Fatal("matching challenge on a bound device must verify")
	}
	if ok, _ := p.VerifyChallenge(ctx, testDevice, "c", types.ActivationPayload{Challenge: "wrong"}); ok {
		t.Fatal("wrong challenge must not verify")
	}
	if ok, _ := p.VerifyChallenge(ctx, testDevice, "c", types.ActivationPayload{}); ok {
		t.Fatal("empty challenge must not verify")
	}
	if ok, err := p.VerifyChallenge(ctx, "unknown-device", "c", types.ActivationPayload{Challenge: challenge}); err != nil || ok {
		t.Fatalf("unknown device: ok=%v err=%v, want false/nil", ok, err)
	}
}

func TestTurningActivatedOffShowsTheSameCodeAgain(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	agent := seedItalianAgent(fake)

	code, _, _, _ := p.GetActivationInfo(ctx, testDevice, "c")
	device := fake.find("devices", "device_id", testDevice)
	bind(fake, device, agent["id"].(string))
	fake.patch("devices", device["id"].(string), map[string]any{"activated": false})

	if ok, _ := p.IsDeviceActivated(ctx, testDevice, "c"); ok {
		t.Fatal("device with activated=false must read as not activated")
	}
	again, _, _, _ := p.GetActivationInfo(ctx, testDevice, "c")
	if again != code {
		t.Fatalf("code changed after the toggle: %s -> %s", code, again)
	}
}

func TestCodeCollisionPicksAnotherCode(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	fake.seed("devices", map[string]any{"device_id": "other", "code": "111111", "activated": false})

	codes := []string{"111111", "111111", "222222"}
	p.newCode = func() string {
		c := codes[0]
		if len(codes) > 1 {
			codes = codes[1:]
		}
		return c
	}

	code, _, _, _ := p.GetActivationInfo(context.Background(), testDevice, "c")
	if code != "222222" {
		t.Fatalf("code = %q, want the second candidate 222222", code)
	}
}

func TestConcurrentPollsCreateOneDevice(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)

	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _, _, _ = p.GetActivationInfo(context.Background(), testDevice, "c")
		}(i)
	}
	wg.Wait()

	if n := fake.count("devices"); n != 1 {
		t.Fatalf("want one device record, got %d", n)
	}
	for _, code := range results {
		if code == "" || code != results[0] {
			t.Fatalf("polls disagree: %v", results)
		}
	}
}

func TestActivationInfoIsEmptyWhenPocketBaseIsDown(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	fake.setDown(true)

	if code, _, _, _ := p.GetActivationInfo(context.Background(), testDevice, "c"); code != "" {
		t.Fatalf("code = %q while PocketBase is down, want empty", code)
	}
	if _, err := p.IsDeviceActivated(context.Background(), testDevice, "c"); err == nil {
		t.Fatal("IsDeviceActivated must return an error while PocketBase is down")
	}
}

func TestGetUserConfigMergesAgentOntoViperSections(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()

	viper.Set("asr.provider", "funasr")
	viper.Set("asr.aliyun_qwen3", map[string]any{"language": "zh", "auto_end": true, "api_key": "secret", "sample_rate": 16000})
	viper.Set("llm.provider", "other")
	viper.Set("llm.aliyun", map[string]any{"api_key": "llm-secret", "model_name": "old", "max_tokens": 100})
	viper.Set("tts.aliyun_qwen", map[string]any{"api_key": "tts-secret", "voice": "Old"})
	viper.Set("memory.provider", "mem0")
	viper.Set("memory.mem0", map[string]any{"api_key": "mem-secret"})
	viper.Set("vad.provider", "silero_vad")
	viper.Set("vad.silero_vad", map[string]any{"threshold": 0.5})

	agent := seedItalianAgent(fake)
	fake.patch("agents", agent["id"].(string), map[string]any{"mcp_service_names": []any{"a", "b"}, "speaker_chat_mode": "identified_only"})
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agent["id"].(string))

	cfg, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}

	if cfg.AgentId != agent["id"] {
		t.Fatalf("AgentId = %q, want the PocketBase string id %q", cfg.AgentId, agent["id"])
	}
	if cfg.SystemPrompt != "Rispondi in italiano." {
		t.Fatalf("SystemPrompt = %q", cfg.SystemPrompt)
	}

	// Both ASR keys come from the agent; the secret and the untouched default stay from the yaml section.
	if cfg.Asr.Provider != "aliyun_qwen3" || cfg.Asr.Config["language"] != "it" || cfg.Asr.Config["auto_end"] != false {
		t.Fatalf("asr = %+v", cfg.Asr)
	}
	if cfg.Asr.Config["api_key"] != "secret" || cfg.Asr.Config["sample_rate"] != 16000 {
		t.Fatalf("asr lost yaml values: %+v", cfg.Asr.Config)
	}
	if cfg.Llm.Provider != "aliyun" || cfg.Llm.Config["model_name"] != "qwen3.8-flash" || cfg.Llm.Config["api_key"] != "llm-secret" {
		t.Fatalf("llm = %+v", cfg.Llm)
	}
	if cfg.Tts.Provider != "aliyun_qwen" || cfg.Tts.Config["voice"] != "Cherry" || cfg.Tts.Config["language_type"] != "Italian" || cfg.Tts.Config["api_key"] != "tts-secret" {
		t.Fatalf("tts = %+v", cfg.Tts)
	}
	if cfg.Memory.Provider != "mem0" || cfg.Memory.Config["api_key"] != "mem-secret" {
		t.Fatalf("memory = %+v", cfg.Memory)
	}
	if cfg.Vad.Provider != "silero_vad" || cfg.Vad.Config["threshold"] != 0.5 {
		t.Fatalf("vad = %+v", cfg.Vad)
	}

	// The merge must not write the agent values back into the shared yaml config.
	if got := viper.GetString("asr.aliyun_qwen3.language"); got != "zh" {
		t.Fatalf("session merge leaked into viper: language = %q", got)
	}

	if cfg.MemoryMode != "none" || cfg.SpeakerChatMode != "identified_only" || cfg.MCPServiceNames != "a,b" {
		t.Fatalf("modes: memory=%q speaker=%q mcp=%q", cfg.MemoryMode, cfg.SpeakerChatMode, cfg.MCPServiceNames)
	}
	if cfg.OpenClaw.Allowed || len(cfg.OpenClaw.EnterKeywords) != 1 || cfg.OpenClaw.EnterKeywords[0] != "apri openclaw" || cfg.OpenClaw.ExitKeywords[0] != "chiudi openclaw" {
		t.Fatalf("openclaw = %+v", cfg.OpenClaw)
	}
	if len(cfg.VoiceIdentify) != 0 || len(cfg.KnowledgeBases) != 0 {
		t.Fatalf("voiceprint and knowledge stay empty until their tickets: %+v %+v", cfg.VoiceIdentify, cfg.KnowledgeBases)
	}
}

func TestGetUserConfigFallsBackToYamlProviderWhenAgentLeavesItEmpty(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	viper.Set("tts.provider", "edge")
	viper.Set("tts.edge", map[string]any{"voice": "it-IT-ElsaNeural"})

	agent := seedItalianAgent(fake)
	fake.patch("agents", agent["id"].(string), map[string]any{"tts_provider": "", "tts_config": map[string]any{}})
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agent["id"].(string))

	cfg, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.Tts.Provider != "edge" || cfg.Tts.Config["voice"] != "it-IT-ElsaNeural" {
		t.Fatalf("tts = %+v", cfg.Tts)
	}
}

func TestGetUserConfigRefusesUnknownAndUnboundDevices(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	agent := seedItalianAgent(fake)

	if _, err := p.GetUserConfig(ctx, "never-seen"); err == nil {
		t.Fatal("unknown device must be an error")
	}

	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	if _, err := p.GetUserConfig(ctx, testDevice); err == nil {
		t.Fatal("device that is not activated must be an error")
	}

	device := fake.find("devices", "device_id", testDevice)
	fake.patch("devices", device["id"].(string), map[string]any{"activated": true})
	if _, err := p.GetUserConfig(ctx, testDevice); err == nil {
		t.Fatal("activated device without an agent must be an error")
	}

	fake.patch("devices", device["id"].(string), map[string]any{"agent": agent["id"]})
	if _, err := p.GetUserConfig(ctx, testDevice); err != nil {
		t.Fatalf("bound device: %v", err)
	}
}

func TestAgentEditAppliesOnTheNextSession(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	agent := seedItalianAgent(fake)
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agent["id"].(string))

	first, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	fake.patch("agents", agent["id"].(string), map[string]any{"prompt": "Nuovo prompt."})
	second, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if first.SystemPrompt == second.SystemPrompt || second.SystemPrompt != "Nuovo prompt." {
		t.Fatalf("prompts: %q -> %q", first.SystemPrompt, second.SystemPrompt)
	}
}

func TestDeviceEventsUpdateOnlineState(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")

	p.NotifyDeviceEvent(ctx, types.EventDeviceOnline, map[string]interface{}{"device_id": testDevice})
	rec := fake.find("devices", "device_id", testDevice)
	if rec["online"] != true || rec["last_active_at"] == nil || rec["last_active_at"] == "" {
		t.Fatalf("after online: %v", rec)
	}

	p.NotifyDeviceEvent(ctx, types.EventDeviceOffline, map[string]interface{}{"device_id": testDevice})
	if rec := fake.find("devices", "device_id", testDevice); rec["online"] != false {
		t.Fatalf("after offline: %v", rec)
	}

	before := fake.count("devices")
	p.NotifyDeviceEvent(ctx, types.EventDeviceOnline, map[string]interface{}{"device_id": "not-in-table"})
	p.NotifyDeviceEvent(ctx, types.EventDeviceOnline, map[string]interface{}{})
	if fake.count("devices") != before {
		t.Fatal("events for unknown devices must not create records")
	}
}

func TestGetSystemConfigBuildsOneObjectFromSettings(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	fake.seed("settings", map[string]any{"key": "udp", "value": map[string]any{"listen_port": float64(8990)}})
	fake.seed("settings", map[string]any{"key": "chat", "value": map[string]any{"realtime_mode": float64(4)}})

	raw, err := p.GetSystemConfig(context.Background())
	if err != nil {
		t.Fatalf("GetSystemConfig: %v", err)
	}
	var got map[string]map[string]float64
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("not a JSON object: %v: %s", err, raw)
	}
	if got["udp"]["listen_port"] != 8990 || got["chat"]["realtime_mode"] != 4 {
		t.Fatalf("system config = %s", raw)
	}
}

func TestGetSystemConfigReadsMoreThanOnePage(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	for i := 0; i < 5; i++ {
		fake.seed("settings", map[string]any{"key": "k" + string(rune('a'+i)), "value": map[string]any{"n": float64(i)}})
	}
	p.client.perPage = 2

	cfg, err := p.LoadSystemConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg) != 5 {
		t.Fatalf("got %d blocks, want 5: %v", len(cfg), cfg)
	}
}

func TestStartLoadsSettingsAndSetsDevicesOffline(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	fake.seed("settings", map[string]any{"key": "udp", "value": map[string]any{"listen_port": float64(8990)}})
	fake.seed("devices", map[string]any{"device_id": "d1", "code": "123456", "online": true})
	fake.seed("devices", map[string]any{"device_id": "d2", "code": "654321", "online": false})

	var got map[string]interface{}
	p.Start(context.Background(), func(cfg map[string]interface{}) { got = cfg })

	if got["udp"] == nil {
		t.Fatalf("settings not delivered before Start returned: %v", got)
	}
	if rec := fake.find("devices", "device_id", "d1"); rec["online"] != false {
		t.Fatalf("device left online after start: %v", rec)
	}
}

func TestServerStartsWhileThePocketBaseIsDownAndRecovers(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	fake.seed("settings", map[string]any{"key": "udp", "value": map[string]any{"listen_port": float64(8990)}})
	fake.seed("devices", map[string]any{"device_id": "d1", "code": "123456", "online": true})
	fake.setDown(true)

	delivered := make(chan map[string]interface{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	returned := make(chan struct{})
	go func() {
		p.Start(ctx, func(cfg map[string]interface{}) { delivered <- cfg })
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Start blocked while PocketBase was down")
	}

	// Requests fail until it is back.
	if _, err := p.IsDeviceActivated(ctx, testDevice, "c"); err == nil {
		t.Fatal("expected an error while PocketBase is down")
	}

	fake.setDown(false)
	select {
	case cfg := <-delivered:
		if cfg["udp"] == nil {
			t.Fatalf("settings after recovery = %v", cfg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settings never arrived after PocketBase came back")
	}
	waitFor(t, func() bool { return fake.find("devices", "device_id", "d1")["online"] == false })

	if _, err := p.IsDeviceActivated(ctx, testDevice, "c"); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestClientReauthenticatesAfterA401(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()

	if _, err := p.IsDeviceActivated(ctx, testDevice, "c"); err != nil {
		t.Fatal(err)
	}
	if fake.auths() != 1 {
		t.Fatalf("auths = %d, want 1", fake.auths())
	}

	fake.invalidateTokens()
	if _, err := p.IsDeviceActivated(ctx, testDevice, "c"); err != nil {
		t.Fatalf("request after the token died: %v", err)
	}
	if fake.auths() != 2 {
		t.Fatalf("auths = %d, want 2 (one re-authentication)", fake.auths())
	}
}

func TestClientRefreshesTheTokenBeforeItExpires(t *testing.T) {
	fake := newFakePB(t)
	fake.setTokenTTL(30 * time.Second) // inside the refresh margin from the start
	p := newTestProvider(t, fake)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := p.IsDeviceActivated(ctx, testDevice, "c"); err != nil {
			t.Fatal(err)
		}
	}
	if fake.auths() != 1 {
		t.Fatalf("auths = %d, want 1: refresh should reuse the session", fake.auths())
	}
	if fake.refreshCount() < 1 {
		t.Fatal("token close to expiry was not refreshed")
	}
}

func TestClientReportsWrongCredentials(t *testing.T) {
	fake := newFakePB(t)
	viper.Reset()
	t.Cleanup(viper.Reset)
	p := NewProvider(NewClient(fake.URL(), fakeEmail, "wrong-password"))

	if _, err := p.IsDeviceActivated(context.Background(), testDevice, "c"); err == nil {
		t.Fatal("expected an authentication error")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestMergeSectionDoesNotShareNestedValuesWithViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("tts.aliyun_qwen", map[string]any{
		"voice":   "Old",
		"options": map[string]any{"speed": 1.0},
		"tags":    []any{"a"},
	})

	_, merged := mergeSection("tts", "aliyun_qwen", nil)
	merged["options"].(map[string]any)["speed"] = 2.0
	merged["tags"].([]any)[0] = "changed"

	if got := viper.GetStringMap("tts.aliyun_qwen")["options"].(map[string]any)["speed"]; got != 1.0 {
		t.Fatalf("nested map shared with viper: speed = %v", got)
	}
	if got := viper.GetStringMap("tts.aliyun_qwen")["tags"].([]any)[0]; got != "a" {
		t.Fatalf("nested slice shared with viper: tags[0] = %v", got)
	}
}
