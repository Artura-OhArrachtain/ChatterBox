package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeProfileRepository struct {
	profiles map[string]*Profile
}

func (f *fakeProfileRepository) Get(_ context.Context, uid string) (*Profile, error) {
	if profile := f.profiles[uid]; profile != nil {
		copy := *profile
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeProfileRepository) Put(_ context.Context, profile Profile) error {
	copy := profile
	f.profiles[profile.UID] = &copy
	return nil
}

func (f *fakeProfileRepository) SearchPrefix(_ context.Context, prefix string, limit int) ([]ProfileSummary, error) {
	result := make([]ProfileSummary, 0)
	lower := strings.ToLower(prefix)
	for _, profile := range f.profiles {
		if profile.Verified && strings.HasPrefix(strings.ToLower(profile.DisplayName), lower) {
			result = append(result, ProfileSummary{UID: profile.UID, DisplayName: profile.DisplayName})
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

type fakeDMRepository struct {
	conversations map[string]Conversation
}

func (f *fakeDMRepository) CreateOrGet(_ context.Context, uidA, uidB string) (Conversation, error) {
	id := stableConversationID(uidA, uidB)
	if conversation, ok := f.conversations[id]; ok {
		return conversation, nil
	}
	ids := []string{uidA, uidB}
	conversation := Conversation{ID: id, ParticipantIDs: ids, CreatedAt: time.Now().UTC()}
	f.conversations[id] = conversation
	return conversation, nil
}

func (f *fakeDMRepository) List(_ context.Context, uid string) ([]Conversation, error) {
	result := make([]Conversation, 0)
	for _, conversation := range f.conversations {
		for _, participant := range conversation.ParticipantIDs {
			if participant == uid {
				result = append(result, conversation)
				break
			}
		}
	}
	return result, nil
}

func (f *fakeDMRepository) IsParticipant(_ context.Context, conversationID, uid string) (bool, error) {
	conversation, ok := f.conversations[conversationID]
	if !ok {
		return false, nil
	}
	for _, participant := range conversation.ParticipantIDs {
		if participant == uid {
			return true, nil
		}
	}
	return false, nil
}

type fakeMessageRepository struct {
	messages []Message
	nextID   int
}

func (f *fakeMessageRepository) List(_ context.Context, roomID string, limit int) ([]Message, error) {
	result := make([]Message, 0)
	for _, message := range f.messages {
		if message.RoomID == roomID {
			result = append(result, message)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakeMessageRepository) Append(_ context.Context, message Message) (Message, error) {
	f.nextID++
	message.ID = "message-" + string(rune('0'+f.nextID))
	f.messages = append(f.messages, message)
	return message, nil
}

func authenticatedRequest(method, path, body, uid, email string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey{}, VerifiedToken{
		UID: uid, Email: email, EmailVerified: true,
	}))
	return request
}

func testHandlerWithFakes(profiles *fakeProfileRepository, dms *fakeDMRepository, messages *fakeMessageRepository) http.Handler {
	server := &chatServer{servers: []Server{}, rooms: []Room{{ID: "general", Name: "general", ServerID: "hangout"}}}
	return newHTTPHandlerWithRepositories(server, stubIDTokenVerifier{identity: VerifiedToken{
		UID: "user-a", Email: "a@example.com", EmailVerified: true,
	}}, profiles, dms, messages)
}

func TestMeCreateUpdateAnd404(t *testing.T) {
	profiles := &fakeProfileRepository{profiles: map[string]*Profile{}}
	handler := testHandlerWithFakes(profiles, &fakeDMRepository{conversations: map[string]Conversation{}}, &fakeMessageRepository{})

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, authenticatedRequest(http.MethodGet, "/api/me", "", "user-a", "a@example.com"))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing profile status = %d, want 404", missing.Code)
	}

	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, authenticatedRequest(http.MethodPut, "/api/me", `{"displayName":"  Alice  "}`, "user-a", "a@example.com"))
	if updated.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200: %s", updated.Code, updated.Body.String())
	}
	var public map[string]any
	if err := json.Unmarshal(updated.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public["displayName"] != "Alice" || public["uid"] != "user-a" {
		t.Fatalf("public profile = %#v", public)
	}
	if _, exists := public["email"]; !exists {
		t.Fatal("GET/PUT /api/me must include email")
	}
}

func TestProfileSearchReturnsOnlyVerifiedWithoutEmail(t *testing.T) {
	profiles := &fakeProfileRepository{profiles: map[string]*Profile{
		"user-a": {UID: "user-a", Email: "a@example.com", DisplayName: "Alice", Verified: true},
		"user-b": {UID: "user-b", Email: "b@example.com", DisplayName: "Bob", Verified: true},
		"user-c": {UID: "user-c", Email: "c@example.com", DisplayName: "Bobby", Verified: false},
	}}
	handler := testHandlerWithFakes(profiles, &fakeDMRepository{conversations: map[string]Conversation{}}, &fakeMessageRepository{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/users?query=bo", "", "user-a", "a@example.com"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "email") || strings.Contains(response.Body.String(), "b@example.com") {
		t.Fatalf("search exposed email: %s", response.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0]["uid"] != "user-b" {
		t.Fatalf("search result = %#v", result)
	}
}

func TestProfileSearchRequiresPrefixAndExcludesRequester(t *testing.T) {
	profiles := &fakeProfileRepository{profiles: map[string]*Profile{
		"user-a": {UID: "user-a", Email: "a@example.com", DisplayName: "Alice", Verified: true},
		"user-b": {UID: "user-b", Email: "b@example.com", DisplayName: "Alberta", Verified: true},
	}}
	handler := testHandlerWithFakes(profiles, &fakeDMRepository{conversations: map[string]Conversation{}}, &fakeMessageRepository{})

	empty := httptest.NewRecorder()
	handler.ServeHTTP(empty, authenticatedRequest(http.MethodGet, "/api/users?query=", "", "user-a", "a@example.com"))
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty search status = %d, want 400", empty.Code)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/users?query=al", "", "user-a", "a@example.com"))
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200", response.Code)
	}
	var result []ProfileSummary
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].UID != "user-b" {
		t.Fatalf("search results = %#v, want only the other user", result)
	}
}

func TestStableDMAndCrossUserMessageAccess(t *testing.T) {
	profiles := &fakeProfileRepository{profiles: map[string]*Profile{
		"user-a": {UID: "user-a", Email: "a@example.com", DisplayName: "Alice", Verified: true},
		"user-b": {UID: "user-b", Email: "b@example.com", DisplayName: "Bob", Verified: true},
	}}
	dms := &fakeDMRepository{conversations: map[string]Conversation{}}
	messages := &fakeMessageRepository{}
	handler := testHandlerWithFakes(profiles, dms, messages)

	create := httptest.NewRecorder()
	handler.ServeHTTP(create, authenticatedRequest(http.MethodPost, "/api/dms", `{"userId":"user-b"}`, "user-a", "a@example.com"))
	if create.Code != http.StatusCreated {
		t.Fatalf("create DM status = %d: %s", create.Code, create.Body.String())
	}
	var conversation ConversationResponse
	if err := json.Unmarshal(create.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	if conversation.ID == "" || conversation.OtherUserID != "user-b" {
		t.Fatalf("conversation = %#v", conversation)
	}

	send := httptest.NewRecorder()
	handler.ServeHTTP(send, authenticatedRequest(http.MethodPost, "/api/messages", `{"roomId":"`+conversation.ID+`","content":"hello","author":"Mallory","userId":"user-b"}`, "user-a", "a@example.com"))
	if send.Code != http.StatusCreated {
		t.Fatalf("send status = %d: %s", send.Code, send.Body.String())
	}
	if len(messages.messages) != 1 || messages.messages[0].Author != "Alice" || messages.messages[0].AuthorID != "user-a" {
		t.Fatalf("persisted message = %#v", messages.messages)
	}

	crossUser := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/messages?room="+conversation.ID, nil)
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey{}, VerifiedToken{
		UID: "user-c", Email: "c@example.com", EmailVerified: true,
	}))
	server := &chatServer{rooms: []Room{{ID: "general", Name: "general", ServerID: "hangout"}}, dms: dms, messages: messages}
	server.handleMessages(crossUser, request)
	if crossUser.Code != http.StatusForbidden {
		t.Fatalf("cross-user read status = %d, want 403", crossUser.Code)
	}
}

func TestReversePairUsesStableConversationAndChannelAccess(t *testing.T) {
	dms := &fakeDMRepository{conversations: map[string]Conversation{}}
	first, err := dms.CreateOrGet(context.Background(), "user-a", "user-b")
	if err != nil {
		t.Fatal(err)
	}
	second, err := dms.CreateOrGet(context.Background(), "user-b", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("reverse DM IDs differ: %q and %q", first.ID, second.ID)
	}

	messages := &fakeMessageRepository{}
	server := &chatServer{
		rooms:    []Room{{ID: "general", Name: "general", ServerID: "hangout"}},
		dms:      dms,
		messages: messages,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/messages?room=unknown", nil)
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey{}, VerifiedToken{
		UID: "user-a", Email: "a@example.com", EmailVerified: true,
	}))
	response := httptest.NewRecorder()
	server.handleMessages(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unknown room status = %d, want 403", response.Code)
	}
}
