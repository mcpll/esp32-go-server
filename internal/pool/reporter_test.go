package pool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/config/pocketbase"

	"github.com/spf13/viper"
)

func TestStatsReporterDisabledByDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	r := newStatsReporter()
	if r.enabled {
		t.Fatal("reporter must be disabled when pool_stats.report_enabled is unset")
	}
	r.StartReporting(context.Background()) // must not panic or start anything

	viper.Set("pool_stats.report_enabled", true)
	if !newStatsReporter().enabled {
		t.Fatal("reporter must honour pool_stats.report_enabled=true")
	}
}

func TestDisabledReporterSendsNothing(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	fake := newPoolStatsPB(t)
	r := newStatsReporter()
	r.client = pocketbase.NewClient(fake.url, "server@example.com", "superuser-pass-123")
	r.every = 5 * time.Millisecond
	r.stats = func() map[string]any {
		return map[string]any{
			"asr:cafebabe": map[string]any{"in_use_resources": 9, "available_resources": 0, "total_resources": 9},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.StartReporting(ctx)
	time.Sleep(40 * time.Millisecond)

	if got := fake.requestCount(); got != 0 {
		t.Fatalf("disabled reporter sent %d requests, want none", got)
	}
	if _, n, _ := fake.mainRecord(); n != 0 {
		t.Fatalf("disabled reporter wrote %d pool_stats records", n)
	}
}

func TestPoolStatsAreReportedEveryFiveSeconds(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	if got := newStatsReporter().interval(); got != 5*time.Second {
		t.Fatalf("interval = %s, want 5s", got)
	}
}

func TestEnabledReporterOverwritesTheSinglePoolStatsRecord(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("pool_stats.report_enabled", true)

	fake := newPoolStatsPB(t)
	var snapshot atomic.Value
	snapshot.Store(map[string]any{
		"asr:cafebabe": map[string]any{
			"total_resources":     4,
			"available_resources": 3,
			"in_use_resources":    1,
		},
	})

	r := newStatsReporter()
	r.client = pocketbase.NewClient(fake.url, "server@example.com", "superuser-pass-123")
	r.every = 15 * time.Millisecond
	r.stats = func() map[string]any {
		loaded, _ := snapshot.Load().(map[string]any)
		return loaded
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.StartReporting(ctx)

	waitForInUse := func(want float64) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			data, n, _ := fake.mainRecord()
			if n == 1 && poolField(data, "asr:cafebabe", "in_use_resources") == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		data, n, id := fake.mainRecord()
		t.Fatalf("pool_stats records=%d id=%q data=%#v, want 1 record with in_use %v", n, id, data, want)
	}

	waitForInUse(1)
	_, _, firstID := fake.mainRecord()

	snapshot.Store(map[string]any{
		"asr:cafebabe": map[string]any{
			"total_resources":     4,
			"available_resources": 1,
			"in_use_resources":    3,
		},
	})
	waitForInUse(3)

	data, n, id := fake.mainRecord()
	if n != 1 {
		t.Fatalf("want one pool_stats record, got %d", n)
	}
	if id == "" || id != firstID {
		t.Fatalf("record id changed from %q to %q", firstID, id)
	}
	if fake.recordKey() != "main" {
		t.Fatalf("key = %q, want main", fake.recordKey())
	}
	if poolField(data, "asr:cafebabe", "available_resources") != 1 {
		t.Fatalf("available = %v, want 1", poolField(data, "asr:cafebabe", "available_resources"))
	}
	if poolField(data, "asr:cafebabe", "total_resources") != 4 {
		t.Fatalf("total = %v, want 4", poolField(data, "asr:cafebabe", "total_resources"))
	}
}

func poolField(data map[string]any, poolKey, field string) float64 {
	row, _ := data[poolKey].(map[string]any)
	n, _ := row[field].(float64)
	return n
}

type poolStatsPB struct {
	url      string
	mu       sync.Mutex
	records  []map[string]any
	nextID   int
	requests atomic.Int64
}

func newPoolStatsPB(t *testing.T) *poolStatsPB {
	t.Helper()
	fake := &poolStatsPB{}
	server := httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(server.Close)
	fake.url = server.URL
	return fake
}

func (f *poolStatsPB) handle(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	switch {
	case r.URL.Path == "/api/collections/_superusers/auth-with-password" && r.Method == http.MethodPost:
		var body struct {
			Identity string `json:"identity"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Identity != "server@example.com" || body.Password != "superuser-pass-123" {
			writePoolJSON(w, http.StatusBadRequest, map[string]any{"message": "Failed to authenticate."})
			return
		}
		exp := time.Now().Add(time.Hour).Unix()
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `}`))
		writePoolJSON(w, http.StatusOK, map[string]any{"token": "h." + payload + ".sig"})
	case r.URL.Path == "/api/collections/pool_stats/records" && r.Method == http.MethodGet:
		f.list(w, r)
	case r.URL.Path == "/api/collections/pool_stats/records" && r.Method == http.MethodPost:
		f.create(w, r)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/collections/pool_stats/records/"):
		f.update(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *poolStatsPB) list(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filter := r.URL.Query().Get("filter")
	var items []map[string]any
	for _, rec := range f.records {
		if filter == "" || filter == `key = "main"` && rec["key"] == "main" {
			items = append(items, rec)
		}
	}
	writePoolJSON(w, http.StatusOK, map[string]any{
		"page": 1, "perPage": 1, "totalItems": len(items), "totalPages": len(items), "items": items,
	})
}

func (f *poolStatsPB) create(w http.ResponseWriter, r *http.Request) {
	body, err := decodePoolBody(r)
	if err != nil {
		writePoolJSON(w, http.StatusBadRequest, map[string]any{"message": "bad json"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	body["id"] = fmt.Sprintf("ps%012d", f.nextID)
	f.records = append(f.records, body)
	writePoolJSON(w, http.StatusOK, body)
}

func (f *poolStatsPB) update(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/collections/pool_stats/records/")
	body, err := decodePoolBody(r)
	if err != nil {
		writePoolJSON(w, http.StatusBadRequest, map[string]any{"message": "bad json"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rec := range f.records {
		if rec["id"] == id {
			for k, v := range body {
				rec[k] = v
			}
			writePoolJSON(w, http.StatusOK, rec)
			return
		}
	}
	http.NotFound(w, r)
}

func (f *poolStatsPB) mainRecord() (map[string]any, int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.records) == 0 {
		return nil, 0, ""
	}
	rec := f.records[0]
	data, _ := rec["data"].(map[string]any)
	id, _ := rec["id"].(string)
	return data, len(f.records), id
}

func (f *poolStatsPB) recordKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.records) == 0 {
		return ""
	}
	key, _ := f.records[0]["key"].(string)
	return key
}

func (f *poolStatsPB) requestCount() int64 {
	return f.requests.Load()
}

func decodePoolBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func writePoolJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
