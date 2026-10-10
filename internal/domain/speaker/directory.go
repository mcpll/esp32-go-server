package speaker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// voiceHTTP talks to the external voice server. Enrolment and deletion use the
// HTTP routes asr_server registers; live turns use the WebSocket client.
var voiceHTTP = &http.Client{Timeout: 30 * time.Second}

// Enrollment is one wav sent to POST /api/v1/speaker/register.
// The voice server stores the uuid the caller supplies and echoes it back.
type Enrollment struct {
	BaseURL     string
	UID         string
	AgentID     string
	SpeakerID   string
	SpeakerName string
	UUID        string
	Filename    string
	Wav         []byte
}

// Register stores a speaker sample on the voice server and returns the uuid it kept.
func Register(ctx context.Context, in Enrollment) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := [][2]string{
		{"speaker_id", in.SpeakerID},
		{"speaker_name", in.SpeakerName},
		{"uuid", in.UUID},
		{"agent_id", in.AgentID},
		{"uid", in.UID},
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return "", err
		}
	}
	filename := in.Filename
	if filename == "" {
		filename = "sample.wav"
	}
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(in.Wav); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceURL(in.BaseURL, "/api/v1/speaker/register"), &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	setSpeakerHeaders(req, in.UID, in.AgentID)

	raw, err := doVoice(req)
	if err != nil {
		return "", err
	}
	var out struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("voice server register: %w", err)
	}
	if out.UUID == "" {
		return "", fmt.Errorf("voice server register returned no uuid")
	}
	return out.UUID, nil
}

// Identify asks POST /api/v1/speaker/identify who the wav belongs to.
// The live turn uses the WebSocket; this is the same embedding lookup.
func Identify(ctx context.Context, baseURL, uid, agentID, filename string, wav []byte) (*IdentifyResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename == "" {
		filename = "clip.wav"
	}
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(wav); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceURL(baseURL, "/api/v1/speaker/identify"), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	setSpeakerHeaders(req, uid, agentID)

	raw, err := doVoice(req)
	if err != nil {
		return nil, err
	}
	var result IdentifyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("voice server identify: %w", err)
	}
	return &result, nil
}

// DeleteSample removes one embedding. The voice server route is
// DELETE /api/v1/speaker?uuid=...
func DeleteSample(ctx context.Context, baseURL, uid, agentID, uuid string) error {
	target := serviceURL(baseURL, "/api/v1/speaker") + "?uuid=" + url.QueryEscape(uuid)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return err
	}
	setSpeakerHeaders(req, uid, agentID)
	_, err = doVoice(req)
	return err
}

// DeleteSpeaker removes every embedding stored under speakerID (the group).
func DeleteSpeaker(ctx context.Context, baseURL, uid, agentID, speakerID string) error {
	target := serviceURL(baseURL, "/api/v1/speaker/"+url.PathEscape(speakerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return err
	}
	setSpeakerHeaders(req, uid, agentID)
	_, err = doVoice(req)
	return err
}

// Reachable reports whether GET {base}/health answers. Voiceprint stays off
// when this is false.
func Reachable(ctx context.Context, baseURL string) bool {
	if strings.TrimSpace(baseURL) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL(baseURL, "/health"), nil)
	if err != nil {
		return false
	}
	resp, err := voiceHTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func setSpeakerHeaders(req *http.Request, uid, agentID string) {
	if uid != "" {
		req.Header.Set("X-User-ID", uid)
	}
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
}

func serviceURL(baseURL, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path
}

func doVoice(req *http.Request) ([]byte, error) {
	resp, err := voiceHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice server: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &body) == nil && body.Error != "" {
			msg = body.Error
		}
		return nil, fmt.Errorf("voice server HTTP %d: %s", resp.StatusCode, msg)
	}
	return raw, nil
}
