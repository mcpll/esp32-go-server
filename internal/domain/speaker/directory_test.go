package speaker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// voiceServer is a small stand-in for hackers365/asr_server: it stores the
// enrolment the client sends and recognises that same audio later. It does
// not run sherpa; matching bytes stand in for a stored embedding.
type voiceServer struct {
	mu       sync.Mutex
	samples  []enrolledSample
	requests []string
}

type enrolledSample struct {
	uid         string
	agentID     string
	speakerID   string
	speakerName string
	uuid        string
	audio       []byte
}

func newVoiceServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := &voiceServer{}
	ts := httptest.NewServer(http.HandlerFunc(srv.serve))
	t.Cleanup(ts.Close)
	return ts
}

func (v *voiceServer) serve(w http.ResponseWriter, r *http.Request) {
	v.mu.Lock()
	v.requests = append(v.requests, r.Method+" "+r.URL.RequestURI())
	v.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/speaker/register":
		v.register(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/speaker/identify":
		v.identify(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/speaker":
		v.deleteSample(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/speaker/"):
		v.deleteSpeaker(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (v *voiceServer) register(w http.ResponseWriter, r *http.Request) {
	uid := firstNonEmpty(r.Header.Get("X-User-ID"), r.URL.Query().Get("uid"))
	agentID := firstNonEmpty(r.Header.Get("X-Agent-ID"), r.URL.Query().Get("agent_id"))
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
	if uid == "" {
		uid = fields["uid"]
	}
	if agentID == "" {
		agentID = fields["agent_id"]
	}
	if uid == "" || agentID == "" || fields["speaker_id"] == "" || fields["speaker_name"] == "" || fields["uuid"] == "" || len(audio) == 0 || !strings.HasSuffix(strings.ToLower(filename), ".wav") {
		http.Error(w, `{"error":"uid, agent_id, speaker_id, speaker_name, uuid and a wav audio file are required"}`, http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	v.samples = append(v.samples, enrolledSample{
		uid: uid, agentID: agentID, speakerID: fields["speaker_id"], speakerName: fields["speaker_name"], uuid: fields["uuid"], audio: audio,
	})
	v.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"message":"Speaker registered successfully","uuid":"` + fields["uuid"] + `","speaker_id":"` + fields["speaker_id"] + `","speaker_name":"` + fields["speaker_name"] + `"}`))
}

func (v *voiceServer) identify(w http.ResponseWriter, r *http.Request) {
	uid := firstNonEmpty(r.Header.Get("X-User-ID"), r.FormValue("uid"))
	agentID := firstNonEmpty(r.Header.Get("X-Agent-ID"), r.URL.Query().Get("agent_id"), r.FormValue("agent_id"))
	file, header, err := r.FormFile("audio")
	if err != nil || header == nil || !strings.HasSuffix(strings.ToLower(header.Filename), ".wav") {
		http.Error(w, `{"error":"audio file is required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()
	audio, _ := io.ReadAll(file)
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, sample := range v.samples {
		if agentID != "" && sample.agentID != agentID {
			continue
		}
		if uid != "" && sample.uid != uid {
			continue
		}
		if bytes.Equal(sample.audio, audio) {
			_, _ = w.Write([]byte(`{"identified":true,"speaker_id":"` + sample.speakerID + `","speaker_name":"` + sample.speakerName + `","confidence":0.91,"threshold":0.6}`))
			return
		}
	}
	_, _ = w.Write([]byte(`{"identified":false,"speaker_id":"","speaker_name":"","confidence":0.1,"threshold":0.6}`))
}

func (v *voiceServer) deleteSample(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")
	id := r.URL.Query().Get("uuid")
	if uid == "" || id == "" {
		http.Error(w, `{"error":"uid and uuid are required"}`, http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	kept := v.samples[:0]
	for _, sample := range v.samples {
		if sample.uuid == id && sample.uid == uid && (agentID == "" || sample.agentID == agentID) {
			continue
		}
		kept = append(kept, sample)
	}
	v.samples = kept
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"Speaker deleted successfully"}`))
}

func (v *voiceServer) deleteSpeaker(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")
	speakerID := strings.TrimPrefix(r.URL.Path, "/api/v1/speaker/")
	speakerID, _ = url.PathUnescape(speakerID)
	if uid == "" || speakerID == "" {
		http.Error(w, `{"error":"uid and speaker_id are required"}`, http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	kept := v.samples[:0]
	for _, sample := range v.samples {
		if sample.speakerID == speakerID && sample.uid == uid && (agentID == "" || sample.agentID == agentID) {
			continue
		}
		kept = append(kept, sample)
	}
	v.samples = kept
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"Speaker deleted successfully"}`))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func TestEnrolledVoiceIsRecognisedAndDeletingTheSampleRemovesIt(t *testing.T) {
	voice := newVoiceServer(t)
	ctx := context.Background()
	wav := []byte("RIFF-matteo-voice")
	id, err := Register(ctx, Enrollment{
		BaseURL:     voice.URL,
		UID:         "owner1",
		AgentID:     "agent1",
		SpeakerID:   "group1",
		SpeakerName: "Matteo",
		UUID:        "11111111-1111-4111-8111-111111111111",
		Filename:    "matteo.wav",
		Wav:         wav,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("uuid = %q", id)
	}

	got, err := Identify(ctx, voice.URL, "owner1", "agent1", "clip.wav", wav)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if !got.Identified || got.SpeakerName != "Matteo" || got.SpeakerID != "group1" {
		t.Fatalf("enrolled voice = %+v, want Matteo/group1", got)
	}

	other, err := Identify(ctx, voice.URL, "owner1", "agent1", "clip.wav", []byte("RIFF-stranger"))
	if err != nil {
		t.Fatalf("Identify stranger: %v", err)
	}
	if other.Identified {
		t.Fatalf("stranger was recognised: %+v", other)
	}

	if err := DeleteSample(ctx, voice.URL, "owner1", "agent1", id); err != nil {
		t.Fatalf("DeleteSample: %v", err)
	}
	gone, err := Identify(ctx, voice.URL, "owner1", "agent1", "clip.wav", wav)
	if err != nil {
		t.Fatalf("Identify after delete: %v", err)
	}
	if gone.Identified {
		t.Fatalf("deleted sample still recognised: %+v", gone)
	}
}

func TestDeletingASpeakerRemovesEverySampleOfThatGroup(t *testing.T) {
	voice := newVoiceServer(t)
	ctx := context.Background()
	for _, sample := range []Enrollment{
		{BaseURL: voice.URL, UID: "owner1", AgentID: "agent1", SpeakerID: "group1", SpeakerName: "Matteo", UUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Filename: "a.wav", Wav: []byte("wav-a")},
		{BaseURL: voice.URL, UID: "owner1", AgentID: "agent1", SpeakerID: "group1", SpeakerName: "Matteo", UUID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Filename: "b.wav", Wav: []byte("wav-b")},
	} {
		if _, err := Register(ctx, sample); err != nil {
			t.Fatalf("Register %s: %v", sample.UUID, err)
		}
	}
	if err := DeleteSpeaker(ctx, voice.URL, "owner1", "agent1", "group1"); err != nil {
		t.Fatalf("DeleteSpeaker: %v", err)
	}
	for _, wav := range [][]byte{[]byte("wav-a"), []byte("wav-b")} {
		got, err := Identify(ctx, voice.URL, "owner1", "agent1", "clip.wav", wav)
		if err != nil {
			t.Fatal(err)
		}
		if got.Identified {
			t.Fatalf("group sample still recognised: %+v", got)
		}
	}
}

func TestVoiceServerIsUnreachableWhenHealthFails(t *testing.T) {
	voice := newVoiceServer(t)
	if !Reachable(context.Background(), voice.URL) {
		t.Fatal("health endpoint should count as reachable")
	}
	if Reachable(context.Background(), "http://127.0.0.1:1") {
		t.Fatal("closed port counted as reachable")
	}
}

func TestIdentifyWebSocketPathFollowsTheVoiceServer(t *testing.T) {
	// asr_server registers GET /api/v1/speaker/identify_ws. The markdown doc's
	// /api/v1/speaker/stream is not that route; the client follows the service.
	if got := deriveWebSocketURL("https://voice.example:8080/ignored"); got != "wss://voice.example:8080/api/v1/speaker/identify_ws" {
		t.Fatalf("websocket url = %s", got)
	}
}
