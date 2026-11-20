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
	ID        int64     `json:"id"`
	RoomID    string    `json:"roomId"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type CallSignal struct {
	ID        int64     `json:"id"`
	RoomID    string    `json:"roomId"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Type      string    `json:"type"`
	SDP       string    `json:"sdp,omitempty"`
	Candidate string    `json:"candidate,omitempty"`
	SDPMid    *string   `json:"sdpMid,omitempty"`
	SDPMLine  *int      `json:"sdpMLineIndex,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type chatServer struct {
	db  *store
	fcm *messaging.Client

	// Call signaling is short-lived, so it stays in memory.
	mu           sync.RWMutex
	signals      []CallSignal
	nextSignalID int64
}

func main() {
	config, err := loadServerConfig()
	if err != nil {
		log.Fatalf("load server config: %v", err)
	}
	dbPath := os.Getenv("CHATTERBOX_DB")
	if dbPath == "" {
		dbPath = "chatterbox.db"
	}
	db, err := openStore(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seeded, err := db.seedIfEmpty(config, welcomeMessages())
	if err != nil {
		log.Fatalf("seed database: %v", err)
	}
	if seeded {
		log.Printf("initialized new database %s from server config", dbPath)
	} else {
		log.Printf("using existing database %s (server config is only read for a new database)", dbPath)
	}
	var fcm *messaging.Client
	if app, err := firebase.NewApp(context.Background(), nil); err != nil {
		log.Printf("FCM disabled: initialize Firebase Admin SDK: %v", err)
	} else if fcm, err = app.Messaging(context.Background()); err != nil {
		log.Printf("FCM disabled: create messaging client: %v", err)
	}
	if fcm == nil {
		log.Println("FCM delivery is disabled; configure Application Default Credentials for the Firebase project")
	}
	s := &chatServer{db: db, fcm: fcm, nextSignalID: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/servers", s.handleServers)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/api/push/register", s.handlePushRegistration)
	mux.HandleFunc("/api/calls/signals", s.handleCallSignals)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.db.PingContext(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": "ChatterBox API", "health": "/health"})
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("ChatterBox API listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, cors(mux)))
}

// welcomeMessages are inserted once, when a new database is created.
func welcomeMessages() []Message {
	at := func(minutesAgo int) time.Time { return time.Now().Add(-time.Duration(minutesAgo) * time.Minute) }
	return []Message{
		{RoomID: "general", Author: "Maya", Content: "Welcome to ChatterBox! This is your space to hang out.", CreatedAt: at(35)},
		{RoomID: "general", Author: "Leo", Content: "A simple chat app, ready for your ideas ✨", CreatedAt: at(32)},
		{RoomID: "general", Author: "Maya", Content: "Pick a channel on the left and say hello.", CreatedAt: at(29)},
		{RoomID: "test-general", Author: "Riley", Content: "Welcome to the Test Server! Try chatting here.", CreatedAt: at(12)},
		{RoomID: "test-projects", Author: "Sam", Content: "Use this channel to share what you’re building.", CreatedAt: at(8)},
		{RoomID: "dm-maya", Author: "Maya Chen", Content: "Hey! Welcome to ChatterBox. 👋", CreatedAt: at(18)},
		{RoomID: "dm-maya", Author: "You", Content: "Thanks, glad to be here!", CreatedAt: at(16)},
		{RoomID: "dm-leo", Author: "Leo Park", Content: "Want to try the voice channels later?", CreatedAt: at(9)},
	}
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
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

func (s *chatServer) handlePushRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Token string `json:"token"`
		User  string `json:"user"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.Token, input.User = strings.TrimSpace(input.Token), strings.TrimSpace(input.User)
	if input.Token == "" || input.User == "" {
		http.Error(w, "token and user are required", http.StatusBadRequest)
		return
	}
	if err := s.db.savePushToken(input.Token, input.User); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *chatServer) notifyOtherDevices(msg Message) {
	if s.fcm == nil {
		return
	}
	tokens, err := s.db.pushTokensExcept(msg.Author)
	if err != nil {
		log.Printf("load push tokens: %v", err)
		return
	}
	if len(tokens) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := s.fcm.SendEachForMulticast(ctx, &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: msg.Author,
			Body:  msg.Content,
		},
		Data: map[string]string{"roomId": msg.RoomID},
	})
	if err != nil {
		log.Printf("push delivery failed: %v", err)
		return
	}
	var stale []string
	for i, r := range resp.Responses {
		if r.Error != nil && messaging.IsUnregistered(r.Error) {
			stale = append(stale, tokens[i])
		}
	}
	if err := s.db.deletePushTokens(stale); err != nil {
		log.Printf("delete stale push tokens: %v", err)
	}
}

func (s *chatServer) handleCallSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet {
		roomID, peerID := r.URL.Query().Get("room"), r.URL.Query().Get("peer")
		if roomID == "" || peerID == "" {
			http.Error(w, "room and peer are required", http.StatusBadRequest)
			return
		}
		var after int64
		if _, err := fmt.Sscan(r.URL.Query().Get("after"), &after); err != nil {
			after = 0
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		result := make([]CallSignal, 0)
		for _, signal := range s.signals {
			if signal.ID > after && signal.RoomID == roomID && signal.From != peerID && (signal.To == "" || signal.To == peerID) {
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
	signal.RoomID, signal.From, signal.To, signal.Type = strings.TrimSpace(signal.RoomID), strings.TrimSpace(signal.From), strings.TrimSpace(signal.To), strings.TrimSpace(signal.Type)
	if signal.RoomID == "" || signal.From == "" || signal.Type == "" {
		http.Error(w, "roomId, from, and type are required", http.StatusBadRequest)
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

func (s *chatServer) handleRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rooms, err := s.db.rooms(r.URL.Query().Get("server"))
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rooms)
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
	servers, err := s.db.servers()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (s *chatServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet {
		roomID := r.URL.Query().Get("room")
		if roomID == "" {
			http.Error(w, "room is required", http.StatusBadRequest)
			return
		}
		messages, err := s.db.messages(roomID)
		if err != nil {
			internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messages)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		RoomID  string `json:"roomId"`
		Author  string `json:"author"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.RoomID, input.Author, input.Content = strings.TrimSpace(input.RoomID), strings.TrimSpace(input.Author), strings.TrimSpace(input.Content)
	if input.RoomID == "" || input.Author == "" || input.Content == "" {
		http.Error(w, "roomId, author, and content are required", http.StatusBadRequest)
		return
	}
	if len(input.Content) > 2000 {
		http.Error(w, "message must be 2000 characters or fewer", http.StatusBadRequest)
		return
	}
	validRoom, err := s.db.roomExists(input.RoomID)
	if err != nil {
		internalError(w, err)
		return
	}
	// Direct messages use this endpoint too, but their IDs are contact-based
	// (for example, "dm-maya") and are not part of the room configuration.
	if strings.HasPrefix(input.RoomID, "dm-") && len(strings.TrimPrefix(input.RoomID, "dm-")) > 0 {
		validRoom = true
	}
	if !validRoom {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	msg, err := s.db.addMessage(input.RoomID, input.Author, input.Content)
	if err != nil {
		internalError(w, err)
		return
	}
	go s.notifyOtherDevices(msg)
	writeJSON(w, http.StatusCreated, msg)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost, http.MethodOptions}, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
