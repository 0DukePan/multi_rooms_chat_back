package db

import (
	"context"
	"fmt"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/models"
	"github.com/google/uuid"
)

// User queries
func (db *Database) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	var user models.User
	err := db.pool.QueryRow(ctx,
		`SELECT id, username, email, avatar_url, status, last_seen, created_at 
		 FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.AvatarURL, &user.Status, &user.LastSeen, &user.CreatedAt)
	return &user, err
}

func (db *Database) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := db.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, avatar_url, status, last_seen, created_at 
		 FROM users WHERE username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.AvatarURL, &user.Status, &user.LastSeen, &user.CreatedAt)
	return &user, err
}

func (db *Database) CreateUser(ctx context.Context, username, email, passwordHash string) (*models.User, error) {
	user := &models.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Status:       "offline",
	}
	_, err := db.pool.Exec(ctx,
		`INSERT INTO users (id, username, email, password_hash, status) VALUES ($1, $2, $3, $4, $5)`,
		user.ID, user.Username, user.Email, user.PasswordHash, user.Status,
	)
	return user, err
}

func (db *Database) UpdateUserStatus(ctx context.Context, userID uuid.UUID, status string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET status = $1, last_seen = NOW() WHERE id = $2`,
		status, userID,
	)
	return err
}

// Room queries
func (db *Database) GetRoomByID(ctx context.Context, roomID uuid.UUID) (*models.Room, error) {
	var room models.Room
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, type, creator_id, topic, is_archived, created_at 
		 FROM rooms WHERE id = $1`,
		roomID,
	).Scan(&room.ID, &room.Name, &room.Type, &room.CreatorID, &room.Topic, &room.IsArchived, &room.CreatedAt)
	return &room, err
}

func (db *Database) GetRoomsByUser(ctx context.Context, userID uuid.UUID) ([]models.Room, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT r.id, r.name, r.type, r.creator_id, r.topic, r.is_archived, r.created_at 
		 FROM rooms r 
		 INNER JOIN room_members rm ON r.id = rm.room_id 
		 WHERE rm.user_id = $1 AND r.is_archived = false
		 ORDER BY r.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []models.Room
	for rows.Next() {
		var room models.Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Type, &room.CreatorID, &room.Topic, &room.IsArchived, &room.CreatedAt); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

func (db *Database) CreateRoom(ctx context.Context, name, roomType string, creatorID uuid.UUID) (*models.Room, error) {
	room := &models.Room{
		ID:        uuid.New(),
		Name:      name,
		Type:      roomType,
		CreatorID: creatorID,
	}
	_, err := db.pool.Exec(ctx,
		`INSERT INTO rooms (id, name, type, creator_id) VALUES ($1, $2, $3, $4)`,
		room.ID, room.Name, room.Type, room.CreatorID,
	)
	if err == nil {
		// Add creator as admin
		db.AddRoomMember(ctx, room.ID, creatorID, "admin")
	}
	return room, err
}

// Room member queries
func (db *Database) AddRoomMember(ctx context.Context, roomID, userID uuid.UUID, role string) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (room_id, user_id) DO NOTHING`,
		roomID, userID, role,
	)
	return err
}

func (db *Database) RemoveRoomMember(ctx context.Context, roomID, userID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`,
		roomID, userID,
	)
	return err
}

func (db *Database) IsRoomMember(ctx context.Context, roomID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`,
		roomID, userID,
	).Scan(&exists)
	return exists, err
}

// Message queries
func (db *Database) GetMessageByID(ctx context.Context, messageID int64) (*models.Message, error) {
	var msg models.Message
	err := db.pool.QueryRow(ctx,
		`SELECT id, room_id, user_id, content, message_type, file_url, parent_id, edited_at, deleted_at, created_at 
		 FROM messages WHERE id = $1 AND deleted_at IS NULL`,
		messageID,
	).Scan(&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.MessageType, &msg.FileURL, &msg.ParentID, &msg.EditedAt, &msg.DeletedAt, &msg.CreatedAt)
	return &msg, err
}

func (db *Database) GetRoomMessages(ctx context.Context, roomID uuid.UUID, limit int, before int64) ([]models.Message, error) {
	query := `SELECT id, room_id, user_id, content, message_type, file_url, parent_id, edited_at, deleted_at, created_at 
	          FROM messages 
	          WHERE room_id = $1 AND deleted_at IS NULL`
	args := []interface{}{roomID}

	if before > 0 {
		query += ` AND id < $2`
		args = append(args, before)
	}

	query += ` ORDER BY created_at DESC LIMIT $` + string(rune(len(args)+1))
	args = append(args, limit)

	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.MessageType, &msg.FileURL, &msg.ParentID, &msg.EditedAt, &msg.DeletedAt, &msg.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (db *Database) CreateMessage(ctx context.Context, msg *models.Message) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO messages (room_id, user_id, content, message_type, file_url, parent_id) 
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		msg.RoomID, msg.UserID, msg.Content, msg.MessageType, msg.FileURL, msg.ParentID,
	).Scan(&msg.ID, &msg.CreatedAt)
}

// SearchMessages searches messages in a room with enhanced filtering and ranking
func (db *Database) SearchMessages(ctx context.Context, roomID uuid.UUID, query string, limit int, senderID *uuid.UUID, beforeTime *time.Time, afterTime *time.Time) ([]models.Message, error) {
	// Use ts_rank for relevance ordering
	baseQuery := `SELECT id, room_id, user_id, content, message_type, file_url, parent_id, edited_at, deleted_at, created_at 
	              FROM messages 
	              WHERE room_id = $1 AND deleted_at IS NULL AND tsv @@ plainto_tsquery('english', $2)`
	args := []interface{}{roomID, query}

	paramIndex := 3

	if senderID != nil {
		baseQuery += fmt.Sprintf(` AND user_id = $%d`, paramIndex)
		args = append(args, *senderID)
		paramIndex++
	}

	if beforeTime != nil {
		baseQuery += fmt.Sprintf(` AND created_at < $%d`, paramIndex)
		args = append(args, *beforeTime)
		paramIndex++
	}

	if afterTime != nil {
		baseQuery += fmt.Sprintf(` AND created_at > $%d`, paramIndex)
		args = append(args, *afterTime)
		paramIndex++
	}

	// Order by relevance and then by creation date
	baseQuery += fmt.Sprintf(` ORDER BY ts_rank(tsv, plainto_tsquery('english', $2)) DESC, created_at DESC LIMIT $%d`, paramIndex)
	args = append(args, limit)

	rows, err := db.pool.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.MessageType, &msg.FileURL, &msg.ParentID, &msg.EditedAt, &msg.DeletedAt, &msg.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// Read receipt queries
func (db *Database) MarkMessageRead(ctx context.Context, messageID int64, userID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO message_reads (message_id, user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		messageID, userID,
	)
	return err
}

// EditMessage updates the content of a message.
func (db *Database) EditMessage(ctx context.Context, messageID int64, userID uuid.UUID, newContent string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE messages SET content = $1, edited_at = NOW() WHERE id = $2 AND user_id = $3 AND deleted_at IS NULL`,
		newContent, messageID, userID,
	)
	return err
}

// SoftDeleteMessage marks a message as deleted.
func (db *Database) SoftDeleteMessage(ctx context.Context, messageID int64, userID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE messages SET deleted_at = NOW() WHERE id = $1 AND user_id = $2`,
		messageID, userID,
	)
	return err
}

func (db *Database) GetMessageReads(ctx context.Context, messageID int64) ([]models.MessageRead, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT message_id, user_id, read_at FROM message_reads WHERE message_id = $1`,
		messageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reads []models.MessageRead
	for rows.Next() {
		var read models.MessageRead
		if err := rows.Scan(&read.MessageID, &read.UserID, &read.ReadAt); err != nil {
			return nil, err
		}
		reads = append(reads, read)
	}
	return reads, rows.Err()
}

// Reaction queries
func (db *Database) AddMessageReaction(ctx context.Context, messageID int64, userID uuid.UUID, emoji string) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)
		 ON CONFLICT (message_id, user_id, emoji) DO NOTHING`,
		messageID, userID, emoji,
	)
	return err
}

func (db *Database) RemoveMessageReaction(ctx context.Context, messageID int64, userID uuid.UUID, emoji string) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
		messageID, userID, emoji,
	)
	return err
}

func (db *Database) GetMessageReactions(ctx context.Context, messageID int64) ([]models.Reaction, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT message_id, user_id, emoji, created_at FROM reactions WHERE message_id = $1`,
		messageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reactions []models.Reaction
	for rows.Next() {
		var reaction models.Reaction
		if err := rows.Scan(&reaction.MessageID, &reaction.UserID, &reaction.Emoji, &reaction.CreatedAt); err != nil {
			return nil, err
		}
		reactions = append(reactions, reaction)
	}
	return reactions, rows.Err()
}
