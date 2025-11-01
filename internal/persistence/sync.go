package persistence

import (
	"context"
	"encoding/json"
	"fmt" // Added import for fmt
	"log"
	"sync"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/db"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
	"github.com/dukepan/multi-rooms-chat-back/internal/rooms"
	"github.com/google/uuid"
)

// SyncEngine coordinates cross-node synchronization via Redis Pub/Sub
type SyncEngine struct {
	db      *db.Database
	cache   *cache.Cache
	roomMgr *rooms.Manager // Add RoomManager
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewSyncEngine creates a new sync engine
func NewSyncEngine(database *db.Database, redisCache *cache.Cache, roomMgr *rooms.Manager) *SyncEngine {
	return &SyncEngine{
		db:      database,
		cache:   redisCache,
		roomMgr: roomMgr, // Initialize roomMgr
		done:    make(chan struct{}),
	}
}

// SetRoomManager sets the room manager for the sync engine. This is used for circular dependencies.
func (se *SyncEngine) SetRoomManager(roomMgr *rooms.Manager) {
	se.roomMgr = roomMgr
}

// Start begins the sync engine
func (se *SyncEngine) Start(ctx context.Context) {
	se.wg.Add(1)
	go se.syncLoop(ctx)
}

// Stop gracefully shuts down the sync engine
func (se *SyncEngine) Stop() {
	close(se.done)
	se.wg.Wait()
}

// syncLoop subscribes to Redis Pub/Sub and handles sync events
func (se *SyncEngine) syncLoop(ctx context.Context) {
	defer se.wg.Done()

	pubsub := se.cache.Subscribe(ctx, "messages", "room_events", "user_events", "messages_delivered")
	defer pubsub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case <-se.done:
			return
		case msg := <-pubsub.Channel():
			if msg != nil {
				se.handleSyncEvent(ctx, msg.Channel, msg.Payload)
			}
		}
	}
}

// handleSyncEvent processes sync events from other nodes
func (se *SyncEngine) handleSyncEvent(ctx context.Context, channel, payload string) {
	switch channel {
	case "messages_delivered":
		se.handleMessageDelivered(ctx, payload)
	case "messages", "message_edited", "message_deleted": // Handle all message update types
		se.handleMessageSync(ctx, payload)
	case "room_events":
		se.handleRoomEvent(ctx, payload)
	case "user_events":
		se.handleUserEvent(ctx, payload)
	}
}

// handleMessageDelivered handles message delivered events from other nodes
func (se *SyncEngine) handleMessageDelivered(ctx context.Context, payload string) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		log.Printf("Error unmarshaling message delivered event: %v", err)
		return
	}

	roomIDStr, ok := event["room_id"].(string)
	if !ok {
		log.Println("Missing room_id in message delivered event")
		return
	}
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		log.Printf("Invalid room_id in message delivered event: %v", err)
		return
	}

	// Use the BroadcastMessage method to send the event to the room
	se.roomMgr.BroadcastMessage(roomID, event)
}

// handleMessageSync handles message sync from other nodes
func (se *SyncEngine) handleMessageSync(ctx context.Context, payload string) {
	var msg models.Message
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return
	}

	// Broadcast the updated/deleted message to clients in the room.
	// The client-side will interpret the 'edited_at' or 'deleted_at' fields
	// or use the message type to update the UI accordingly.
	if msg.RoomID != uuid.Nil {
		se.roomMgr.BroadcastMessage(msg.RoomID, msg)
	}
}

// RunCleanupJob performs periodic database cleanup (e.g., old soft-deleted messages)
func (se *SyncEngine) RunCleanupJob(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Running cleanup job...")
				// TODO: Implement actual cleanup logic, e.g., delete soft-deleted messages older than X days
			}
		}
	}()
}

// RunArchivingJob performs periodic archiving of old data
func (se *SyncEngine) RunArchivingJob(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Running archiving job...")
				// TODO: Implement actual archiving logic, e.g., move old messages to cold storage
			}
		}
	}()
}

// RunIndexingJob performs periodic indexing tasks (e.g., rebuilding search indexes)
func (se *SyncEngine) RunIndexingJob(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Running indexing job...")
				// TODO: Implement actual indexing logic, e.g., refresh full-text search index
			}
		}
	}()
}

// handleRoomEvent handles room events
func (se *SyncEngine) handleRoomEvent(ctx context.Context, payload string) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		log.Printf("Error unmarshaling room event: %v", err)
		return
	}

	eventType, ok := event["type"].(string)
	if !ok {
		log.Printf("Missing event type in room event")
		return
	}

	roomIDStr, ok := event["room_id"].(string)
	if !ok {
		log.Printf("Missing room_id in room event")
		return
	}
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		log.Printf("Invalid room_id in room event: %v", err)
		return
	}

	switch eventType {
	case "reaction_added", "reaction_removed":
		// Broadcast reaction event to clients in the room
		se.roomMgr.BroadcastMessage(roomID, event)
	default:
		log.Printf("Unknown room event type: %s", eventType)
	}
}

// handleUserEvent handles user events
func (se *SyncEngine) handleUserEvent(ctx context.Context, payload string) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		log.Printf("Error unmarshaling user event: %v", err)
		return
	}

	eventType, ok := event["type"].(string)
	if !ok {
		log.Println("Missing event type in user event")
		return
	}

	if eventType == "status_change" {
		userIDStr, ok := event["user_id"].(string)
		if !ok {
			log.Println("Missing user_id in user status change event")
			return
		}
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			log.Printf("Invalid user_id in user status change event: %v", err)
			return
		}

		status, ok := event["status"].(string)
		if !ok {
			log.Println("Missing status in user status change event")
			return
		}
		_ = status // Mark as used to satisfy linter

		// Update user status in DB (if not already done by originating node)
		// This ensures eventual consistency in the DB even if Redis is primary for real-time
		// se.db.UpdateUserStatus(ctx, userID, status) // This is handled by Client.Stop() already

		// Update presence in local RoomManager (if user is in an active room on this node)
		// The RoomManager will then broadcast to relevant clients.
		if roomIDStr, ok := event["room_id"].(string); ok && roomIDStr != "" {
			roomID, err := uuid.Parse(roomIDStr)
			if err == nil {
				room := se.roomMgr.GetOrCreateRoom(roomID)
				if room != nil {
					// Broadcast user event to clients in that room
					se.roomMgr.BroadcastUserEvent(roomID, userID, eventType) // Use exported BroadcastUserEvent
				}
			}
		}
	}
}

// PublishUserStatus publishes user status changes
func (se *SyncEngine) PublishUserStatus(ctx context.Context, userID uuid.UUID, status string) error {
	_ = status // Mark as used to satisfy linter
	event := map[string]interface{}{
		"type":      "status_change",
		"user_id":   userID.String(),
		"status":    status,
		"timestamp": time.Now(),
	}

	data, _ := json.Marshal(event)
	return se.cache.Publish(ctx, "user_events", string(data))
}

// PublishRoomEvent publishes room events
func (se *SyncEngine) PublishRoomEvent(ctx context.Context, roomID uuid.UUID, eventType string, data map[string]interface{}) error {
	event := map[string]interface{}{
		"type":      eventType,
		"room_id":   roomID.String(),
		"timestamp": time.Now(),
		"data":      data,
	}

	eventData, _ := json.Marshal(event)
	return se.cache.Publish(ctx, "room_events", string(eventData))
}

// PublishMessage publishes a new message to the sync channel
func (se *SyncEngine) PublishMessage(ctx context.Context, message *models.Message) error {
	messageData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message for sync: %w", err)
	}
	return se.cache.Publish(ctx, "messages", string(messageData))
}
