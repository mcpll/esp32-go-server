package pocketbase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/rag"

	"github.com/spf13/viper"
)

func TestKnowledgeStaysOffUnlessTheAgentSwitchIsOn(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	ctx := context.Background()

	kb := fake.seed("knowledge_bases", map[string]any{
		"name":                "Cancello",
		"description":         "codici di accesso",
		"provider":            "ragflow",
		"external_kb_id":      "dataset-cancello",
		"retrieval_threshold": 0.35,
		"status":              "active",
	})
	agent := seedItalianAgent(fake)
	agentID := agent["id"].(string)
	fake.patch("agents", agentID, map[string]any{
		"knowledge_enabled": false,
		"knowledge_bases":   []any{kb["id"]},
	})
	_, _, _, _ = p.GetActivationInfo(ctx, testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agentID)

	off, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if len(off.KnowledgeBases) != 0 {
		t.Fatalf("knowledge bases with the switch off: %+v", off.KnowledgeBases)
	}

	fake.patch("agents", agentID, map[string]any{"knowledge_enabled": true})
	on, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if len(on.KnowledgeBases) != 1 {
		t.Fatalf("knowledge bases with the switch on: %+v", on.KnowledgeBases)
	}
	got := on.KnowledgeBases[0]
	if got.Name != "Cancello" || got.Description != "codici di accesso" || got.Provider != "ragflow" || got.ExternalKBID != "dataset-cancello" || got.Status != "active" {
		t.Fatalf("base = %+v", got)
	}
	if got.RetrievalThreshold == nil || *got.RetrievalThreshold != 0.35 {
		t.Fatalf("threshold = %v", got.RetrievalThreshold)
	}
	if got.ID == 0 {
		t.Fatal("knowledge base id is 0, so the routing prompt drops the base")
	}
	again, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.KnowledgeBases) != 1 || again.KnowledgeBases[0].ID != got.ID {
		t.Fatalf("id changed from %d to %+v", got.ID, again.KnowledgeBases)
	}
}

func TestKnowledgeSyncRefreshesDocumentStatusFromTheProvider(t *testing.T) {
	rag := newFakeRagflow(t, "rag-secret")
	rag.seedDocument("dataset-cancello", "ext-remote", "1")
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	viper.Set("knowledge.providers.ragflow", map[string]any{
		"base_url": rag.URL(),
		"api_key":  "rag-secret",
	})
	p.RegisterCommand("knowledge_sync", p.KnowledgeSync)

	kb := fake.seed("knowledge_bases", map[string]any{
		"name":           "Cancello",
		"provider":       "ragflow",
		"external_kb_id": "dataset-cancello",
		"status":         "active",
	})
	remote := fake.seed("knowledge_documents", map[string]any{
		"knowledge_base":  kb["id"],
		"file":            "cancello.txt",
		"status":          "pending",
		"external_doc_id": "ext-remote",
	})
	local := fake.seed("knowledge_documents", map[string]any{
		"knowledge_base": kb["id"],
		"file":           "bozza.txt",
		"status":         "pending",
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()
	waitFor(t, func() bool { return fake.subscriptionCount() >= 1 })

	rag.setRun("ext-remote", "FAIL")
	failID := seedPending(fake, "knowledge_sync", map[string]any{"knowledge_base": kb["id"]}, time.Now().UTC())
	fake.publish("commands/"+failID, `{"action":"create"}`)
	waitFor(t, func() bool {
		doc := fake.find("knowledge_documents", "id", remote["id"])
		cmd := fake.find("commands", "id", failID)
		return doc["status"] == "error" && cmd["status"] == "done" &&
			documentStatus(cmd["result"], remote["id"].(string)) == "error" &&
			fake.find("knowledge_documents", "id", local["id"])["status"] == "pending"
	})

	rag.setRun("ext-remote", "DONE")
	doneID := seedPending(fake, "knowledge_sync", map[string]any{"knowledge_base": kb["id"]}, time.Now().UTC())
	fake.publish("commands/"+doneID, `{"action":"create"}`)
	waitFor(t, func() bool {
		doc := fake.find("knowledge_documents", "id", remote["id"])
		cmd := fake.find("commands", "id", doneID)
		return doc["status"] == "synced" && cmd["status"] == "done" &&
			documentStatus(cmd["result"], remote["id"].(string)) == "synced" &&
			fake.find("knowledge_documents", "id", local["id"])["status"] == "pending"
	})
}

func TestUploadedDocumentAnswersASpokenQuestion(t *testing.T) {
	const (
		documentText = "Il codice del cancello è 4821."
		question     = "qual è il codice del cancello?"
	)
	ragflow := newFakeRagflow(t, "rag-secret")
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	viper.Set("knowledge.providers.ragflow", map[string]any{
		"base_url":             ragflow.URL(),
		"api_key":              "rag-secret",
		"similarity_threshold": 0.2,
	})
	p.RegisterCommand("knowledge_upload", p.KnowledgeUpload)
	p.RegisterCommand("knowledge_sync", p.KnowledgeSync)

	kb := fake.seed("knowledge_bases", map[string]any{
		"name":        "Cancello",
		"description": "codici di accesso",
		"provider":    "ragflow",
		"status":      "active",
	})
	doc := fake.seed("knowledge_documents", map[string]any{
		"knowledge_base": kb["id"],
		"file":           "cancello.txt",
		"status":         "pending",
	})
	fake.seedFile("knowledge_documents", doc["id"].(string), "cancello.txt", []byte(documentText))
	agent := seedItalianAgent(fake)
	agentID := agent["id"].(string)
	fake.patch("agents", agentID, map[string]any{
		"knowledge_enabled": true,
		"knowledge_bases":   []any{kb["id"]},
	})
	_, _, _, _ = p.GetActivationInfo(context.Background(), testDevice, "c")
	bind(fake, fake.find("devices", "device_id", testDevice), agentID)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()
	waitFor(t, func() bool { return fake.subscriptionCount() >= 1 })

	uploadID := seedPending(fake, "knowledge_upload", map[string]any{"document": doc["id"]}, time.Now().UTC())
	fake.publish("commands/"+uploadID, `{"action":"create"}`)
	waitFor(t, func() bool {
		cmd := fake.find("commands", "id", uploadID)
		stored := fake.find("knowledge_documents", "id", doc["id"])
		base := fake.find("knowledge_bases", "id", kb["id"])
		ext, _ := stored["external_doc_id"].(string)
		dataset, _ := base["external_kb_id"].(string)
		return cmd["status"] == "done" && stored["status"] == "pending" && ext != "" && dataset != ""
	})

	stored := fake.find("knowledge_documents", "id", doc["id"])
	ragflow.setRun(stored["external_doc_id"].(string), "DONE")
	syncID := seedPending(fake, "knowledge_sync", map[string]any{"knowledge_base": kb["id"]}, time.Now().UTC())
	fake.publish("commands/"+syncID, `{"action":"create"}`)
	waitFor(t, func() bool {
		cur := fake.find("knowledge_documents", "id", doc["id"])
		return cur["status"] == "synced"
	})

	cfg, err := p.GetUserConfig(ctx, testDevice)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.KnowledgeBases) != 1 || cfg.KnowledgeBases[0].ExternalKBID == "" {
		t.Fatalf("session config = %+v", cfg.KnowledgeBases)
	}
	hits, err := rag.Search(ctx, question, 5, cfg.KnowledgeBases, []uint{cfg.KnowledgeBases[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Content != documentText {
		t.Fatalf("hits = %+v", hits)
	}
}

func documentStatus(result any, documentID string) string {
	body, _ := result.(map[string]any)
	items, _ := body["documents"].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		if row["id"] == documentID {
			status, _ := row["status"].(string)
			return status
		}
	}
	return ""
}

// fakeRagflow is the slice of RAGFlow the commands talk to.
type fakeRagflow struct {
	t      *testing.T
	server *httptest.Server
	apiKey string

	mu     sync.Mutex
	next   int
	docs   map[string]map[string]string // dataset id -> document id -> run
	files  map[string][]byte
	parsed map[string]bool
}

func newFakeRagflow(t *testing.T, apiKey string) *fakeRagflow {
	t.Helper()
	f := &fakeRagflow{
		t:      t,
		apiKey: apiKey,
		docs:   map[string]map[string]string{},
		files:  map[string][]byte{},
		parsed: map[string]bool{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeRagflow) URL() string { return f.server.URL }

func (f *fakeRagflow) seedDocument(datasetID, documentID, run string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.docs[datasetID] == nil {
		f.docs[datasetID] = map[string]string{}
	}
	f.docs[datasetID][documentID] = run
}

func (f *fakeRagflow) setRun(documentID, run string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, docs := range f.docs {
		if _, ok := docs[documentID]; ok {
			docs[documentID] = run
		}
	}
}

func (f *fakeRagflow) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.apiKey {
		http.Error(w, `{"code":401,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/api/v1/datasets" && r.Method == http.MethodPost:
		f.createDataset(w)
	case r.URL.Path == "/api/v1/retrieval" && r.Method == http.MethodPost:
		f.retrieve(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.HasSuffix(r.URL.Path, "/documents") && r.Method == http.MethodGet:
		f.listDocuments(w, datasetID(r.URL.Path, "/documents"))
	case strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.HasSuffix(r.URL.Path, "/documents") && r.Method == http.MethodPost:
		f.uploadDocument(w, r, datasetID(r.URL.Path, "/documents"))
	case strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.HasSuffix(r.URL.Path, "/chunks") && r.Method == http.MethodPost:
		f.parseDocuments(w, r, datasetID(r.URL.Path, "/chunks"))
	default:
		http.NotFound(w, r)
	}
}

func datasetID(path, suffix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/datasets/"), suffix)
}

func (f *fakeRagflow) createDataset(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("dataset-%d", f.next)
	f.docs[id] = map[string]string{}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id}})
}

func (f *fakeRagflow) listDocuments(w http.ResponseWriter, datasetID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var docs []map[string]any
	for id, run := range f.docs[datasetID] {
		docs = append(docs, map[string]any{"id": id, "run": run})
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"docs": docs, "total": len(docs)}})
}

func (f *fakeRagflow) uploadDocument(w http.ResponseWriter, r *http.Request, datasetID string) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.docs[datasetID] == nil {
		http.Error(w, `{"code":102,"message":"dataset not found"}`, http.StatusOK)
		return
	}
	f.next++
	id := fmt.Sprintf("ext-%d", f.next)
	f.docs[datasetID][id] = "UNSTART"
	f.files[id] = data
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0,
		"data": []any{map[string]any{"id": id, "name": header.Filename}},
	})
}

func (f *fakeRagflow) parseDocuments(w http.ResponseWriter, r *http.Request, datasetID string) {
	var body struct {
		DocumentIDs []string `json:"document_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range body.DocumentIDs {
		if _, ok := f.docs[datasetID][id]; ok {
			f.parsed[id] = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0})
}

func (f *fakeRagflow) retrieve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DatasetIDs []string `json:"dataset_ids"`
		Question   string   `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var chunks []map[string]any
	if strings.TrimSpace(body.Question) != "" {
		for _, datasetID := range body.DatasetIDs {
			for docID := range f.docs[datasetID] {
				if !f.parsed[docID] || len(f.files[docID]) == 0 {
					continue
				}
				chunks = append(chunks, map[string]any{
					"content":       string(f.files[docID]),
					"similarity":    0.91,
					"document_name": "cancello.txt",
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"chunks": chunks}})
}
