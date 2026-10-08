package mem0

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestLongMemoryStoresAndRecallsWithTheMem0Key(t *testing.T) {
	const apiKey = "mem-secret"
	store := &mem0Fake{byAgent: map[string][]string{}}
	server := httptest.NewServer(store)
	t.Cleanup(server.Close)

	client, err := newMem0Client(map[string]interface{}{
		"api_key":          apiKey,
		"base_url":         server.URL,
		"enable_search":    true,
		"search_threshold": 0.5,
		"search_topk":      3,
	})
	if err != nil {
		t.Fatalf("newMem0Client: %v", err)
	}

	ctx := context.Background()
	message := schema.Message{Role: schema.User, Content: "Il gatto si chiama Nino"}
	if err := client.AddMessage(ctx, "agent-9", message); err != nil {
		t.Fatal(err)
	}
	recalled, err := client.Search(ctx, "agent-9", "gatto", 10, 180)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recalled, "Il gatto si chiama Nino") {
		t.Fatalf("recall = %q", recalled)
	}
	other, err := client.Search(ctx, "agent-other", "gatto", 10, 180)
	if err != nil {
		t.Fatal(err)
	}
	if other != "" {
		t.Fatalf("other agent recalled %q", other)
	}
	if store.auth != "Token "+apiKey {
		t.Fatalf("authorization = %q", store.auth)
	}
}

type mem0Fake struct {
	mu      sync.Mutex
	byAgent map[string][]string
	auth    string
}

func (f *mem0Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if auth := r.Header.Get("Authorization"); auth != "" {
		f.auth = auth
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/ping/":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","org_id":"org","project_id":"proj","user_email":"owner@example.com"}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/":
		body := decodeBody(r)
		agentID, _ := body["agent_id"].(string)
		for _, item := range messageContents(body["messages"]) {
			f.byAgent[agentID] = append(f.byAgent[agentID], item)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/search/":
		body := decodeBody(r)
		agentID, _ := body["agent_id"].(string)
		memories := make([]map[string]string, 0, len(f.byAgent[agentID]))
		for _, text := range f.byAgent[agentID] {
			memories = append(memories, map[string]string{"id": "m1", "memory": text, "agent_id": agentID})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(memories)
	default:
		http.NotFound(w, r)
	}
}

func decodeBody(r *http.Request) map[string]any {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

func messageContents(raw any) []string {
	items, _ := raw.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		msg, _ := item.(map[string]any)
		text, _ := msg["content"].(string)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
