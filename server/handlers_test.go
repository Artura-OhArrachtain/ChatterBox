package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Handler tests for the Go API. Requests carry a verified identity in their
// context, as firebaseIDTokenMiddleware would set it; the middleware itself is
// covered in auth_test.go.

func newTestServer() *chatServer {
	return &chatServer{
		servers: []Server{{ID: "hangout", Name: "The Hangout"}, {ID: "test", Name: "Test Server"}},
		rooms: []Room{
			{ID: "general", Name: "general", ServerID: "hangout"},
			{ID: "random", Name: "random", ServerID: "hangout"},
			{ID: "test-general", Name: "general", ServerID: "test"},
		},
		nextSignalID: 1,
		pushTokens:   make(map[string]string),
		profiles: &fakeProfileRepository{profiles: map[string]*Profile{
			"alice": {UID: "alice", DisplayName: "Alice", Verified: true},
			"bob":   {UID: "bob", DisplayName: "Bob", Verified: true},
		}},
		dms:      &fakeDMRepository{conversations: map[string]Conversation{}},
		messages: &fakeMessageRepository{},
	}
}

// doAs calls h as the given signed-in user.
func doAs(t *testing.T, uid string, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), identityContextKey{}, VerifiedToken{
		UID: uid, Email: uid + "@example.com", EmailVerified: true,
	}))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func do(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doAs(t, "alice", h, method, target, body)
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
		t.Run(name+"/patch", func(t *testing.T) {
			rec := do(t, h, http.MethodPatch, "/", "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("PATCH status = %d, want 405", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != "GET, POST, PUT, DELETE, OPTIONS" {
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
	for _, m := range []Message{
		{RoomID: "general", Author: "Alice", Content: "hi"},
		{RoomID: "random", Author: "Bob", Content: "yo"},
		{RoomID: "general", Author: "Bob", Content: "hey"},
	} {
		if _, err := s.messages.Append(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}

	if rec := do(t, s.handleMessages, http.MethodGet, "/api/messages", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing room status = %d, want 400", rec.Code)
	}
	if rec := do(t, s.handleMessages, http.MethodGet, "/api/messages?room=nope", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown room status = %d, want 403", rec.Code)
	}

	rec := do(t, s.handleMessages, http.MethodGet, "/api/messages?room=general", "")
	got := decode[[]Message](t, rec)
	if len(got) != 2 || got[0].Content != "hi" || got[1].Content != "hey" {
		t.Fatalf("general messages = %+v", got)
	}

	rec = do(t, s.handleMessages, http.MethodGet, "/api/messages?room=test-general", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty room body = %q, want []", rec.Body.String())
	}
}

func TestHandleMessagesPost(t *testing.T) {
	s := newTestServer()
	// The author comes from the signed-in user's profile; a client-supplied
	// author is ignored.
	rec := do(t, s.handleMessages, http.MethodPost, "/api/messages",
		`{"roomId":" general ","author":"Mallory","content":"  hello  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	msg := decode[Message](t, rec)
	if msg.ID == "" || msg.RoomID != "general" || msg.Author != "Alice" || msg.AuthorID != "alice" || msg.Content != "hello" {
		t.Fatalf("message not trimmed or attributed: %+v", msg)
	}
	if msg.CreatedAt.IsZero() || msg.CreatedAt.Location().String() != "UTC" {
		t.Errorf("CreatedAt = %v, want non-zero UTC", msg.CreatedAt)
	}

	second := decode[Message](t, doAs(t, "bob", s.handleMessages, http.MethodPost, "/",
		`{"roomId":"general","content":"again"}`))
	if second.ID == "" || second.ID == msg.ID || second.Author != "Bob" {
		t.Errorf("second message = %+v", second)
	}

	stored := decode[[]Message](t, do(t, s.handleMessages, http.MethodGet, "/?room=general", ""))
	if len(stored) != 2 || stored[0].Content != "hello" || stored[1].Content != "again" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestHandleMessagesPostValidation(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		body string
		want int
	}{
		{"invalid json", "alice", `{`, http.StatusBadRequest},
		{"missing room", "alice", `{"content":"c"}`, http.StatusBadRequest},
		{"whitespace content", "alice", `{"roomId":"general","content":"   "}`, http.StatusBadRequest},
		{"unknown room", "alice", `{"roomId":"nope","content":"c"}`, http.StatusForbidden},
		// The client's built-in contacts are not real DM conversations.
		{"placeholder dm", "alice", `{"roomId":"dm-maya","content":"hi"}`, http.StatusForbidden},
		{"no profile yet", "carol", `{"roomId":"general","content":"hi"}`, http.StatusForbidden},
		{"2000 chars", "alice", fmt.Sprintf(`{"roomId":"general","content":%q}`, strings.Repeat("x", 2000)), http.StatusCreated},
		{"2001 chars", "alice", fmt.Sprintf(`{"roomId":"general","content":%q}`, strings.Repeat("x", 2001)), http.StatusBadRequest},
		{"body over 16KiB", "alice", fmt.Sprintf(`{"roomId":"general","content":%q}`, strings.Repeat("x", 17*1024)), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			rec := doAs(t, tt.uid, s.handleMessages, http.MethodPost, "/api/messages", tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.want, rec.Body.String())
			}
			stored := s.messages.(*fakeMessageRepository).messages
			if tt.want != http.StatusCreated && len(stored) != 0 {
				t.Fatalf("rejected message was stored: %+v", stored)
			}
		})
	}
}

func TestHandleMessagesInRealDM(t *testing.T) {
	s := newTestServer()
	conversation, err := s.dms.CreateOrGet(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"roomId":%q,"content":"psst"}`, conversation.ID)
	if rec := do(t, s.handleMessages, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
		t.Fatalf("participant post status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, "bob", s.handleMessages, http.MethodGet, "/?room="+conversation.ID, ""); rec.Code != http.StatusOK || len(decode[[]Message](t, rec)) != 1 {
		t.Fatalf("other participant read status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, "mallory", s.handleMessages, http.MethodGet, "/?room="+conversation.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider read status = %d, want 403", rec.Code)
	}
}

// Token ownership, 409 on a foreign token and DELETE are covered in
// repositories_test.go; this checks input handling.
func TestHandlePushRegistration(t *testing.T) {
	s := newTestServer()
	rec := do(t, s.handlePushRegistration, http.MethodPost, "/api/push/register", `{"token":" tok-1 "}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := s.pushTokens["tok-1"]; got != "alice" {
		t.Fatalf("token not trimmed or not owned by the signed-in user: %v", s.pushTokens)
	}
	// Registering the same token again as its owner is idempotent.
	if rec := do(t, s.handlePushRegistration, http.MethodPost, "/", `{"token":"tok-1"}`); rec.Code != http.StatusNoContent || len(s.pushTokens) != 1 {
		t.Fatalf("re-register status = %d, tokens = %v", rec.Code, s.pushTokens)
	}

	for _, body := range []string{`{`, `{}`, `{"token":" "}`} {
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

// Peer routing, owner binding and malformed peer IDs are also covered in
// repositories_test.go.
func TestHandleCallSignalsPost(t *testing.T) {
	s := newTestServer()
	rec := do(t, s.handleCallSignals, http.MethodPost, "/api/calls/signals",
		`{"roomId":" general ","from":"alice-1","to":" bob-1 ","type":" offer ","sdp":"v=0","sdpMid":"0","sdpMLineIndex":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	sig := decode[CallSignal](t, rec)
	if sig.ID != 1 || sig.RoomID != "general" || sig.From != "alice-1" || sig.To != "bob-1" || sig.Type != "offer" {
		t.Fatalf("signal not trimmed or numbered: %+v", sig)
	}
	if sig.SDPMid == nil || *sig.SDPMid != "0" || sig.SDPMLine == nil || *sig.SDPMLine != 1 || sig.CreatedAt.IsZero() {
		t.Fatalf("signal fields = %+v", sig)
	}
	if s.signals[0].OwnerUID != "alice" {
		t.Fatalf("stored owner = %q, want the signed-in user", s.signals[0].OwnerUID)
	}
	if strings.Contains(rec.Body.String(), "alice\"") || strings.Contains(rec.Body.String(), "OwnerUID") {
		t.Fatalf("owner UID leaked in response: %s", rec.Body.String())
	}

	for _, typ := range []string{"join", "leave", "answer", "candidate"} {
		body := fmt.Sprintf(`{"roomId":"general","from":"alice-1","type":%q}`, typ)
		if rec := do(t, s.handleCallSignals, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
			t.Errorf("type %s status = %d, want 201", typ, rec.Code)
		}
	}

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"invalid json":     {`{`, http.StatusBadRequest},
		"missing room":     {`{"from":"alice-1","type":"join"}`, http.StatusBadRequest},
		"missing from":     {`{"roomId":"general","type":"join"}`, http.StatusBadRequest},
		"missing type":     {`{"roomId":"general","from":"alice-1"}`, http.StatusBadRequest},
		"unsupported type": {`{"roomId":"general","from":"alice-1","type":"hangup"}`, http.StatusBadRequest},
		"unknown room":     {`{"roomId":"nope","from":"alice-1","type":"join"}`, http.StatusForbidden},
	} {
		if rec := do(t, s.handleCallSignals, http.MethodPost, "/", tc.body); rec.Code != tc.want {
			t.Errorf("%s status = %d, want %d", name, rec.Code, tc.want)
		}
	}
	if len(s.signals) != 5 {
		t.Fatalf("stored %d signals, want 5", len(s.signals))
	}
}

func TestHandleCallSignalsGet(t *testing.T) {
	s := newTestServer()
	post := func(uid, body string) {
		t.Helper()
		if rec := doAs(t, uid, s.handleCallSignals, http.MethodPost, "/", body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: status %d", body, rec.Code)
		}
	}
	post("alice", `{"roomId":"general","from":"alice-1","type":"join"}`)                 // 1: broadcast from alice
	post("bob", `{"roomId":"general","from":"bob-1","type":"join"}`)                     // 2: bob's own signal
	post("alice", `{"roomId":"general","from":"alice-1","to":"bob-1","type":"offer"}`)   // 3: directed to bob's peer
	post("alice", `{"roomId":"general","from":"alice-1","to":"carol-1","type":"offer"}`) // 4: directed to someone else
	post("alice", `{"roomId":"random","from":"alice-1","type":"join"}`)                  // 5: other room

	ids := func(uid, query string) []int64 {
		t.Helper()
		rec := doAs(t, uid, s.handleCallSignals, http.MethodGet, "/api/calls/signals"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s as %s status = %d", query, uid, rec.Code)
		}
		var out []int64
		for _, sig := range decode[[]CallSignal](t, rec) {
			out = append(out, sig.ID)
		}
		return out
	}

	tests := []struct {
		uid, query string
		want       []int64
	}{
		{"bob", "?room=general&peer=bob-1", []int64{1, 3}},
		{"bob", "?room=general&peer=bob-1&after=1", []int64{3}},
		{"bob", "?room=general&peer=bob-1&after=3", nil},
		{"carol", "?room=general&peer=carol-1", []int64{1, 2, 4}},
		{"bob", "?room=random&peer=bob-1", []int64{5}},
	}
	for _, tt := range tests {
		got := ids(tt.uid, tt.query)
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("GET %s as %s = %v, want %v", tt.query, tt.uid, got, tt.want)
		}
	}

	rec := doAs(t, "bob", s.handleCallSignals, http.MethodGet, "/?room=general&peer=bob-1&after=99", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("no-signal body = %q, want []", rec.Body.String())
	}
	for name, tc := range map[string]struct {
		query string
		want  int
	}{
		"missing room":   {"/?peer=bob-1", http.StatusBadRequest},
		"missing peer":   {"/?room=general", http.StatusBadRequest},
		"bad after":      {"/?room=general&peer=bob-1&after=garbage", http.StatusBadRequest},
		"negative after": {"/?room=general&peer=bob-1&after=-1", http.StatusBadRequest},
		"unknown room":   {"/?room=nope&peer=bob-1", http.StatusForbidden},
	} {
		if rec := doAs(t, "bob", s.handleCallSignals, http.MethodGet, tc.query, ""); rec.Code != tc.want {
			t.Errorf("GET %s status = %d, want %d", name, rec.Code, tc.want)
		}
	}
}

func TestHandleCallSignalsTrimsHistory(t *testing.T) {
	s := newTestServer()
	const join = `{"roomId":"general","from":"alice-1","type":"join"}`
	for range 2001 {
		do(t, s.handleCallSignals, http.MethodPost, "/", join)
	}
	if len(s.signals) != 1000 {
		t.Fatalf("len(signals) = %d, want 1000 after exceeding 2000", len(s.signals))
	}
	if first, last := s.signals[0].ID, s.signals[len(s.signals)-1].ID; first != 1002 || last != 2001 {
		t.Fatalf("kept IDs %d..%d, want 1002..2001", first, last)
	}
	// IDs keep counting after a trim, so clients polling with ?after= do not see repeats.
	sig := decode[CallSignal](t, do(t, s.handleCallSignals, http.MethodPost, "/", join))
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
		"Access-Control-Allow-Headers": "Authorization, Content-Type",
		"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
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
