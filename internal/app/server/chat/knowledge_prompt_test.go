package chat

import (
	"strings"
	"testing"

	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
)

func TestKnowledgeRoutingPromptIsItalian(t *testing.T) {
	got := buildKnowledgeSearchRoutingPolicy([]config_types.KnowledgeBaseRef{{
		ID:           7,
		Name:         "Cancello",
		Description:  "codici di accesso",
		ExternalKBID: "dataset-cancello",
		Status:       "active",
	}})
	for _, phrase := range []string{
		"Regole di ricerca (strumento: search_knowledge):",
		"Basi disponibili (id: nome + descrizione): 7: nome=Cancello; descrizione=codici di accesso",
		"non inventare",
		"knowledge_base_ids",
		"top_k è 5",
	} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("missing %q\n%s", phrase, got)
		}
	}
	for _, r := range got {
		if r >= 0x4e00 && r <= 0x9fff {
			t.Fatalf("prompt still contains Chinese:\n%s", got)
		}
	}

	blank := buildKnowledgeSearchRoutingPolicy([]config_types.KnowledgeBaseRef{{
		ID:           3,
		Name:         "Note",
		ExternalKBID: "dataset-note",
		Status:       "active",
	}})
	if !strings.Contains(blank, "3: nome=Note; descrizione=nessuna descrizione") {
		t.Fatalf("blank description prompt:\n%s", blank)
	}
}

func TestKnowledgeRoutingPromptSkipsUnusableBases(t *testing.T) {
	if got := buildKnowledgeSearchRoutingPolicy(nil); got != "" {
		t.Fatalf("nil bases: %q", got)
	}
	if got := buildKnowledgeSearchRoutingPolicy([]config_types.KnowledgeBaseRef{{
		ID: 7, Name: "Cancello", Status: "active",
	}}); got != "" {
		t.Fatalf("missing external id: %q", got)
	}
	if got := buildKnowledgeSearchRoutingPolicy([]config_types.KnowledgeBaseRef{{
		ID: 7, Name: "Cancello", ExternalKBID: "dataset-cancello", Status: "inactive",
	}}); got != "" {
		t.Fatalf("inactive base: %q", got)
	}
}
