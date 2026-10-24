package pocketbase

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"

	"github.com/spf13/viper"
)

func TestVoiceprintIsOffUnlessSystemServiceAgentAndAGroupArePresent(t *testing.T) {
	voice := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(voice.Close)

	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()
	agent := seedItalianAgent(fake)
	agentID := agent["id"].(string)
	fake.patch("agents", agentID, map[string]any{"voiceprint_enabled": true})
	group := fake.seed("speaker_groups", map[string]any{
		"agent":  agentID,
		"name":   "Matteo",
		"prompt": "Dagli del tu.",
		"voice":  "Serena",
	})
	fake.seed("speaker_samples", map[string]any{
		"group":  group["id"],
		"file":   "matteo.wav",
		"uuid":   "11111111-1111-4111-8111-111111111111",
		"status": "enrolled",
	})
	fake.seed("speaker_samples", map[string]any{
		"group":  group["id"],
		"file":   "later.wav",
		"uuid":   "22222222-2222-4222-8222-222222222222",
		"status": "pending",
	})
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agentID)

	viper.Set("voice_identify.enable", true)
	viper.Set("voice_identify.base_url", voice.URL)
	cfg := mustUserConfig(t, p)
	info, ok := cfg.VoiceIdentify["Matteo"]
	if !ok || info.Prompt != "Dagli del tu." || info.Voice == nil || *info.Voice != "Serena" {
		t.Fatalf("group = %+v", cfg.VoiceIdentify)
	}
	if len(info.Uuids) != 1 || info.Uuids[0] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("uuids = %#v", info.Uuids)
	}

	viper.Set("voice_identify.enable", false)
	if got := mustUserConfig(t, p); len(got.VoiceIdentify) != 0 {
		t.Fatalf("system switch off still filled %+v", got.VoiceIdentify)
	}

	viper.Set("voice_identify.enable", true)
	viper.Set("voice_identify.base_url", "http://127.0.0.1:1")
	if got := mustUserConfig(t, p); len(got.VoiceIdentify) != 0 {
		t.Fatalf("unreachable service still filled %+v", got.VoiceIdentify)
	}

	viper.Set("voice_identify.base_url", voice.URL)
	fake.patch("agents", agentID, map[string]any{"voiceprint_enabled": false})
	if got := mustUserConfig(t, p); len(got.VoiceIdentify) != 0 {
		t.Fatalf("agent switch off still filled %+v", got.VoiceIdentify)
	}

	fake.patch("agents", agentID, map[string]any{"voiceprint_enabled": true})
	fake.patch("speaker_groups", group["id"].(string), map[string]any{"agent": "someone-else"})
	if got := mustUserConfig(t, p); len(got.VoiceIdentify) != 0 {
		t.Fatalf("no group still filled %+v", got.VoiceIdentify)
	}
}

func TestEnrolledSampleIsRecognisedUntilTheSampleIsDeleted(t *testing.T) {
	voice := newEnrolmentServer(t)
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	viper.Set("voice_identify.enable", true)
	viper.Set("voice_identify.base_url", voice.URL())
	owner := fake.seed("users", map[string]any{"email": "owner@example.com"})
	agent := seedItalianAgent(fake)
	agentID := agent["id"].(string)
	fake.patch("agents", agentID, map[string]any{"voiceprint_enabled": true})
	group := fake.seed("speaker_groups", map[string]any{
		"agent": agentID, "name": "Matteo", "prompt": "Dagli del tu.", "voice": "Serena",
	})
	wav := []byte("RIFF-matteo-voice")
	sample := fake.seed("speaker_samples", map[string]any{
		"group": group["id"], "file": "matteo.wav", "status": "pending",
	})
	fake.storeFile("speaker_samples", sample["id"].(string), "matteo.wav", wav)
	enrollID := seedPending(fake, "speaker_enroll", map[string]any{"sample": sample["id"]}, time.Now().UTC())

	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()

	var enrolledUUID string
	waitFor(t, func() bool {
		cur := fake.find("commands", "id", enrollID)
		if cur == nil || cur["status"] != "done" {
			return false
		}
		result, _ := cur["result"].(map[string]any)
		enrolledUUID, _ = result["uuid"].(string)
		return enrolledUUID != ""
	})
	stored := fake.find("speaker_samples", "id", sample["id"])
	if stored["uuid"] != enrolledUUID || stored["status"] != "enrolled" {
		t.Fatalf("sample after enrol = %#v", stored)
	}

	got, err := speaker.Identify(ctx, voice.URL(), owner["id"].(string), agentID, "clip.wav", wav)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if !got.Identified || got.SpeakerName != "Matteo" || got.SpeakerID != group["id"] {
		t.Fatalf("enrolled voice = %+v", got)
	}

	deleteID := seedPending(fake, "speaker_delete", map[string]any{"sample": sample["id"]}, time.Now().UTC())
	fake.publish("commands/"+deleteID, `{"action":"create","record":{}}`)
	waitFor(t, func() bool {
		cur := fake.find("commands", "id", deleteID)
		if cur == nil || cur["status"] != "done" {
			return false
		}
		result, _ := cur["result"].(map[string]any)
		return result["ok"] == true
	})
	if fake.find("speaker_samples", "id", sample["id"]) != nil {
		t.Fatal("deleted sample record is still there")
	}
	gone, err := speaker.Identify(ctx, voice.URL(), owner["id"].(string), agentID, "clip.wav", wav)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Identified {
		t.Fatalf("deleted embedding still recognised: %+v", gone)
	}
}

func TestDeletingAGroupRemovesEveryEmbedding(t *testing.T) {
	voice := newEnrolmentServer(t)
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	viper.Set("voice_identify.enable", true)
	viper.Set("voice_identify.base_url", voice.URL())
	owner := fake.seed("users", map[string]any{"email": "owner@example.com"})
	agent := seedItalianAgent(fake)
	agentID := agent["id"].(string)
	group := fake.seed("speaker_groups", map[string]any{
		"agent": agentID, "name": "Matteo", "prompt": "Dagli del tu.", "voice": "Serena",
	})
	groupID := group["id"].(string)
	wavs := [][]byte{[]byte("RIFF-matteo-a"), []byte("RIFF-matteo-b")}
	var sampleIDs []string
	for i, wav := range wavs {
		name := fmt.Sprintf("take-%d.wav", i)
		sample := fake.seed("speaker_samples", map[string]any{
			"group": groupID, "file": name, "status": "pending",
		})
		sampleIDs = append(sampleIDs, sample["id"].(string))
		fake.storeFile("speaker_samples", sample["id"].(string), name, wav)
		seedPending(fake, "speaker_enroll", map[string]any{"sample": sample["id"]}, time.Now().UTC())
	}

	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()
	waitFor(t, func() bool {
		for _, id := range sampleIDs {
			stored := fake.find("speaker_samples", "id", id)
			if stored == nil || stored["status"] != "enrolled" || stored["uuid"] == "" {
				return false
			}
		}
		return true
	})
	for _, wav := range wavs {
		got, err := speaker.Identify(ctx, voice.URL(), owner["id"].(string), agentID, "clip.wav", wav)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Identified || got.SpeakerName != "Matteo" {
			t.Fatalf("enrolled voice = %+v", got)
		}
	}

	deleteID := seedPending(fake, "speaker_delete", map[string]any{"group": groupID}, time.Now().UTC())
	fake.publish("commands/"+deleteID, `{"action":"create","record":{}}`)
	waitFor(t, func() bool {
		cur := fake.find("commands", "id", deleteID)
		if cur == nil || cur["status"] != "done" {
			return false
		}
		result, _ := cur["result"].(map[string]any)
		return result["ok"] == true
	})
	if fake.find("speaker_groups", "id", groupID) == nil {
		t.Fatal("group record was removed")
	}
	for i, id := range sampleIDs {
		stored := fake.find("speaker_samples", "id", id)
		if stored == nil || stored["uuid"] != "" || stored["status"] != "pending" {
			t.Fatalf("sample %d after group delete = %#v", i, stored)
		}
		gone, err := speaker.Identify(ctx, voice.URL(), owner["id"].(string), agentID, "clip.wav", wavs[i])
		if err != nil {
			t.Fatal(err)
		}
		if gone.Identified {
			t.Fatalf("group embedding still recognised: %+v", gone)
		}
	}
}

// enrolmentServer stores wav bytes the way the voice server stores embeddings.
type enrolmentServer struct {
	url     string
	mu      sync.Mutex
	samples []enrolment
}

type enrolment struct {
	uid, agentID, speakerID, speakerName, uuid string
	audio                                      []byte
}

func newEnrolmentServer(t *testing.T) *enrolmentServer {
	t.Helper()
	srv := &enrolmentServer{}
	ts := httptest.NewServer(http.HandlerFunc(srv.serve))
	t.Cleanup(ts.Close)
	srv.url = ts.URL
	return srv
}

func (s *enrolmentServer) URL() string { return s.url }

func (s *enrolmentServer) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/speaker/register":
		s.register(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/speaker/identify":
		s.identify(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/speaker":
		s.deleteUUID(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/speaker/"):
		s.deleteSpeaker(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *enrolmentServer) register(w http.ResponseWriter, r *http.Request) {
	form, err := r.MultipartReader()
	if err != nil {
		http.Error(w, `{"error":"audio file is required"}`, http.StatusBadRequest)
		return
	}
	fields := map[string]string{}
	var audio []byte
	var filename string
	for {
		part, err := form.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, `{"error":"bad multipart"}`, http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(part)
		if part.FileName() != "" {
			filename = part.FileName()
			audio = body
			continue
		}
		fields[part.FormName()] = string(body)
	}
	uid := firstHeader(r, "X-User-ID", fields["uid"])
	agentID := firstHeader(r, "X-Agent-ID", fields["agent_id"])
	if uid == "" || agentID == "" || fields["speaker_id"] == "" || fields["speaker_name"] == "" || fields["uuid"] == "" || !strings.HasSuffix(strings.ToLower(filename), ".wav") || len(audio) == 0 {
		http.Error(w, `{"error":"incomplete enrolment"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.samples = append(s.samples, enrolment{
		uid: uid, agentID: agentID, speakerID: fields["speaker_id"], speakerName: fields["speaker_name"], uuid: fields["uuid"], audio: append([]byte(nil), audio...),
	})
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"uuid":"` + fields["uuid"] + `","speaker_name":"` + fields["speaker_name"] + `"}`))
}

func (s *enrolmentServer) identify(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, `{"error":"audio file is required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()
	audio, _ := io.ReadAll(file)
	_ = header
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sample := range s.samples {
		if sample.uid == uid && sample.agentID == agentID && bytes.Equal(sample.audio, audio) {
			_, _ = w.Write([]byte(`{"identified":true,"speaker_id":"` + sample.speakerID + `","speaker_name":"` + sample.speakerName + `","confidence":0.91,"threshold":0.6}`))
			return
		}
	}
	_, _ = w.Write([]byte(`{"identified":false,"speaker_id":"","speaker_name":"","confidence":0,"threshold":0.6}`))
}

func (s *enrolmentServer) deleteUUID(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")
	id := r.URL.Query().Get("uuid")
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.samples[:0]
	for _, sample := range s.samples {
		if sample.uuid == id && sample.uid == uid && sample.agentID == agentID {
			continue
		}
		kept = append(kept, sample)
	}
	s.samples = kept
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"Speaker deleted successfully"}`))
}

func (s *enrolmentServer) deleteSpeaker(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")
	speakerID := strings.TrimPrefix(r.URL.Path, "/api/v1/speaker/")
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.samples[:0]
	for _, sample := range s.samples {
		if sample.speakerID == speakerID && sample.uid == uid && sample.agentID == agentID {
			continue
		}
		kept = append(kept, sample)
	}
	s.samples = kept
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"Speaker deleted successfully"}`))
}

func firstHeader(r *http.Request, header, fallback string) string {
	if value := r.Header.Get(header); value != "" {
		return value
	}
	return fallback
}

func mustUserConfig(t *testing.T, p *Provider) types.UConfig {
	t.Helper()
	cfg, err := p.GetUserConfig(context.Background(), testDevice)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	return cfg
}
