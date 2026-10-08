package providertest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestWrongAPIKeyIsTheVendorError(t *testing.T) {
	const vendor = "Incorrect API key provided: sk-wrong"
	vendorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-from-yaml" {
			t.Errorf("Authorization = %q, want the key from the server config", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"` + vendor + `"}}`))
	}))
	t.Cleanup(vendorServer.Close)

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("llm.openai", map[string]any{
		"api_key":    "sk-from-yaml",
		"type":       "openai",
		"model_name": "gpt-4o-mini",
	})

	_, err := FromCommand(context.Background(), map[string]any{
		"stage":    "llm",
		"provider": "openai",
		"config": map[string]any{
			"type":       "openai",
			"model_name": "gpt-4o-mini",
			"base_url":   vendorServer.URL + "/v1",
		},
	})
	if err == nil || !strings.Contains(err.Error(), vendor) {
		t.Fatalf("error = %v, want the vendor text %q", err, vendor)
	}
}
