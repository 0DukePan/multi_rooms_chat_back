package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
)

// CreateRoomRequest represents a create room request
type CreateRoomRequest struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // public, private, group
	Topic string `json:"topic"`
}

// CreateRoomHandler creates a new room
func (r *Router) CreateRoomHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var createReq CreateRoomRequest
	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate room type
	if createReq.Type != "public" && createReq.Type != "private" && createReq.Type != "group" {
		http.Error(w, "Invalid room type", http.StatusBadRequest)
		return
	}

	// Create room
	room, err := r.db.CreateRoom(req.Context(), createReq.Name, createReq.Type, userID)
	if err != nil {
		http.Error(w, "Failed to create room", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(room)
}

// GetRoomsHandler retrieves all rooms for the user
func (r *Router) GetRoomsHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	rooms, err := r.db.GetRoomsByUser(req.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to fetch rooms", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if rooms == nil {
		rooms = make([]models.Room, 0)
	}
	json.NewEncoder(w).Encode(rooms)
}

// GetRoomHandler retrieves a single room by ID
func (r *Router) GetRoomHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	// Check membership
	isMember, err := r.db.IsRoomMember(req.Context(), roomID, userID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	room, err := r.db.GetRoomByID(req.Context(), roomID)
	if err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(room)
}

// GetRoomMessagesHandler retrieves messages from a room (paginated)
func (r *Router) GetRoomMessagesHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	// Check membership
	isMember, err := r.db.IsRoomMember(req.Context(), roomID, userID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	// Parse query parameters
	limitStr := req.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	beforeStr := req.URL.Query().Get("before")
	before := int64(0)
	if beforeStr != "" {
		if b, err := strconv.ParseInt(beforeStr, 10, 64); err == nil {
			before = b
		}
	}

	messages, err := r.db.GetRoomMessages(req.Context(), roomID, limit, before)
	if err != nil {
		http.Error(w, "Failed to fetch messages", http.StatusInternalServerError)
		return
	}

	// Enrich messages with user info
	enrichedMessages := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		user, _ := r.db.GetUserByID(req.Context(), msg.UserID)
		enrichedMessages[i] = map[string]interface{}{
			"id":         msg.ID,
			"room_id":    msg.RoomID,
			"user":       user,
			"content":    msg.Content,
			"type":       msg.MessageType,
			"file_url":   msg.FileURL,
			"created_at": msg.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(enrichedMessages)
}

// SearchMessagesHandler searches messages in a room
func (r *Router) SearchMessagesHandler(w http.ResponseWriter, req *http.Request) {
	userID, err := getUserIDFromContext(req.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	// Check membership
	isMember, err := r.db.IsRoomMember(req.Context(), roomID, userID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	query := req.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing search query", http.StatusBadRequest)
		return
	}

	// Parse optional query parameters
	limitStr := req.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	senderIDStr := req.URL.Query().Get("sender_id")
	var senderID *uuid.UUID
	if senderIDStr != "" {
		sID, err := uuid.Parse(senderIDStr)
		if err != nil {
			http.Error(w, "Invalid sender_id", http.StatusBadRequest)
			return
		}
		senderID = &sID
	}

	beforeTimeStr := req.URL.Query().Get("before_time")
	var beforeTime *time.Time
	if beforeTimeStr != "" {
		t, err := time.Parse(time.RFC3339, beforeTimeStr)
		if err != nil {
			http.Error(w, "Invalid before_time format", http.StatusBadRequest)
			return
		}
		beforeTime = &t
	}

	afterTimeStr := req.URL.Query().Get("after_time")
	var afterTime *time.Time
	if afterTimeStr != "" {
		t, err := time.Parse(time.RFC3339, afterTimeStr)
		if err != nil {
			http.Error(w, "Invalid after_time format", http.StatusBadRequest)
			return
		}
		afterTime = &t
	}

	messages, err := r.db.SearchMessages(req.Context(), roomID, query, limit, senderID, beforeTime, afterTime)
	if err != nil {
		http.Error(w, "Failed to search messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if messages == nil {
		messages = make([]models.Message, 0)
	}
	json.NewEncoder(w).Encode(messages)
}

// EditMessageRequest represents an edit message request
type EditMessageRequest struct {
	Content string `json:"content"`
}

// EditMessageHandler handles message editing
func (r *Router) EditMessageHandler(w http.ResponseWriter, req *http.Request) {
	userID, err := getUserIDFromContext(req.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	messageIDStr := req.PathValue("messageID")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	var editReq EditMessageRequest
	if err := json.NewDecoder(req.Body).Decode(&editReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get the message to verify ownership
	message, err := r.db.GetMessageByID(req.Context(), messageID)
	if err != nil || message.UserID != userID {
		http.Error(w, "Message not found or unauthorized to edit", http.StatusForbidden)
		return
	}

	// Edit message in DB
	if err := r.db.EditMessage(req.Context(), messageID, userID, editReq.Content); err != nil {
		http.Error(w, "Failed to edit message", http.StatusInternalServerError)
		return
	}

	// Fetch the updated message to send in response and broadcast
	updatedMessage, err := r.db.GetMessageByID(req.Context(), messageID)
	if err != nil {
		// Log error, but proceed with a generic success if DB update was fine
		json.NewEncoder(w).Encode(map[string]string{"message": "Message edited successfully, but failed to fetch updated message."})
		return
	}

	// Publish message update event to other nodes (via syncEngine)
	// This will then trigger broadcasting to clients in the room
	r.syncEngine.PublishMessage(req.Context(), updatedMessage)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedMessage)
}

// SoftDeleteMessageHandler handles message soft deletion
func (r *Router) SoftDeleteMessageHandler(w http.ResponseWriter, req *http.Request) {
	userID, err := getUserIDFromContext(req.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	messageIDStr := req.PathValue("messageID")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	// Get the message to verify ownership
	message, err := r.db.GetMessageByID(req.Context(), messageID)
	if err != nil || message.UserID != userID {
		http.Error(w, "Message not found or unauthorized to delete", http.StatusForbidden)
		return
	}

	// Soft delete message in DB
	if err := r.db.SoftDeleteMessage(req.Context(), messageID, userID); err != nil {
		http.Error(w, "Failed to delete message", http.StatusInternalServerError)
		return
	}

	// Publish message update event (soft delete) to other nodes (via syncEngine)
	// This will then trigger broadcasting to clients in the room
	// For soft delete, we'll send a simplified message indicating deletion
	r.syncEngine.PublishMessage(req.Context(), &models.Message{ID: messageID, RoomID: message.RoomID, DeletedAt: &message.CreatedAt})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Message deleted successfully"})
}

// AddReactionRequest represents an add reaction request
type AddReactionRequest struct {
	Emoji string `json:"emoji"`
}

// AddReactionHandler handles adding a reaction to a message
func (r *Router) AddReactionHandler(w http.ResponseWriter, req *http.Request) {
	userID, err := getUserIDFromContext(req.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	messageIDStr := req.PathValue("messageID")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	var addReq AddReactionRequest
	if err := json.NewDecoder(req.Body).Decode(&addReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Check room membership
	isMember, err := r.db.IsRoomMember(req.Context(), roomID, userID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	// Add reaction to DB
	if err := r.db.AddMessageReaction(req.Context(), messageID, userID, addReq.Emoji); err != nil {
		http.Error(w, "Failed to add reaction", http.StatusInternalServerError)
		return
	}

	// Publish reaction update event
	r.syncEngine.PublishRoomEvent(req.Context(), roomID, "reaction_added", map[string]interface{}{
		"message_id": messageID,
		"user_id":    userID,
		"emoji":      addReq.Emoji,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reaction added successfully"})
}

// RemoveReactionHandler handles removing a reaction from a message
func (r *Router) RemoveReactionHandler(w http.ResponseWriter, req *http.Request) {
	userID, err := getUserIDFromContext(req.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	messageIDStr := req.PathValue("messageID")
	messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	emoji := req.PathValue("emoji")
	if emoji == "" {
		http.Error(w, "Missing emoji", http.StatusBadRequest)
		return
	}

	// Check room membership
	isMember, err := r.db.IsRoomMember(req.Context(), roomID, userID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		return
	}

	// Remove reaction from DB
	if err := r.db.RemoveMessageReaction(req.Context(), messageID, userID, emoji); err != nil {
		http.Error(w, "Failed to remove reaction", http.StatusInternalServerError)
		return
	}

	// Publish reaction update event
	r.syncEngine.PublishRoomEvent(req.Context(), roomID, "reaction_removed", map[string]interface{}{
		"message_id": messageID,
		"user_id":    userID,
		"emoji":      emoji,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reaction removed successfully"})
}

// getUserIDFromContext is a helper to extract userID from context
func getUserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(contextkey.ContextKeyUserID).(uuid.UUID)
	if !ok {
		return uuid.Nil, fmt.Errorf("user ID not found in context")
	}
	return userID, nil
}
