package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestServer() *chatServer {
	return &chatServer{
		servers: []Server{{ID: "hangout", Name: "The Hangout"}, {ID: "test", Name: "Test Server"}},
		rooms: []Room{
			{ID: "general", Name: "general", ServerID: "hangout"},
			{ID: "random", Name: "random", ServerID: "hangout"},
			{ID: "test-general", Name: "general", ServerID: "test"},
		},
		nextID:       1,
		nextSignalID: 1,
		pushTokens:   make(map[string]string),
	}
}

func do(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHandlersMethodHandling(t *testing.T) {
	s := newTestServer()
	handlers := map[string]http.HandlerFunc{
		"servers":  s.handleServers,
		"rooms":    s.handleRooms,
		"messages": s.handleMessages,
		"push":     s.handlePushRegistration,
		"signals":  s.handleCallSignals,
	}
	for name, h := range handlers {
		t.Run(name+"/options", func(t *testing.T) {
			if rec := do(t, h, http.MethodOptions, "/", ""); rec.Code != http.StatusNoContent {
				t.Fatalf("OPTIONS status = %d, want 204", rec.Code)
			}
		})
		t.Run(name+"/delete", func(t *testing.T) {
			rec := do(t, h, http.MethodDelete, "/", "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("DELETE status = %d, want 405", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != "GET, POST, OPTIONS" {
				t.Fatalf("Allow = %q", got)
			}
		})
	}
	// Read-only and write-only endpoints reject the other verb.
	if rec := do(t, s.handleServers, http.MethodPost, "/", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST servers status = %d, want 405", rec.Code)
	}
	if rec := do(t, s.handleRooms, http.MethodPost, "/", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST rooms status = %d, want 405", rec.Code)
	}
	if rec := do(t, s.handlePushRegistration, http.MethodGet, "/", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET push status = %d, want 405", rec.Code)
	}
}

func TestHandleServers(t *testing.T) {
	rec := do(t, newTestServer().handleServers, http.MethodGet, "/api/servers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	got := decode[[]Server](t, rec)
	if len(got) != 2 || got[0].ID != "hangout" || got[1].Name != "Test Server" {
		t.Fatalf("servers = %+v", got)
	}
}

func TestHandleRooms(t *testing.T) {
	s := newTestServer()
	tests := []struct {
		query string
		want  []string
	}{
		{"", []string{"general", "random", "test-general"}},
		{"?server=hangout", []string{"general", "random"}},
		{"?server=test", []string{"test-general"}},
		{"?server=nope", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := do(t, s.handleRooms, http.MethodGet, "/api/rooms"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			// An unknown server must encode as [], not null, so clients can iterate it.
			if len(tt.want) == 0 && strings.TrimSpace(rec.Body.String()) != "[]" {
				t.Fatalf("body = %q, want []", rec.Body.String())
			}
			got := decode[[]Room](t, rec)
			if len(got) != len(tt.want) {
				t.Fatalf("rooms = %+v, want ids %v", got, tt.want)
			}
			for i, id := range tt.want {
				if got[i].ID != id {
					t.Errorf("room[%d] = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestHandleMessagesGet(t *testing.T) {
	s := newTestServer()
	s.messages = []Message{
		{ID: 1, RoomID: "general", Author: "Maya", Content: "hi"},
		{ID: 2, RoomID: "random", Author: "Leo", Content: "yo"},
		{ID: 3, RoomID: "general", Author: "Leo", Content: "hey"},
	}

	if rec := do(t, s.handleMessages, http.MethodGet, "/api/messages", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing room status = %d, want 400", rec.Code)
	}

	rec := do(t, s.handleMessages, http.MethodGet, "/api/messages?room=general", "")
	got := decode[[]Message](t, rec)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("general messages = %+v", got)
	}

	rec = do(t, s.handleMessages, http.MethodGet, "/api/messages?room=test-general", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty room body = %q, want []", rec.Body.String())
	}
}

func TestHandleMessagesPost(t *testing.T) {
	s := newTestServer()
	rec := do(t, s.handleMessages, http.MethodPost, "/api/messages",
		`{"roomId":" general ","author":" Maya ","content":"  hello  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	msg := decode[Message](t, rec)
	if msg.ID != 1 || msg.RoomID != "general" || msg.Author != "Maya" || msg.Content != "hello" {
		t.Fatalf("message not trimmed or numbered: %+v", msg)
	}
	if msg.CreatedAt.IsZero() || msg.CreatedAt.Location().String() != "UTC" {
		t.Errorf("CreatedAt = %v, want non-zero UTC", msg.CreatedAt)
	}

	second := decode[Message](t, do(t, s.handleMessages, http.MethodPost, "/",
		`{"roomId":"general","author":"Leo","content":"again"}`))
	if second.ID != 2 {
		t.Errorf("second ID = %d, want 2", second.ID)
	}

	stored := decode[[]Message](t, do(t, s.handleMessages, http.MethodGet, "/?room=general", ""))
	if len(stored) != 2 || stored[0].Content != "hello" || stored[1].Content != "again" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestHandleMessagesPostValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", `{`, http.StatusBadRequest},
		{"missing room", `{"author":"a","content":"c"}`, http.StatusBadRequest},
		{"missing author", `{"roomId":"general","content":"c"}`, http.StatusBadRequest},
		{"whitespace content", `{"roomId":"general","author":"a","content":"   "}`, http.StatusBadRequest},
		{"unknown room", `{"roomId":"nope","author":"a","content":"c"}`, http.StatusNotFound},
		// Seeded DM conversations are not rooms in config.json, so posting to them is rejected.
		{"dm conversation", `{"roomId":"dm-maya","author":"You","content":"hi"}`, http.StatusNotFound},
		{"2000 chars", fmt.Sprintf(`{"roomId":"general","author":"a","content":%q}`, strings.Repeat("x", 2000)), http.StatusCreated},
		{"2001 chars", fmt.Sprintf(`{"roomId":"general","author":"a","content":%q}`, strings.Repeat("x", 2001)), http.StatusBadRequest},
		{"body over 16KiB", fmt.Sprintf(`{"roomId":"general","author":"a","content":%q}`, strings.Repeat("x", 17*1024)), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			rec := do(t, s.handleMessages, http.MethodPost, "/api/messages", tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want != http.StatusCreated && len(s.messages) != 0 {
				t.Fatalf("rejected message was stored: %+v", s.messages)
			}
		})
	}
}

func TestHandleMessagesConcurrentPosts(t *testing.T) {
	s := newTestServer()
	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"roomId":"general","author":"u%d","content":"m%d"}`, i, i)
			if rec := do(t, s.handleMessages, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
				t.Errorf("status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	seen := map[int64]bool{}
	for _, m := range s.messages {
		if seen[m.ID] {
			t.Fatalf("duplicate message ID %d", m.ID)
		}
		seen[m.ID] = true
	}
	if len(seen) != n || s.nextID != n+1 {
		t.Fatalf("stored %d messages, nextID %d", len(seen), s.nextID)
	}
}

func TestHandlePushRegistration(t *testing.T) {
	s := newTestServer()
	rec := do(t, s.handlePushRegistration, http.MethodPost, "/api/push/register", `{"token":" tok-1 ","user":" Maya "}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := s.pushTokens["tok-1"]; got != "Maya" {
		t.Fatalf("pushTokens = %v", s.pushTokens)
	}

	// Re-registering a token reassigns it to the new user.
	do(t, s.handlePushRegistration, http.MethodPost, "/", `{"token":"tok-1","user":"Leo"}`)
	if got := s.pushTokens["tok-1"]; got != "Leo" || len(s.pushTokens) != 1 {
		t.Fatalf("pushTokens after re-register = %v", s.pushTokens)
	}

	for _, body := range []string{`{`, `{"token":"t"}`, `{"user":"u"}`, `{"token":" ","user":"u"}`} {
		if rec := do(t, s.handlePushRegistration, http.MethodPost, "/", body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s status = %d, want 400", body, rec.Code)
		}
	}
}

func TestNotifyOtherDevicesWithoutFCM(t *testing.T) {
	s := newTestServer()
	s.pushTokens["tok"] = "Leo"
	// With FCM unconfigured this must be a no-op rather than a nil dereference.
	s.notifyOtherDevices(Message{RoomID: "general", Author: "Maya", Content: "hi"})
}

func TestHandleCallSignalsPost(t *testing.T) {
	s := newTestServer()
	rec := do(t, s.handleCallSignals, http.MethodPost, "/api/calls/signals",
		`{"roomId":" general ","from":" alice ","to":" bob ","type":" offer ","sdp":"v=0","sdpMid":"0","sdpMLineIndex":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	sig := decode[CallSignal](t, rec)
	if sig.ID != 1 || sig.RoomID != "general" || sig.From != "alice" || sig.To != "bob" || sig.Type != "offer" {
		t.Fatalf("signal not trimmed or numbered: %+v", sig)
	}
	if sig.SDPMid == nil || *sig.SDPMid != "0" || sig.SDPMLine == nil || *sig.SDPMLine != 1 || sig.CreatedAt.IsZero() {
		t.Fatalf("signal fields = %+v", sig)
	}

	for _, typ := range []string{"join", "leave", "answer", "candidate"} {
		body := fmt.Sprintf(`{"roomId":"general","from":"alice","type":%q}`, typ)
		if rec := do(t, s.handleCallSignals, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
			t.Errorf("type %s status = %d, want 201", typ, rec.Code)
		}
	}

	for name, body := range map[string]string{
		"invalid json":     `{`,
		"missing room":     `{"from":"a","type":"join"}`,
		"missing from":     `{"roomId":"general","type":"join"}`,
		"missing type":     `{"roomId":"general","from":"a"}`,
		"unsupported type": `{"roomId":"general","from":"a","type":"hangup"}`,
	} {
		if rec := do(t, s.handleCallSignals, http.MethodPost, "/", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", name, rec.Code)
		}
	}
	if len(s.signals) != 5 {
		t.Fatalf("stored %d signals, want 5", len(s.signals))
	}
}

func TestHandleCallSignalsGet(t *testing.T) {
	s := newTestServer()
	post := func(body string) {
		t.Helper()
		if rec := do(t, s.handleCallSignals, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: status %d", body, rec.Code)
		}
	}
	post(`{"roomId":"general","from":"alice","type":"join"}`)               // 1: broadcast from alice
	post(`{"roomId":"general","from":"bob","type":"join"}`)                 // 2: bob's own signal
	post(`{"roomId":"general","from":"alice","to":"bob","type":"offer"}`)   // 3: directed to bob
	post(`{"roomId":"general","from":"alice","to":"carol","type":"offer"}`) // 4: directed to someone else
	post(`{"roomId":"random","from":"alice","type":"join"}`)                // 5: other room

	ids := func(query string) []int64 {
		t.Helper()
		rec := do(t, s.handleCallSignals, http.MethodGet, "/api/calls/signals"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", query, rec.Code)
		}
		var out []int64
		for _, sig := range decode[[]CallSignal](t, rec) {
			out = append(out, sig.ID)
		}
		return out
	}

	tests := []struct {
		query string
		want  []int64
	}{
		{"?room=general&peer=bob", []int64{1, 3}},
		{"?room=general&peer=bob&after=1", []int64{3}},
		{"?room=general&peer=bob&after=3", nil},
		{"?room=general&peer=bob&after=garbage", []int64{1, 3}},
		{"?room=general&peer=carol", []int64{1, 2, 4}},
		{"?room=random&peer=bob", []int64{5}},
	}
	for _, tt := range tests {
		got := ids(tt.query)
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("GET %s = %v, want %v", tt.query, got, tt.want)
		}
	}

	rec := do(t, s.handleCallSignals, http.MethodGet, "/?room=general&peer=bob&after=99", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("no-signal body = %q, want []", rec.Body.String())
	}
	for _, q := range []string{"", "?room=general", "?peer=bob"} {
		if rec := do(t, s.handleCallSignals, http.MethodGet, "/"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q status = %d, want 400", q, rec.Code)
		}
	}
}

func TestHandleCallSignalsTrimsHistory(t *testing.T) {
	s := newTestServer()
	for range 2001 {
		do(t, s.handleCallSignals, http.MethodPost, "/", `{"roomId":"general","from":"a","type":"join"}`)
	}
	if len(s.signals) != 1000 {
		t.Fatalf("len(signals) = %d, want 1000 after exceeding 2000", len(s.signals))
	}
	if first, last := s.signals[0].ID, s.signals[len(s.signals)-1].ID; first != 1002 || last != 2001 {
		t.Fatalf("kept IDs %d..%d, want 1002..2001", first, last)
	}
	// IDs keep counting after a trim, so clients polling with ?after= do not see repeats.
	sig := decode[CallSignal](t, do(t, s.handleCallSignals, http.MethodPost, "/", `{"roomId":"general","from":"a","type":"join"}`))
	if sig.ID != 2002 {
		t.Fatalf("next ID = %d, want 2002", sig.ID)
	}
}

func TestCORS(t *testing.T) {
	called := false
	h := cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called || rec.Code != http.StatusTeapot {
		t.Fatalf("wrapped handler not invoked (code %d)", rec.Code)
	}
	for k, want := range map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Headers": "Content-Type",
		"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadServerConfig(t *testing.T) {
	t.Run("shipped config", func(t *testing.T) {
		t.Setenv("CHATTERBOX_SERVER_CONFIG", "config.json")
		cfg, err := loadServerConfig()
		if err != nil {
			t.Fatalf("server/config.json: %v", err)
		}
		if len(cfg.Servers) == 0 || len(cfg.Rooms) == 0 {
			t.Fatalf("config = %+v", cfg)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		t.Setenv("CHATTERBOX_SERVER_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
		if _, err := loadServerConfig(); err == nil {
			t.Fatal("want error for missing file")
		}
	})

	tests := []struct {
		name, body string
		ok         bool
	}{
		{"valid", `{"servers":[{"id":"s","name":"S"}],"rooms":[{"id":"r","name":"R","serverId":"s"}]}`, true},
		{"invalid json", `{`, false},
		{"two json values", `{"servers":[{"id":"s","name":"S"}],"rooms":[{"id":"r","name":"R","serverId":"s"}]} {}`, false},
		{"no servers", `{"servers":[],"rooms":[{"id":"r","name":"R","serverId":"s"}]}`, false},
		{"no rooms", `{"servers":[{"id":"s","name":"S"}],"rooms":[]}`, false},
		{"blank server name", `{"servers":[{"id":"s","name":" "}],"rooms":[{"id":"r","name":"R","serverId":"s"}]}`, false},
		{"duplicate server", `{"servers":[{"id":"s","name":"S"},{"id":"s","name":"T"}],"rooms":[{"id":"r","name":"R","serverId":"s"}]}`, false},
		{"duplicate room", `{"servers":[{"id":"s","name":"S"}],"rooms":[{"id":"r","name":"R","serverId":"s"},{"id":"r","name":"Q","serverId":"s"}]}`, false},
		{"unknown serverId", `{"servers":[{"id":"s","name":"S"}],"rooms":[{"id":"r","name":"R","serverId":"x"}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CHATTERBOX_SERVER_CONFIG", path)
			_, err := loadServerConfig()
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}
