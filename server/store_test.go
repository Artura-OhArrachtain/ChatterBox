package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

var testConfig = serverConfig{
	Servers: []Server{{ID: "hangout", Name: "The Hangout"}},
	Rooms:   []Room{{ID: "general", Name: "general", ServerID: "hangout"}},
}

func TestMessagesSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if seeded, err := db.seedIfEmpty(testConfig, welcomeMessages()[:1]); err != nil || !seeded {
		t.Fatalf("seed: seeded=%v err=%v", seeded, err)
	}
	if _, err := db.addMessage("general", "alice", "persist me"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if seeded, err := db.seedIfEmpty(testConfig, welcomeMessages()); err != nil || seeded {
		t.Fatalf("reseeded existing database: seeded=%v err=%v", seeded, err)
	}
	msgs, err := db.messages("general")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Content != "persist me" || msgs[1].Author != "alice" {
		t.Fatalf("unexpected messages after reopen: %+v", msgs)
	}
	if msgs[1].ID <= msgs[0].ID || msgs[1].CreatedAt.IsZero() {
		t.Fatalf("bad id ordering or timestamp: %+v", msgs)
	}
}

func TestMessageHandlers(t *testing.T) {
	db, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.seedIfEmpty(testConfig, nil); err != nil {
		t.Fatal(err)
	}
	s := &chatServer{db: db, nextSignalID: 1}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleMessages(rec, httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"roomId":"general","author":"bob","content":"hi"}`); rec.Code != http.StatusCreated {
		t.Fatalf("post: %d %s", rec.Code, rec.Body)
	}
	if rec := post(`{"roomId":"nope","author":"bob","content":"hi"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown room: got %d", rec.Code)
	}
	if rec := post(`{"roomId":"general","author":"","content":"hi"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing author: got %d", rec.Code)
	}

	rec := httptest.NewRecorder()
	s.handleMessages(rec, httptest.NewRequest(http.MethodGet, "/api/messages?room=general", nil))
	var msgs []Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil || len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("get: %d %s err=%v", rec.Code, rec.Body, err)
	}
}

func TestPushTokens(t *testing.T) {
	db, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for token, user := range map[string]string{"t1": "alice", "t2": "bob", "t3": "bob"} {
		if err := db.savePushToken(token, user); err != nil {
			t.Fatal(err)
		}
	}
	if tokens, _ := db.pushTokensExcept("bob"); len(tokens) != 1 || tokens[0] != "t1" {
		t.Fatalf("pushTokensExcept: %v", tokens)
	}
	if err := db.deletePushTokens([]string{"t1"}); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := db.pushTokensExcept("bob"); len(tokens) != 0 {
		t.Fatalf("token not deleted: %v", tokens)
	}
}
