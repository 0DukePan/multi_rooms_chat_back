package rooms

import (
	"context"

	"github.com/google/uuid"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
)

// SyncEngineService defines the interface for synchronization operations.
type SyncEngineService interface {
	PublishMessage(ctx context.Context, message *models.Message) error
	PublishUserStatus(ctx context.Context, userID uuid.UUID, status string) error
	PublishRoomEvent(ctx context.Context, roomID uuid.UUID, eventType string, data map[string]interface{}) error // Added for room events
	Stop()
	// Add other sync-related methods as needed
}

// MessageWriterService defines the interface for message persistence.
type MessageWriterService interface {
	QueueMessage(message *models.Message)
	Stop()
	// Add other message writing methods as needed
}
