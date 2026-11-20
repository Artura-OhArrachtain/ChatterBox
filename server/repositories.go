package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Profile struct {
	UID         string `json:"uid"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName"`
	Verified    bool   `json:"-"`
}

type ProfileSummary struct {
	UID         string `json:"uid"`
	DisplayName string `json:"displayName"`
}

func (p Profile) Public() map[string]any {
	return map[string]any{"uid": p.UID, "email": p.Email, "displayName": p.DisplayName}
}

type ProfileRepository interface {
	Get(ctx context.Context, uid string) (*Profile, error)
	Put(ctx context.Context, profile Profile) error
	SearchPrefix(ctx context.Context, prefix string, limit int) ([]ProfileSummary, error)
}

type Conversation struct {
	ID             string    `json:"id"`
	ParticipantIDs []string  `json:"participantIds"`
	CreatedAt      time.Time `json:"createdAt"`
}

type DMRepository interface {
	CreateOrGet(ctx context.Context, uidA, uidB string) (Conversation, error)
	List(ctx context.Context, uid string) ([]Conversation, error)
	IsParticipant(ctx context.Context, conversationID, uid string) (bool, error)
}

type MessageRepository interface {
	List(ctx context.Context, roomID string, limit int) ([]Message, error)
	Append(ctx context.Context, message Message) (Message, error)
}

type firestoreRepositories struct {
	client *firestore.Client
}

func NewFirestoreRepositories(client *firestore.Client) *firestoreRepositories {
	return &firestoreRepositories{client: client}
}

func NewFirestoreMessageRepository(client *firestore.Client) *firestoreMessageRepository {
	return &firestoreMessageRepository{client: client}
}

type firestoreMessageRepository struct {
	client *firestore.Client
}

func (r *firestoreRepositories) Get(ctx context.Context, uid string) (*Profile, error) {
	doc, err := r.client.Collection("profiles").Doc(uid).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	data := doc.Data()
	profile := &Profile{
		UID:         uid,
		Email:       stringValue(data["email"]),
		DisplayName: stringValue(data["displayName"]),
		Verified:    boolValue(data["verified"]),
	}
	return profile, nil
}

func (r *firestoreRepositories) Put(ctx context.Context, profile Profile) error {
	_, err := r.client.Collection("profiles").Doc(profile.UID).Set(ctx, map[string]any{
		"uid":              profile.UID,
		"email":            profile.Email,
		"displayName":      profile.DisplayName,
		"displayNameLower": strings.ToLower(profile.DisplayName),
		"verified":         profile.Verified,
		"updatedAt":        time.Now().UTC(),
	})
	return err
}

func (r *firestoreRepositories) SearchPrefix(ctx context.Context, prefix string, limit int) ([]ProfileSummary, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}
	lower := strings.ToLower(strings.TrimSpace(prefix))
	query := r.client.Collection("profiles").
		Where("displayNameLower", ">=", lower)
	if lower != "" {
		query = query.Where("displayNameLower", "<", lower+"\uf8ff")
	}
	query = query.OrderBy("displayNameLower", firestore.Asc).Limit(limit * 3)
	iter := query.Documents(ctx)
	defer iter.Stop()
	result := make([]ProfileSummary, 0, limit)
	for len(result) < limit {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		if !boolValue(data["verified"]) {
			continue
		}
		result = append(result, ProfileSummary{UID: doc.Ref.ID, DisplayName: stringValue(data["displayName"])})
	}
	return result, nil
}

func stableConversationID(uidA, uidB string) string {
	ids := []string{uidA, uidB}
	if ids[1] < ids[0] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	sum := sha256.Sum256([]byte(ids[0] + "\x00" + ids[1]))
	return "dm-" + hex.EncodeToString(sum[:])[:32]
}

func (r *firestoreRepositories) CreateOrGet(ctx context.Context, uidA, uidB string) (Conversation, error) {
	id := stableConversationID(uidA, uidB)
	ref := r.client.Collection("dms").Doc(id)
	doc, err := ref.Get(ctx)
	if err == nil {
		return conversationFromSnapshot(doc), nil
	}
	if status.Code(err) != codes.NotFound {
		return Conversation{}, err
	}
	now := time.Now().UTC()
	ids := []string{uidA, uidB}
	if ids[1] < ids[0] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	_, err = ref.Set(ctx, map[string]any{
		"id": id, "participantIds": ids, "createdAt": now,
	})
	if err != nil {
		return Conversation{}, err
	}
	return Conversation{ID: id, ParticipantIDs: ids, CreatedAt: now}, nil
}

func (r *firestoreRepositories) List(ctx context.Context, uid string) ([]Conversation, error) {
	iter := r.client.Collection("dms").
		Where("participantIds", "array-contains", uid).
		OrderBy("createdAt", firestore.Desc).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()
	result := make([]Conversation, 0)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		result = append(result, conversationFromSnapshot(doc))
	}
	return result, nil
}

func (r *firestoreRepositories) IsParticipant(ctx context.Context, conversationID, uid string) (bool, error) {
	doc, err := r.client.Collection("dms").Doc(conversationID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, err
	}
	conversation := conversationFromSnapshot(doc)
	for _, participant := range conversation.ParticipantIDs {
		if participant == uid {
			return true, nil
		}
	}
	return false, nil
}

func conversationFromSnapshot(doc *firestore.DocumentSnapshot) Conversation {
	data := doc.Data()
	ids := make([]string, 0)
	switch raw := data["participantIds"].(type) {
	case []string:
		ids = append(ids, raw...)
	case []any:
		for _, value := range raw {
			if id, ok := value.(string); ok {
				ids = append(ids, id)
			}
		}
	}
	createdAt := time.Time{}
	if value, ok := data["createdAt"].(time.Time); ok {
		createdAt = value
	}
	return Conversation{ID: doc.Ref.ID, ParticipantIDs: ids, CreatedAt: createdAt}
}

func (r *firestoreMessageRepository) List(ctx context.Context, roomID string, limit int) ([]Message, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	iter := r.client.Collection("messages").
		Where("roomId", "==", roomID).
		OrderBy("createdAt", firestore.Desc).
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()
	result := make([]Message, 0)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		result = append(result, messageFromSnapshot(doc))
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result, nil
}

func (r *firestoreMessageRepository) Append(ctx context.Context, message Message) (Message, error) {
	ref := r.client.Collection("messages").NewDoc()
	message.ID = ref.ID
	message.CreatedAt = time.Now().UTC()
	_, err := ref.Create(ctx, map[string]any{
		"roomId":    message.RoomID,
		"author":    message.Author,
		"authorId":  message.AuthorID,
		"content":   message.Content,
		"createdAt": message.CreatedAt,
	})
	if err != nil {
		return Message{}, err
	}
	return message, nil
}

func messageFromSnapshot(doc *firestore.DocumentSnapshot) Message {
	data := doc.Data()
	createdAt := time.Time{}
	if value, ok := data["createdAt"].(time.Time); ok {
		createdAt = value
	}
	return Message{
		ID:        doc.Ref.ID,
		RoomID:    stringValue(data["roomId"]),
		Author:    stringValue(data["author"]),
		AuthorID:  stringValue(data["authorId"]),
		Content:   stringValue(data["content"]),
		CreatedAt: createdAt,
	}
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}
