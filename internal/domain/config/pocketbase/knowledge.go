package pocketbase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/config/store"
)

const ragflowPageSize = 100

// KnowledgeUpload sends one document file to the provider dataset and records the external id.
// Parsing is started there. The document stays pending until knowledge_sync sees it finished.
func (p *Provider) KnowledgeUpload(ctx context.Context, payload map[string]any) (any, error) {
	docID := strings.TrimSpace(stringField(payload["document"]))
	if docID == "" {
		return nil, fmt.Errorf("document is required")
	}
	doc, err := p.client.First(ctx, knowledgeDocumentsCollection, textEquals("id", docID), "")
	if err != nil {
		return nil, fmt.Errorf("document %s: %w", docID, err)
	}
	kbID := strings.TrimSpace(doc.String("knowledge_base"))
	if kbID == "" {
		return nil, fmt.Errorf("document %s has no knowledge base", docID)
	}
	kb, err := p.client.First(ctx, knowledgeBasesCollection, textEquals("id", kbID), "")
	if err != nil {
		return nil, fmt.Errorf("knowledge base %s: %w", kbID, err)
	}
	provider := strings.ToLower(strings.TrimSpace(kb.String("provider")))
	if provider != "ragflow" {
		return nil, fmt.Errorf("provider %q cannot accept a document upload", provider)
	}
	name := strings.TrimSpace(doc.String("file"))
	if name == "" {
		p.markDocument(ctx, docID, documentError)
		return nil, fmt.Errorf("document %s has no file", docID)
	}
	file, err := p.client.File(ctx, knowledgeDocumentsCollection, docID, name)
	if err != nil {
		p.markDocument(ctx, docID, documentError)
		return nil, fmt.Errorf("document %s file: %w", docID, err)
	}
	cfg, err := loadRagflowSettings()
	if err != nil {
		p.markDocument(ctx, docID, documentError)
		return nil, err
	}
	datasetID := strings.TrimSpace(kb.String("external_kb_id"))
	if datasetID == "" {
		datasetID, err = createRagflowDataset(ctx, cfg, kb)
		if err != nil {
			p.markDocument(ctx, docID, documentError)
			return nil, err
		}
		if err := p.client.Update(ctx, knowledgeBasesCollection, kb.String("id"), map[string]any{"external_kb_id": datasetID}); err != nil {
			return nil, err
		}
	}
	externalID, err := uploadRagflowDocument(ctx, cfg, datasetID, name, file)
	if err != nil {
		p.markDocument(ctx, docID, documentError)
		return nil, err
	}
	if err := parseRagflowDocuments(ctx, cfg, datasetID, externalID); err != nil {
		_ = p.client.Update(ctx, knowledgeDocumentsCollection, docID, map[string]any{
			"external_doc_id": externalID,
			"status":          documentError,
		})
		return nil, err
	}
	if err := p.client.Update(ctx, knowledgeDocumentsCollection, docID, map[string]any{
		"external_doc_id": externalID,
		"status":          documentPending,
	}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (p *Provider) markDocument(ctx context.Context, id, status string) {
	_ = p.client.Update(ctx, knowledgeDocumentsCollection, id, map[string]any{"status": status})
}

// KnowledgeSync refreshes each document of one knowledge base from its provider.
// A document that has not been uploaded stays as it is.
func (p *Provider) KnowledgeSync(ctx context.Context, payload map[string]any) (any, error) {
	kbID := strings.TrimSpace(stringField(payload["knowledge_base"]))
	if kbID == "" {
		return nil, fmt.Errorf("knowledge_base is required")
	}
	kb, err := p.client.First(ctx, knowledgeBasesCollection, textEquals("id", kbID), "")
	if err != nil {
		return nil, fmt.Errorf("knowledge base %s: %w", kbID, err)
	}
	docs, err := p.client.List(ctx, knowledgeDocumentsCollection, textEquals("knowledge_base", kbID), "")
	if err != nil {
		return nil, err
	}
	runs, err := p.providerRuns(ctx, kb, docs)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		status := doc.String("status")
		if status == "" {
			status = documentPending
		}
		if ext := strings.TrimSpace(doc.String("external_doc_id")); ext != "" {
			run, ok := runs[ext]
			if !ok {
				status = documentError
			} else {
				status = ragflowRunStatus(run)
			}
			if err := p.client.Update(ctx, knowledgeDocumentsCollection, doc.String("id"), map[string]any{"status": status}); err != nil {
				return nil, err
			}
		}
		items = append(items, map[string]any{"id": doc.String("id"), "status": status})
	}
	return map[string]any{"documents": items}, nil
}

const (
	documentPending = "pending"
	documentSynced  = "synced"
	documentError   = "error"
)

func (p *Provider) providerRuns(ctx context.Context, kb Record, docs []Record) (map[string]string, error) {
	need := false
	for _, doc := range docs {
		if strings.TrimSpace(doc.String("external_doc_id")) != "" {
			need = true
			break
		}
	}
	if !need {
		return map[string]string{}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(kb.String("provider")))
	if provider != "ragflow" {
		return nil, fmt.Errorf("provider %q cannot refresh document status", provider)
	}
	datasetID := strings.TrimSpace(kb.String("external_kb_id"))
	if datasetID == "" {
		return nil, fmt.Errorf("knowledge base %s has no external id", kb.String("id"))
	}
	cfg, err := loadRagflowSettings()
	if err != nil {
		return nil, err
	}
	return ragflowDocumentRuns(ctx, cfg, datasetID)
}

type ragflowConfig struct {
	baseURL string
	apiKey  string
}

func loadRagflowSettings() (ragflowConfig, error) {
	cfg := store.GetStringMap("knowledge.providers.ragflow")
	base := configString(cfg, "base_url")
	key := configString(cfg, "api_key")
	if base == "" || key == "" {
		// Viper sometimes keeps the provider object under the parent key.
		parent := store.GetStringMap("knowledge.providers")
		if nested, ok := parent["ragflow"].(map[string]any); ok {
			if base == "" {
				base = configString(nested, "base_url")
			}
			if key == "" {
				key = configString(nested, "api_key")
			}
		}
	}
	if base == "" {
		return ragflowConfig{}, fmt.Errorf("ragflow base_url is empty")
	}
	if key == "" {
		return ragflowConfig{}, fmt.Errorf("ragflow api_key is empty")
	}
	return ragflowConfig{baseURL: base, apiKey: key}, nil
}

func configString(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func createRagflowDataset(ctx context.Context, cfg ragflowConfig, kb Record) (string, error) {
	name := strings.TrimSpace(kb.String("name"))
	name = strings.ReplaceAll(strings.ReplaceAll(name, "\n", " "), "\r", " ")
	if name == "" {
		name = "knowledge-base"
	}
	settings := store.GetStringMap("knowledge.providers.ragflow")
	permission := configString(settings, "dataset_permission")
	if permission == "" {
		permission = "me"
	}
	chunkMethod := configString(settings, "dataset_chunk_method")
	if chunkMethod == "" {
		chunkMethod = "naive"
	}
	body, err := ragflowRequest(ctx, http.MethodPost, ragflowURL(cfg.baseURL, "/datasets"), cfg.apiKey, map[string]any{
		"name":         name,
		"description":  strings.TrimSpace(kb.String("description")),
		"permission":   permission,
		"chunk_method": chunkMethod,
	}, "")
	if err != nil {
		return "", fmt.Errorf("create ragflow dataset: %w", err)
	}
	id, err := ragflowObjectID(body)
	if err != nil {
		return "", fmt.Errorf("create ragflow dataset: %w", err)
	}
	return id, nil
}

func uploadRagflowDocument(ctx context.Context, cfg ragflowConfig, datasetID, fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	endpoint := ragflowURL(cfg.baseURL, "/datasets/"+url.PathEscape(datasetID)+"/documents")
	body, err := ragflowRaw(ctx, http.MethodPost, endpoint, cfg.apiKey, &buf, writer.FormDataContentType())
	if err != nil {
		return "", fmt.Errorf("upload ragflow document: %w", err)
	}
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("upload ragflow document: %w", err)
	}
	if len(envelope.Data) == 0 || strings.TrimSpace(envelope.Data[0].ID) == "" {
		return "", fmt.Errorf("upload ragflow document: response has no document id")
	}
	return strings.TrimSpace(envelope.Data[0].ID), nil
}

func parseRagflowDocuments(ctx context.Context, cfg ragflowConfig, datasetID, documentID string) error {
	endpoint := ragflowURL(cfg.baseURL, "/datasets/"+url.PathEscape(datasetID)+"/chunks")
	_, err := ragflowRequest(ctx, http.MethodPost, endpoint, cfg.apiKey, map[string]any{
		"document_ids": []string{documentID},
	}, "")
	if err != nil {
		return fmt.Errorf("parse ragflow document: %w", err)
	}
	return nil
}

func ragflowObjectID(body []byte) (string, error) {
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", err
	}
	id := strings.TrimSpace(envelope.Data.ID)
	if id == "" {
		return "", fmt.Errorf("response has no id")
	}
	return id, nil
}

func ragflowDocumentRuns(ctx context.Context, cfg ragflowConfig, datasetID string) (map[string]string, error) {
	runs := map[string]string{}
	for page := 1; ; page++ {
		endpoint := ragflowURL(cfg.baseURL, "/datasets/"+url.PathEscape(datasetID)+"/documents")
		endpoint += "?page=" + strconv.Itoa(page) + "&page_size=" + strconv.Itoa(ragflowPageSize)
		body, err := ragflowRequest(ctx, http.MethodGet, endpoint, cfg.apiKey, nil, "")
		if err != nil {
			return nil, err
		}
		docs, total, err := parseRagflowDocs(body)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			id := strings.TrimSpace(doc.ID)
			if id == "" {
				continue
			}
			runs[id] = runString(doc.Run)
		}
		if len(docs) == 0 || page*ragflowPageSize >= total {
			return runs, nil
		}
	}
}

func ragflowRunStatus(run string) string {
	switch strings.ToUpper(strings.TrimSpace(run)) {
	case "DONE", "3":
		return documentSynced
	case "FAIL", "FAILED", "4", "CANCEL", "CANCELLED", "CANCELED", "2":
		return documentError
	default:
		return documentPending
	}
}

func ragflowURL(baseURL, path string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(lower, "/api/v1"):
		return trimmed + path
	case strings.HasSuffix(lower, "/api"):
		return trimmed + "/v1" + path
	default:
		return trimmed + "/api/v1" + path
	}
}

func ragflowRequest(ctx context.Context, method, endpoint, apiKey string, payload any, contentType string) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
		if contentType == "" {
			contentType = "application/json"
		}
	}
	return ragflowRaw(ctx, method, endpoint, apiKey, reader, contentType)
}

func ragflowRaw(ctx context.Context, method, endpoint, apiKey string, reader io.Reader, contentType string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ragflow: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ragflow: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Code != 0 {
		return nil, fmt.Errorf("ragflow: code=%d %s", envelope.Code, strings.TrimSpace(envelope.Message))
	}
	return body, nil
}

type ragflowListedDoc struct {
	ID  string          `json:"id"`
	Run json.RawMessage `json:"run"`
}

func parseRagflowDocs(body []byte) ([]ragflowListedDoc, int, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, 0, fmt.Errorf("ragflow document list: %w", err)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || string(data) == "null" {
		return nil, 0, nil
	}
	if data[0] == '{' {
		var wrapped struct {
			Docs  []ragflowListedDoc `json:"docs"`
			Total int                `json:"total"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return nil, 0, fmt.Errorf("ragflow document list: %w", err)
		}
		total := wrapped.Total
		if total == 0 {
			total = len(wrapped.Docs)
		}
		return wrapped.Docs, total, nil
	}
	var docs []ragflowListedDoc
	if err := json.Unmarshal(data, &docs); err != nil {
		return nil, 0, fmt.Errorf("ragflow document list: %w", err)
	}
	return docs, len(docs), nil
}

func runString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return strconv.Itoa(int(number))
	}
	return ""
}
