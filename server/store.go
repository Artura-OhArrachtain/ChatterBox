package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// store persists ChatterBox data in a single SQLite file.
type store struct {
	db *sql.DB
}

// migrations are applied in order; PRAGMA user_version records how many ran.
var migrations = []string{
	`CREATE TABLE servers (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		position   INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	CREATE TABLE rooms (
		id         TEXT PRIMARY KEY,
		server_id  TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		position   INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	CREATE INDEX rooms_server ON rooms(server_id, position);
	CREATE TABLE messages (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		room_id    TEXT NOT NULL,
		author     TEXT NOT NULL,
		content    TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX messages_room ON messages(room_id, id);
	CREATE TABLE push_tokens (
		token      TEXT PRIMARY KEY,
		user_name  TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
}

func openStore(path string) (*store, error) {
	// _txlock=immediate avoids SQLITE_BUSY upgrades inside write transactions.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	s := &store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) Close() error { return s.db.Close() }

func (s *store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

// seedIfEmpty loads servers/rooms from config and the welcome messages, but
// only on a brand-new database. After that the database is the source of truth.
func (s *store) seedIfEmpty(config serverConfig, welcome []Message) (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM servers`).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ts := now()
	for i, server := range config.Servers {
		if _, err := tx.Exec(`INSERT INTO servers (id, name, position, created_at) VALUES (?, ?, ?, ?)`,
			server.ID, server.Name, i, ts); err != nil {
			return false, err
		}
	}
	for i, room := range config.Rooms {
		if _, err := tx.Exec(`INSERT INTO rooms (id, server_id, name, position, created_at) VALUES (?, ?, ?, ?, ?)`,
			room.ID, room.ServerID, room.Name, i, ts); err != nil {
			return false, err
		}
	}
	for _, msg := range welcome {
		if _, err := tx.Exec(`INSERT INTO messages (room_id, author, content, created_at) VALUES (?, ?, ?, ?)`,
			msg.RoomID, msg.Author, msg.Content, msg.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (s *store) servers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT id, name FROM servers ORDER BY position, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Server, 0)
	for rows.Next() {
		var server Server
		if err := rows.Scan(&server.ID, &server.Name); err != nil {
			return nil, err
		}
		result = append(result, server)
	}
	return result, rows.Err()
}

// rooms returns all rooms, or only those for serverID when it is non-empty.
func (s *store) rooms(serverID string) ([]Room, error) {
	query := `SELECT id, name, server_id FROM rooms`
	var args []any
	if serverID != "" {
		query += ` WHERE server_id = ?`
		args = append(args, serverID)
	}
	query += ` ORDER BY position, created_at`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Room, 0)
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.ServerID); err != nil {
			return nil, err
		}
		result = append(result, room)
	}
	return result, rows.Err()
}

func (s *store) roomExists(id string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM rooms WHERE id = ?)`, id).Scan(&exists)
	return exists, err
}

func (s *store) messages(roomID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, room_id, author, content, created_at FROM messages WHERE room_id = ? ORDER BY id`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Message, 0)
	for rows.Next() {
		var msg Message
		var created string
		if err := rows.Scan(&msg.ID, &msg.RoomID, &msg.Author, &msg.Content, &created); err != nil {
			return nil, err
		}
		msg.CreatedAt = parseTime(created)
		result = append(result, msg)
	}
	return result, rows.Err()
}

func (s *store) addMessage(roomID, author, content string) (Message, error) {
	msg := Message{RoomID: roomID, Author: author, Content: content, CreatedAt: time.Now().UTC()}
	res, err := s.db.Exec(`INSERT INTO messages (room_id, author, content, created_at) VALUES (?, ?, ?, ?)`,
		roomID, author, content, msg.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Message{}, err
	}
	msg.ID, err = res.LastInsertId()
	return msg, err
}

func (s *store) savePushToken(token, user string) error {
	_, err := s.db.Exec(`INSERT INTO push_tokens (token, user_name, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET user_name = excluded.user_name, updated_at = excluded.updated_at`,
		token, user, now())
	return err
}

// pushTokensExcept returns registered tokens whose user is not excludeUser.
func (s *store) pushTokensExcept(excludeUser string) ([]string, error) {
	rows, err := s.db.Query(`SELECT token FROM push_tokens WHERE user_name <> ?`, excludeUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// deletePushTokens removes tokens FCM reported as no longer valid.
func (s *store) deletePushTokens(tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tokens)), ",")
	args := make([]any, len(tokens))
	for i, t := range tokens {
		args[i] = t
	}
	_, err := s.db.Exec(`DELETE FROM push_tokens WHERE token IN (`+placeholders+`)`, args...)
	return err
}
