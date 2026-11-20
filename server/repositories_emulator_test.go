package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

// TestFirestoreMessagePagingEmulator exercises the real Firestore queries.
// It runs only against the Firestore emulator, for example:
//
//	firebase emulators:exec --only firestore "go test ./server -run Emulator"
func TestFirestoreMessagePagingEmulator(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("set FIRESTORE_EMULATOR_HOST to run against the Firestore emulator")
	}
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, fmt.Sprintf("chatterbox-test-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	repo := NewFirestoreMessageRepository(client)

	// Seven messages in "general" where m3-m5 share one timestamp, so paging
	// must break ties consistently. Document IDs are chosen so ID order
	// differs from insertion order inside the tie.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		id, room, content string
		at                time.Time
	}{
		{"g1", "general", "m1", base},
		{"g2", "general", "m2", base.Add(time.Second)},
		{"z3", "general", "m3", base.Add(2 * time.Second)},
		{"a4", "general", "m4", base.Add(2 * time.Second)},
		{"k5", "general", "m5", base.Add(2 * time.Second)},
		{"g6", "general", "m6", base.Add(3 * time.Second)},
		{"g7", "general", "m7", base.Add(4 * time.Second)},
		{"o1", "other", "elsewhere", base.Add(5 * time.Second)},
	}
	for _, m := range seed {
		if _, err := client.Collection("messages").Doc(m.id).Set(ctx, map[string]any{
			"roomId": m.room, "author": "a", "authorId": "u", "content": m.content, "createdAt": m.at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list := func(page MessagePage) ([]Message, error) { return repo.List(ctx, "general", page) }
	contents := func(msgs []Message) string {
		parts := make([]string, len(msgs))
		for i, m := range msgs {
			parts[i] = m.Content
		}
		return strings.Join(parts, ",")
	}

	// Walk backwards two at a time from the newest page; every message must
	// appear exactly once, in order.
	var walked []Message
	page, err := list(MessagePage{Limit: 2})
	for err == nil && len(page) > 0 {
		walked = append(page, walked...)
		page, err = list(MessagePage{Before: page[0].ID, Limit: 2})
	}
	if err != nil {
		t.Fatal(err)
	}
	all := contents(walked)
	if len(walked) != 7 {
		t.Fatalf("backward walk returned %d messages: %s", len(walked), all)
	}
	for _, want := range []string{"m1", "m2", "m6", "m7"} {
		if strings.Count(all, want) != 1 {
			t.Fatalf("backward walk %s: %s missing or repeated", all, want)
		}
	}

	// Walk forwards one at a time from the oldest message; it must visit the
	// same sequence as the backward walk.
	forward := []Message{walked[0]}
	for {
		next, err := list(MessagePage{After: forward[len(forward)-1].ID, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(next) == 0 {
			break
		}
		forward = append(forward, next...)
	}
	if got := contents(forward); got != all {
		t.Fatalf("forward walk %s differs from backward walk %s", got, all)
	}

	if got, _ := list(MessagePage{Limit: 3}); contents(got) != contents(walked[4:]) {
		t.Fatalf("latest page = %s, want %s", contents(got), contents(walked[4:]))
	}
	for name, page := range map[string]MessagePage{
		"other room": {Before: "o1", Limit: 2},
		"missing":    {After: "nope", Limit: 2},
		"bad id":     {After: "a/b", Limit: 2},
	} {
		if _, err := list(page); !errors.Is(err, ErrCursorNotFound) {
			t.Errorf("%s cursor: err = %v, want ErrCursorNotFound", name, err)
		}
	}
}
