package chat

import (
	"context"
	"strings"
	"testing"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"

	"github.com/spf13/viper"
)

func TestIdentifiedOnlyDropsAStrangerAndKeepsAnEnrolledVoice(t *testing.T) {
	voice := "Serena"
	session := &ChatSession{clientState: &data_client.ClientState{
		DeviceConfig: types.UConfig{
			SpeakerChatMode: data_client.SpeakerChatModeIdentifiedOnly,
			VoiceIdentify: map[string]types.SpeakerGroupInfo{
				"Matteo": {Name: "Matteo", Prompt: "Dagli del tu.", Voice: &voice},
			},
		},
	}}

	allow, reason := session.ShouldAllowSpeakerChat(&speaker.IdentifyResult{
		Identified: true, SpeakerName: "Sconosciuto",
	}, false)
	if allow || reason != "speaker_chat_mode_identified_only_not_matched" {
		t.Fatalf("stranger allowed: %v %q", allow, reason)
	}

	allow, reason = session.ShouldAllowSpeakerChat(&speaker.IdentifyResult{Identified: false}, false)
	if allow || reason != "speaker_chat_mode_identified_only_not_matched" {
		t.Fatalf("unidentified turn allowed: %v %q", allow, reason)
	}

	allow, reason = session.ShouldAllowSpeakerChat(&speaker.IdentifyResult{
		Identified: true, SpeakerName: "Matteo",
	}, false)
	if !allow || reason != "" {
		t.Fatalf("enrolled voice dropped: %v %q", allow, reason)
	}
}

func TestRecognisedSpeakerAddsAnItalianPrompt(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	manager := &LLMManager{clientState: &data_client.ClientState{
		Dialogue:     &data_client.Dialogue{},
		SystemPrompt: "Rispondi in italiano.",
		DeviceConfig: types.UConfig{
			MemoryMode: data_client.MemoryModeNone,
			VoiceIdentify: map[string]types.SpeakerGroupInfo{
				"Matteo": {Name: "Matteo", Prompt: "Dagli del tu."},
			},
		},
	}}

	messages := manager.GetMessages(context.Background(), nil, 10, &speaker.IdentifyResult{
		Identified: true, SpeakerName: "Matteo",
	})
	if len(messages) == 0 || messages[0].Role != "system" {
		t.Fatalf("messages = %+v", messages)
	}
	prompt := messages[0].Content
	if !strings.Contains(prompt, "Informazioni sulla persona riconosciuta dalla voce:") || !strings.Contains(prompt, "Dagli del tu.") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestGroupVoiceBecomesTheTurnTTSVoice(t *testing.T) {
	voice := "Serena"
	session := &ChatSession{clientState: &data_client.ClientState{
		DeviceConfig: types.UConfig{
			Tts: types.TtsConfig{
				Provider: "aliyun_qwen",
				Config:   map[string]interface{}{"voice": "Cherry", "model": "qwen3-tts-flash"},
			},
			VoiceIdentify: map[string]types.SpeakerGroupInfo{
				"Matteo": {Name: "Matteo", Voice: &voice},
			},
		},
	}}

	if err := session.switchTTSForSpeaker(&speaker.IdentifyResult{Identified: true, SpeakerName: "Matteo"}); err != nil {
		t.Fatal(err)
	}
	got := session.clientState.SpeakerTTSConfig
	if got["voice"] != "Serena" || got["provider"] != "aliyun_qwen" || got["model"] != "qwen3-tts-flash" {
		t.Fatalf("turn voice = %#v", got)
	}
	if session.clientState.DeviceConfig.Tts.Config["voice"] != "Cherry" {
		t.Fatal("the agent voice was overwritten")
	}
}
