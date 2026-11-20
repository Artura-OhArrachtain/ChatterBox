package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
)

type Room struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"serverId"`
}

type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type serverConfig struct {
	Servers []Server `json:"servers"`
	Rooms   []Room   `json:"rooms"`
}

type Message struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"roomId"`
	Author    string    `json:"author"`
	AuthorID  string    `json:"authorId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type CallSignal struct {
	ID        int64     `json:"id"`
	RoomID    string    `json:"roomId"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	OwnerUID  string    `json:"-"`
	Type      string    `json:"type"`
	SDP       string    `json:"sdp,omitempty"`
	Candidate string    `json:"candidate,omitempty"`
	SDPMid    *string   `json:"sdpMid,omitempty"`
	SDPMLine  *int      `json:"sdpMLineIndex,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type chatServer struct {
	mu           sync.RWMutex
	servers      []Server
	rooms        []Room
	signals      []CallSignal
	nextSignalID int64
	pushTokens   map[string]string
	fcm          *messaging.Client
	profiles     ProfileRepository
	dms          DMRepository
	messages     MessageRepository
}

func main() {
	config, err := loadServerConfig()
	if err != nil {
		log.Fatalf("load server config: %v", err)
	}
	var firebaseConfig *firebase.Config
	if projectID := strings.TrimSpace(os.Getenv("FIREBASE_PROJECT_ID")); projectID != "" {
		firebaseConfig = &firebase.Config{ProjectID: projectID}
	}
	app, err := firebase.NewApp(context.Background(), firebaseConfig)
	if err != nil {
		log.Fatalf("initialize Firebase Admin SDK: %v", err)
	}
	authClient, err := app.Auth(context.Background())
	if err != nil {
		log.Fatalf("initialize Firebase Auth: %v", err)
	}
	firestoreClient, err := app.Firestore(context.Background())
	if err != nil {
		log.Fatalf("initialize Firestore: %v", err)
	}
	defer firestoreClient.Close()
	repositories := NewFirestoreRepositories(firestoreClient)
	var fcm *messaging.Client
	if fcm, err = app.Messaging(context.Background()); err != nil {
		log.Printf("FCM disabled: create messaging client: %v", err)
	}
	if fcm == nil {
		log.Println("FCM delivery is disabled; configure Application Default Credentials for the Firebase project")
	}
	s := &chatServer{
		servers: config.Servers,
		rooms:   config.Rooms,
		signals: []CallSignal{}, nextSignalID: 1, pushTokens: make(map[string]string),
		fcm: fcm, profiles: repositories, dms: repositories, messages: NewFirestoreMessageRepository(firestoreClient),
	}
	handler := newHTTPHandler(s, firebaseIDTokenVerifier{client: authClient})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("ChatterBox API listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func newHTTPHandler(s *chatServer, verifier IDTokenVerifier) http.Handler {
	return newHTTPHandlerWithRepositories(s, verifier, s.profiles, s.dms, s.messages)
}

func newHTTPHandlerWithRepositories(s *chatServer, verifier IDTokenVerifier, profiles ProfileRepository, dms DMRepository, messages MessageRepository) http.Handler {
	authenticate := firebaseIDTokenMiddleware(verifier)
	s.profiles, s.dms, s.messages = profiles, dms, messages
	mux := http.NewServeMux()
	mux.Handle("/api/servers", authenticate(http.HandlerFunc(s.handleServers)))
	mux.Handle("/api/rooms", authenticate(http.HandlerFunc(s.handleRooms)))
	mux.Handle("/api/messages", authenticate(http.HandlerFunc(s.handleMessages)))
	mux.Handle("/api/me", authenticate(http.HandlerFunc(s.handleMe)))
	mux.Handle("/api/users", authenticate(http.HandlerFunc(s.handleUsers)))
	mux.Handle("/api/dms", authenticate(http.HandlerFunc(s.handleDMs)))
	mux.Handle("/api/push/register", authenticate(http.HandlerFunc(s.handlePushRegistration)))
	mux.Handle("/api/calls/signals", authenticate(http.HandlerFunc(s.handleCallSignals)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": "ChatterBox API", "health": "/health"})
	})
	return cors(mux)
}

func loadServerConfig() (serverConfig, error) {
	path := os.Getenv("CHATTERBOX_SERVER_CONFIG")
	if path == "" {
		path = "server/config.json"
	}
	file, err := os.Open(path)
	if err != nil {
		return serverConfig{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	var config serverConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return serverConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return serverConfig{}, fmt.Errorf("%s must contain one JSON value", path)
	}
	if len(config.Servers) == 0 || len(config.Rooms) == 0 {
		return serverConfig{}, fmt.Errorf("%s must define at least one server and one room", path)
	}
	serverIDs, roomIDs := map[string]bool{}, map[string]bool{}
	for _, server := range config.Servers {
		if strings.TrimSpace(server.ID) == "" || strings.TrimSpace(server.Name) == "" || serverIDs[server.ID] {
			return serverConfig{}, fmt.Errorf("%s has an empty or duplicate server id/name", path)
		}
		serverIDs[server.ID] = true
	}
	for _, room := range config.Rooms {
		if strings.TrimSpace(room.ID) == "" || strings.TrimSpace(room.Name) == "" || roomIDs[room.ID] || !serverIDs[room.ServerID] {
			return serverConfig{}, fmt.Errorf("%s has an empty/duplicate room or a room with unknown serverId", path)
		}
		roomIDs[room.ID] = true
	}
	return config, nil
}

const maxPushTokenBytes = 4096

func (s *chatServer) handlePushRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	identity, ok := UserIdentityFromContext(r.Context())
	if !ok || strings.TrimSpace(identity.UID) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.Token = strings.TrimSpace(input.Token)
	if input.Token == "" || len(input.Token) > maxPushTokenBytes {
		http.Error(w, "token must be 1 to 4096 bytes", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if r.Method == http.MethodPost {
		if s.pushTokens == nil {
			s.pushTokens = make(map[string]string)
		}
		if owner, exists := s.pushTokens[input.Token]; exists && owner != identity.UID {
			s.mu.Unlock()
			http.Error(w, "push token is already registered", http.StatusConflict)
			return
		}
		s.pushTokens[input.Token] = identity.UID
	} else if s.pushTokens[input.Token] == identity.UID {
		delete(s.pushTokens, input.Token)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *chatServer) pushTokensForOtherUsers(authorUID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tokens := make([]string, 0, len(s.pushTokens))
	for token, uid := range s.pushTokens {
		if uid != authorUID {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func (s *chatServer) notifyOtherDevices(msg Message) {
	if s.fcm == nil {
		return
	}
	tokens := s.pushTokensForOtherUsers(msg.AuthorID)
	if len(tokens) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.fcm.SendEachForMulticast(ctx, &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: msg.Author,
			Body:  msg.Content,
		},
		Data: map[string]string{"roomId": msg.RoomID},
	})
	if err != nil {
		log.Printf("push delivery failed: %v", err)
	}
}

func (s *chatServer) handleCallSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	identity, ok := UserIdentityFromContext(r.Context())
	if !ok || strings.TrimSpace(identity.UID) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		roomID := r.URL.Query().Get("room")
		peerID := r.URL.Query().Get("peer")
		if roomID == "" || !validCallPeerID(peerID) {
			http.Error(w, "room and valid peer are required", http.StatusBadRequest)
			return
		}
		allowed, err := s.roomAllowed(r.Context(), roomID, identity.UID)
		if err != nil {
			http.Error(w, "could not verify conversation membership", http.StatusInternalServerError)
			return
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var after int64
		if rawAfter := r.URL.Query().Get("after"); rawAfter != "" {
			if _, err := fmt.Sscan(rawAfter, &after); err != nil || after < 0 {
				http.Error(w, "invalid after cursor", http.StatusBadRequest)
				return
			}
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		result := make([]CallSignal, 0)
		for _, signal := range s.signals {
			if signal.ID > after && signal.RoomID == roomID && signal.OwnerUID != identity.UID && (signal.To == "" || signal.To == peerID) {
				result = append(result, signal)
			}
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var signal CallSignal
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&signal); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	signal.RoomID, signal.To, signal.Type = strings.TrimSpace(signal.RoomID), strings.TrimSpace(signal.To), strings.TrimSpace(signal.Type)
	if signal.RoomID == "" || !validCallPeerID(signal.From) || signal.Type == "" || (signal.To != "" && !validCallPeerID(signal.To)) {
		http.Error(w, "roomId, valid from peer, and type are required; to must be a valid peer when set", http.StatusBadRequest)
		return
	}
	signal.OwnerUID = identity.UID
	allowed, err := s.roomAllowed(r.Context(), signal.RoomID, identity.UID)
	if err != nil {
		http.Error(w, "could not verify conversation membership", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch signal.Type {
	case "join", "leave", "offer", "answer", "candidate":
	default:
		http.Error(w, "unsupported signal type", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	signal.ID, signal.CreatedAt = s.nextSignalID, time.Now().UTC()
	s.nextSignalID++
	s.signals = append(s.signals, signal)
	if len(s.signals) > 2000 {
		s.signals = append([]CallSignal(nil), s.signals[len(s.signals)-1000:]...)
	}
	writeJSON(w, http.StatusCreated, signal)
}

func validCallPeerID(peerID string) bool {
	if len(peerID) == 0 || len(peerID) > 128 {
		return false
	}
	for _, char := range peerID {
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') &&
			char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

func (s *chatServer) handleRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	serverID := r.URL.Query().Get("server")
	if serverID == "" {
		writeJSON(w, http.StatusOK, s.rooms)
		return
	}
	result := make([]Room, 0)
	for _, room := range s.rooms {
		if room.ServerID == serverID {
			result = append(result, room)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *chatServer) handleServers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.servers)
}

func (s *chatServer) roomAllowed(ctx context.Context, roomID, uid string) (bool, error) {
	for _, room := range s.rooms {
		if room.ID == roomID {
			return true, nil
		}
	}
	if s.dms == nil {
		return false, nil
	}
	return s.dms.IsParticipant(ctx, roomID, uid)
}

func (s *chatServer) handleMe(w http.ResponseWriter, r *http.Request) {
	identity, _ := UserIdentityFromContext(r.Context())
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		if s.profiles == nil {
			http.Error(w, "profile storage unavailable", http.StatusServiceUnavailable)
			return
		}
		profile, err := s.profiles.Get(r.Context(), identity.UID)
		if err != nil {
			http.Error(w, "could not load profile", http.StatusInternalServerError)
			return
		}
		if profile == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, profile.Public())
	case http.MethodPut:
		var input struct {
			DisplayName string `json:"displayName"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		input.DisplayName = strings.TrimSpace(input.DisplayName)
		if len(input.DisplayName) < 1 || len([]rune(input.DisplayName)) > 40 {
			http.Error(w, "displayName must be 1 to 40 characters", http.StatusBadRequest)
			return
		}
		if s.profiles == nil {
			http.Error(w, "profile storage unavailable", http.StatusServiceUnavailable)
			return
		}
		profile := Profile{UID: identity.UID, Email: identity.Email, DisplayName: input.DisplayName, Verified: identity.EmailVerified}
		if err := s.profiles.Put(r.Context(), profile); err != nil {
			http.Error(w, "could not save profile", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, profile.Public())
	default:
		methodNotAllowed(w)
	}
}

func (s *chatServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if len([]rune(query)) > 40 {
		http.Error(w, "query is too long", http.StatusBadRequest)
		return
	}
	if len([]rune(query)) < 2 {
		http.Error(w, "query must be at least 2 characters", http.StatusBadRequest)
		return
	}
	if s.profiles == nil {
		writeJSON(w, http.StatusOK, []ProfileSummary{})
		return
	}
	profiles, err := s.profiles.SearchPrefix(r.Context(), query, 20)
	if err != nil {
		http.Error(w, "could not search profiles", http.StatusInternalServerError)
		return
	}
	identity, _ := UserIdentityFromContext(r.Context())
	result := make([]ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		if profile.UID != identity.UID {
			result = append(result, profile)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *chatServer) handleDMs(w http.ResponseWriter, r *http.Request) {
	identity, _ := UserIdentityFromContext(r.Context())
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		if s.dms == nil {
			writeJSON(w, http.StatusOK, []ConversationResponse{})
			return
		}
		conversations, err := s.dms.List(r.Context(), identity.UID)
		if err != nil {
			http.Error(w, "could not load conversations", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.conversationResponses(r, conversations))
	case http.MethodPost:
		var input struct {
			UserID string `json:"userId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		input.UserID = strings.TrimSpace(input.UserID)
		if input.UserID == "" {
			http.Error(w, "userId is required", http.StatusBadRequest)
			return
		}
		if input.UserID == identity.UID {
			http.Error(w, "cannot create a DM with yourself", http.StatusBadRequest)
			return
		}
		if s.profiles == nil {
			http.Error(w, "profile storage unavailable", http.StatusServiceUnavailable)
			return
		}
		target, err := s.profiles.Get(r.Context(), input.UserID)
		if err != nil {
			http.Error(w, "could not load target profile", http.StatusInternalServerError)
			return
		}
		if target == nil || !target.Verified {
			http.Error(w, "target profile not found", http.StatusNotFound)
			return
		}
		if s.dms == nil {
			http.Error(w, "conversation storage unavailable", http.StatusServiceUnavailable)
			return
		}
		conversation, err := s.dms.CreateOrGet(r.Context(), identity.UID, input.UserID)
		if err != nil {
			http.Error(w, "could not create conversation", http.StatusInternalServerError)
			return
		}
		responses := s.conversationResponses(r, []Conversation{conversation})
		writeJSON(w, http.StatusCreated, responses[0])
	default:
		methodNotAllowed(w)
	}
}

type ConversationResponse struct {
	ID               string    `json:"id"`
	ParticipantIDs   []string  `json:"participantIds"`
	OtherUserID      string    `json:"otherUserId"`
	OtherDisplayName string    `json:"otherDisplayName"`
	CreatedAt        time.Time `json:"createdAt"`
}

func (s *chatServer) conversationResponses(r *http.Request, conversations []Conversation) []ConversationResponse {
	identity, _ := UserIdentityFromContext(r.Context())
	result := make([]ConversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		other := ""
		for _, uid := range conversation.ParticipantIDs {
			if uid != identity.UID {
				other = uid
				break
			}
		}
		displayName := ""
		if s.profiles != nil && other != "" {
			if profile, err := s.profiles.Get(r.Context(), other); err == nil && profile != nil {
				displayName = profile.DisplayName
			}
		}
		result = append(result, ConversationResponse{
			ID: conversation.ID, ParticipantIDs: append([]string(nil), conversation.ParticipantIDs...),
			OtherUserID: other, OtherDisplayName: displayName, CreatedAt: conversation.CreatedAt,
		})
	}
	return result
}

func (s *chatServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	identity, _ := UserIdentityFromContext(r.Context())
	if r.Method == http.MethodGet {
		roomID := r.URL.Query().Get("room")
		if roomID == "" {
			http.Error(w, "room is required", http.StatusBadRequest)
			return
		}
		allowed, err := s.roomAllowed(r.Context(), roomID, identity.UID)
		if err != nil {
			http.Error(w, "could not verify conversation membership", http.StatusInternalServerError)
			return
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if s.messages == nil {
			writeJSON(w, http.StatusOK, []Message{})
			return
		}
		result, err := s.messages.List(r.Context(), roomID, 200)
		if err != nil {
			http.Error(w, "could not load messages", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		RoomID  string `json:"roomId"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.RoomID, input.Content = strings.TrimSpace(input.RoomID), strings.TrimSpace(input.Content)
	if input.RoomID == "" || input.Content == "" {
		http.Error(w, "roomId and content are required", http.StatusBadRequest)
		return
	}
	if len(input.Content) > 2000 {
		http.Error(w, "message must be 2000 characters or fewer", http.StatusBadRequest)
		return
	}
	allowed, err := s.roomAllowed(r.Context(), input.RoomID, identity.UID)
	if err != nil {
		http.Error(w, "could not verify conversation membership", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.profiles == nil {
		http.Error(w, "profile required", http.StatusForbidden)
		return
	}
	profile, err := s.profiles.Get(r.Context(), identity.UID)
	if err != nil {
		http.Error(w, "could not load profile", http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.Error(w, "profile required", http.StatusForbidden)
		return
	}
	if s.messages == nil {
		http.Error(w, "message storage unavailable", http.StatusServiceUnavailable)
		return
	}
	msg := Message{RoomID: input.RoomID, Author: profile.DisplayName, AuthorID: identity.UID, Content: input.Content, CreatedAt: time.Now().UTC()}
	msg, err = s.messages.Append(r.Context(), msg)
	if err != nil {
		http.Error(w, "could not save message", http.StatusInternalServerError)
		return
	}
	go s.notifyOtherDevices(msg)
	writeJSON(w, http.StatusCreated, msg)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
