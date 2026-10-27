package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
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

// Direct-message conversations are not configured rooms; the client addresses them as dm-<contactId>.
var directMessageID = regexp.MustCompile(`^dm-[a-z0-9-]{1,64}$`)

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
	mu           sync.RWMutex
	servers      []Server
	rooms        []Room
	messages     []Message
	nextID       int64
	signals      []CallSignal
	nextSignalID int64
	pushTokens   map[string]string
	fcm          *messaging.Client
}

func main() {
	config, err := loadServerConfig()
	if err != nil {
		log.Fatalf("load server config: %v", err)
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
	s := &chatServer{
		servers: config.Servers,
		rooms:   config.Rooms,
		messages: []Message{
			{1, "general", "Maya", "Welcome to ChatterBox! This is your space to hang out.", time.Now().Add(-35 * time.Minute)},
			{2, "general", "Leo", "A simple chat app, ready for your ideas ✨", time.Now().Add(-32 * time.Minute)},
			{3, "general", "Maya", "Pick a channel on the left and say hello.", time.Now().Add(-29 * time.Minute)},
			{4, "test-general", "Riley", "Welcome to the Test Server! Try chatting here.", time.Now().Add(-12 * time.Minute)},
			{5, "test-projects", "Sam", "Use this channel to share what you’re building.", time.Now().Add(-8 * time.Minute)},
			{6, "dm-maya", "Maya Chen", "Hey! Welcome to ChatterBox. 👋", time.Now().Add(-18 * time.Minute)},
			{7, "dm-maya", "You", "Thanks, glad to be here!", time.Now().Add(-16 * time.Minute)},
			{8, "dm-leo", "Leo Park", "Want to try the voice channels later?", time.Now().Add(-9 * time.Minute)},
		}, nextID: 9, nextSignalID: 1, pushTokens: make(map[string]string), fcm: fcm,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/servers", s.handleServers)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/api/push/register", s.handlePushRegistration)
	mux.HandleFunc("/api/calls/signals", s.handleCallSignals)
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
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("ChatterBox API listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, cors(mux)))
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
	s.mu.Lock()
	s.pushTokens[input.Token] = input.User
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *chatServer) notifyOtherDevices(msg Message) {
	if s.fcm == nil {
		return
	}
	s.mu.RLock()
	tokens := make([]string, 0, len(s.pushTokens))
	for token, user := range s.pushTokens {
		if user != msg.Author {
			tokens = append(tokens, token)
		}
	}
	s.mu.RUnlock()
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
		s.mu.RLock()
		defer s.mu.RUnlock()
		result := make([]Message, 0)
		for _, msg := range s.messages {
			if msg.RoomID == roomID {
				result = append(result, msg)
			}
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
	s.mu.Lock()
	validRoom := directMessageID.MatchString(input.RoomID)
	for _, room := range s.rooms {
		if room.ID == input.RoomID {
			validRoom = true
			break
		}
	}
	if !validRoom {
		s.mu.Unlock()
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	msg := Message{s.nextID, input.RoomID, input.Author, input.Content, time.Now().UTC()}
	s.nextID++
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
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
