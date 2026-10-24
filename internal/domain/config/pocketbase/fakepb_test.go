package pocketbase

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeEmail    = "server@example.com"
	fakePassword = "superuser-pass-123"
)

// fakePB is the small slice of PocketBase the provider talks to: superuser auth, and records
// list/create/update with the unique indexes of the real migrations.
type fakePB struct {
	t      *testing.T
	server *httptest.Server

	mu          sync.Mutex
	collections map[string][]map[string]any
	files       map[string][]byte
	nextID      int
	down        bool
	tokenTTL    time.Duration
	validTokens map[string]bool
	authCalls   int
	refreshes   int
	issued      int
	requests    []string

	sseIDs  map[string]bool
	conns   []*sseConn
	subs    [][]string
	sseHeld bool
	nextSSE int
}

type sseConn struct {
	id     string
	events chan string
	drop   chan struct{}
	once   sync.Once
}

func (c *sseConn) close() {
	c.once.Do(func() { close(c.drop) })
}

func newFakePB(t *testing.T) *fakePB {
	t.Helper()
	f := &fakePB{
		t:           t,
		collections: map[string][]map[string]any{},
		files:       map[string][]byte{},
		tokenTTL:    time.Hour,
		validTokens: map[string]bool{},
		sseIDs:      map[string]bool{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakePB) URL() string { return f.server.URL }

func (f *fakePB) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// seed adds a record straight to a collection and returns it with its generated id.
func (f *fakePB) seed(collection string, rec map[string]any) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	rec["id"] = fmt.Sprintf("rec%012d", f.nextID)
	f.collections[collection] = append(f.collections[collection], rec)
	return rec
}

// find returns a copy of the first record matching field == value.
func (f *fakePB) find(collection, field string, value any) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rec := range f.collections[collection] {
		if rec[field] == value {
			return cloneMap(rec)
		}
	}
	return nil
}

// seedFile stores the bytes PocketBase would serve for a file field.
func (f *fakePB) seedFile(collection, id, name string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[collection+"/"+id+"/"+name] = append([]byte(nil), body...)
}

// patch edits a record the way the owner does in the dashboard.
func (f *fakePB) patch(collection, id string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rec := range f.collections[collection] {
		if rec["id"] == id {
			for k, v := range fields {
				rec[k] = v
			}
			return
		}
	}
	f.t.Fatalf("patch: no %s record %s", collection, id)
}

func (f *fakePB) count(collection string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.collections[collection])
}

func (f *fakePB) invalidateTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validTokens = map[string]bool{}
}

func (f *fakePB) auths() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authCalls
}

func (f *fakePB) refreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes
}

func (f *fakePB) setTokenTTL(ttl time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenTTL = ttl
}

var uniqueFields = map[string][]string{
	"devices":  {"device_id", "code"},
	"settings": {"key"},
}

var recordsRoute = regexp.MustCompile(`^/api/collections/([a-z_]+)/records(?:/([A-Za-z0-9]+))?$`)

func (f *fakePB) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/realtime" && r.Method == http.MethodGet {
		f.serveSSE(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	if f.down {
		http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.URL.Path == "/api/collections/_superusers/auth-with-password" && r.Method == http.MethodPost:
		f.authLocked(w, r)
		return
	case r.URL.Path == "/api/collections/_superusers/auth-refresh" && r.Method == http.MethodPost:
		if !f.validTokens[r.Header.Get("Authorization")] {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "The request requires valid record authorization token."})
			return
		}
		f.refreshes++
		f.respondTokenLocked(w)
		return
	}

	if !f.validTokens[r.Header.Get("Authorization")] {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "The request requires valid record authorization token."})
		return
	}
	if r.URL.Path == "/api/realtime" && r.Method == http.MethodPost {
		f.subscribeLocked(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/files/") {
		f.fileLocked(w, r)
		return
	}
	m := recordsRoute.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	collection, id := m[1], m[2]
	switch {
	case r.Method == http.MethodGet && id == "":
		f.listLocked(w, r, collection)
	case r.Method == http.MethodPost && id == "":
		f.createLocked(w, r, collection)
	case r.Method == http.MethodPatch && id != "":
		f.updateLocked(w, r, collection, id)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakePB) authLocked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identity string `json:"identity"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Identity != fakeEmail || body.Password != fakePassword {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "Failed to authenticate."})
		return
	}
	f.authCalls++
	f.respondTokenLocked(w)
}

func (f *fakePB) respondTokenLocked(w http.ResponseWriter) {
	f.issued++
	exp := time.Now().Add(f.tokenTTL).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `}`))
	token := fmt.Sprintf("h.%s.sig%d", payload, f.issued)
	f.validTokens[token] = true
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "record": map[string]any{"id": "su1"}})
}

var simpleFilter = regexp.MustCompile(`^\(?\s*(\w+)\s*=\s*("(?:[^"\\]|\\.)*"|true|false)\s*\)?$`)

func matchFilter(rec map[string]any, filter string) (bool, error) {
	if strings.TrimSpace(filter) == "" {
		return true, nil
	}
	m := simpleFilter.FindStringSubmatch(strings.TrimSpace(filter))
	if m == nil {
		return false, fmt.Errorf("fake PocketBase cannot parse filter %q", filter)
	}
	field, raw := m[1], m[2]
	switch raw {
	case "true":
		return rec[field] == true, nil
	case "false":
		return rec[field] != true, nil
	}
	unquoted, err := strconv.Unquote(raw)
	if err != nil {
		return false, err
	}
	return rec[field] == unquoted, nil
}

func (f *fakePB) fileLocked(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/files/")
	body, ok := f.files[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (f *fakePB) listLocked(w http.ResponseWriter, r *http.Request, collection string) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("perPage"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	var matched []map[string]any
	for _, rec := range f.collections[collection] {
		ok, err := matchFilter(rec, q.Get("filter"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
			return
		}
		if ok {
			matched = append(matched, f.expandLocked(cloneMap(rec), q.Get("expand")))
		}
	}
	start := (page - 1) * perPage
	if start > len(matched) {
		start = len(matched)
	}
	end := start + perPage
	if end > len(matched) {
		end = len(matched)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page": page, "perPage": perPage, "totalItems": len(matched),
		"totalPages": (len(matched) + perPage - 1) / perPage, "items": matched[start:end],
	})
}

// expandLocked mimics ?expand=agent for the single relation the provider asks for.
func (f *fakePB) expandLocked(rec map[string]any, expand string) map[string]any {
	if expand != "agent" {
		return rec
	}
	id, _ := rec["agent"].(string)
	if id == "" {
		return rec
	}
	for _, agent := range f.collections["agents"] {
		if agent["id"] == id {
			rec["expand"] = map[string]any{"agent": cloneMap(agent)}
		}
	}
	return rec
}

func (f *fakePB) createLocked(w http.ResponseWriter, r *http.Request, collection string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad json"})
		return
	}
	if field := f.uniqueViolationLocked(collection, "", body); field != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"message": "Failed to create record.",
			"data":    map[string]any{field: map[string]any{"code": "validation_not_unique"}},
		})
		return
	}
	f.nextID++
	body["id"] = fmt.Sprintf("rec%012d", f.nextID)
	f.collections[collection] = append(f.collections[collection], body)
	writeJSON(w, http.StatusOK, body)
}

func (f *fakePB) updateLocked(w http.ResponseWriter, r *http.Request, collection, id string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad json"})
		return
	}
	for _, rec := range f.collections[collection] {
		if rec["id"] != id {
			continue
		}
		if field := f.uniqueViolationLocked(collection, id, body); field != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"message": "Failed to update record.",
				"data":    map[string]any{field: map[string]any{"code": "validation_not_unique"}},
			})
			return
		}
		for k, v := range body {
			rec[k] = v
		}
		writeJSON(w, http.StatusOK, rec)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "The requested resource wasn't found."})
}

func (f *fakePB) uniqueViolationLocked(collection, selfID string, body map[string]any) string {
	for _, field := range uniqueFields[collection] {
		v, ok := body[field]
		if !ok {
			continue
		}
		for _, rec := range f.collections[collection] {
			if rec["id"] != selfID && rec[field] == v {
				return field
			}
		}
	}
	return ""
}

func (f *fakePB) serveSSE(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	down, hold := f.down, f.sseHeld
	tokenOK := f.validTokens[r.Header.Get("Authorization")]
	f.mu.Unlock()
	if down || hold {
		http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if !tokenOK {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "The request requires valid record authorization token."})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	f.mu.Lock()
	f.nextSSE++
	conn := &sseConn{
		id:     fmt.Sprintf("sse%d", f.nextSSE),
		events: make(chan string, 8),
		drop:   make(chan struct{}),
	}
	f.sseIDs[conn.id] = true
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	defer func() {
		conn.close()
		f.mu.Lock()
		delete(f.sseIDs, conn.id)
		f.conns = removeConn(f.conns, conn)
		f.mu.Unlock()
	}()

	fmt.Fprintf(w, "event: PB_CONNECT\ndata: {\"clientId\":%q}\n\n", conn.id)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-conn.drop:
			return
		case chunk := <-conn.events:
			fmt.Fprint(w, chunk)
			flusher.Flush()
		}
	}
}

func (f *fakePB) subscribeLocked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID      string   `json:"clientId"`
		Subscriptions []string `json:"subscriptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !f.sseIDs[body.ClientID] {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Missing or invalid client id."})
		return
	}
	f.subs = append(f.subs, append([]string(nil), body.Subscriptions...))
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakePB) subscriptionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

func (f *fakePB) lastSubscriptions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) == 0 {
		return nil
	}
	return append([]string(nil), f.subs[len(f.subs)-1]...)
}

func (f *fakePB) streamCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.conns)
}

func (f *fakePB) holdSSE(hold bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sseHeld = hold
}

func (f *fakePB) dropStreams() {
	f.mu.Lock()
	conns := append([]*sseConn(nil), f.conns...)
	f.mu.Unlock()
	for _, conn := range conns {
		conn.close()
	}
}

func (f *fakePB) publish(event, data string) {
	chunk := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	f.mu.Lock()
	conns := append([]*sseConn(nil), f.conns...)
	f.mu.Unlock()
	for _, conn := range conns {
		select {
		case conn.events <- chunk:
		case <-conn.drop:
		}
	}
}

func removeConn(conns []*sseConn, target *sseConn) []*sseConn {
	out := make([]*sseConn, 0, len(conns))
	for _, conn := range conns {
		if conn != target {
			out = append(out, conn)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
