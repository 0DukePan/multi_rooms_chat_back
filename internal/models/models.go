package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the chat system
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // Don't expose password hash
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Status       string    `json:"status"` // online, offline, away
	LastSeen     time.Time `json:"last_seen"`
	CreatedAt    time.Time `json:"created_at"`
}

// Room represents a chat room
type Room struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // public, private, group, dm
	CreatorID  uuid.UUID `json:"creator_id"`
	Topic      string    `json:"topic,omitempty"`
	IsArchived bool      `json:"is_archived"`
	CreatedAt  time.Time `json:"created_at"`
}

// RoomMember represents a user's membership in a room
type RoomMember struct {
	RoomID    uuid.UUID `json:"room_id"`
	UserID    uuid.UUID `json:"user_id"`
	Role      string    `json:"role"` // admin, member
	JoinedAt  time.Time `json:"joined_at"`
}

// Message represents a chat message
type Message struct {
	ID          int64     `json:"id"`
	RoomID      uuid.UUID `json:"room_id"`
	UserID      uuid.UUID `json:"user_id"`
	Content     string    `json:"content"`	
	MessageType string    `json:"message_type"` // text, image, file
	FileURL     string    `json:"file_url,omitempty"`
	ParentID    *int64    `json:"parent_id,omitempty"` // For threading
	EditedAt    *time.Time `json:"edited_at,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// MessageRead represents a read receipt for a message
type MessageRead struct {
	MessageID int64     `json:"message_id"`
	UserID    uuid.UUID `json:"user_id"`
	ReadAt    time.Time `json:"read_at"`
}

// Reaction represents a message reaction
type Reaction struct {
	MessageID int64     `json:"message_id"`
	UserID    uuid.UUID `json:"user_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"` // Added for reaction timestamp
}

// WebSocket events
type WSMessage struct {
	Type    string          `json:"type"` // message, typing, read, join, leave
	RoomID  uuid.UUID       `json:"room_id"`
	UserID  uuid.UUID       `json:"user_id"`
	Content string          `json:"content"`
	Data    interface{}     `json:"data"`
}

// HistoryMessage includes user info with message
type HistoryMessage struct {
	*Message
	User *User `json:"user"`
}
