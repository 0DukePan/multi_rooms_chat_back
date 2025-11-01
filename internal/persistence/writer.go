package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/db"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
	"github.com/redis/go-redis/v9"
)

const (
	maxRetries     = 5
	initialBackoff = 100 * time.Millisecond // 100ms
)

// MessageWriter batches and persists messages to database
type MessageWriter struct {
	db           *db.Database
	cache        *cache.Cache
	messageQueue chan *models.Message
	done         chan struct{}
	wg           sync.WaitGroup

	batchSize     int
	flushInterval time.Duration
}

// NewMessageWriter creates a new message writer
func NewMessageWriter(database *db.Database, redisCache *cache.Cache) *MessageWriter {
	return &MessageWriter{
		db:            database,
		cache:         redisCache,
		messageQueue:  make(chan *models.Message, 1000),
		done:          make(chan struct{}),
		batchSize:     50,
		flushInterval: 100 * time.Millisecond,
	}
}

// Start begins the writer's batch processing loop
func (mw *MessageWriter) Start(ctx context.Context) {
	mw.wg.Add(1)
	go mw.batchWriter(ctx)
}

// Stop gracefully shuts down the writer
func (mw *MessageWriter) Stop() {
	close(mw.done)
	mw.wg.Wait()
}

// QueueMessage adds a message to the write queue
func (mw *MessageWriter) QueueMessage(msg *models.Message) {
	select {
	case mw.messageQueue <- msg:
	case <-mw.done:
	}
}

// batchWriter processes messages in batches
func (mw *MessageWriter) batchWriter(ctx context.Context) {
	defer mw.wg.Done()

	batch := make([]*models.Message, 0, mw.batchSize)
	ticker := time.NewTicker(mw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining messages
			if len(batch) > 0 {
				mw.writeBatch(ctx, batch)
			}
			return

		case <-mw.done:
			// Flush remaining messages
			if len(batch) > 0 {
				mw.writeBatch(ctx, batch)
			}
			return

		case msg := <-mw.messageQueue:
			if msg != nil {
				batch = append(batch, msg)
				if len(batch) >= mw.batchSize {
					mw.writeBatch(ctx, batch)
					batch = batch[:0]
					ticker.Reset(mw.flushInterval)
				}
			}

		case <-ticker.C:
			if len(batch) > 0 {
				mw.writeBatch(ctx, batch)
				batch = batch[:0]
			}
		}
	}
}

// writeBatch persists a batch of messages to database
func (mw *MessageWriter) writeBatch(ctx context.Context, batch []*models.Message) {
	if len(batch) == 0 {
		return
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// Use a transaction for batch inserts
		x, err := mw.db.GetPool().Begin(ctx)
		if err != nil {
			lastErr = fmt.Errorf("error beginning transaction for message batch (attempt %d/%d): %w", i+1, maxRetries, err)
			time.Sleep(initialBackoff * time.Duration(math.Pow(2, float64(i))))
			continue
		}

		allMessagesPersisted := true
		for _, msg := range batch {
			// Create message within the transaction
			if err := mw.db.CreateMessage(ctx, msg); err != nil {
				log.Printf("Error persisting message in batch (attempt %d/%d): %v", i+1, maxRetries, err)
				x.Rollback(ctx) // Rollback the entire batch if any message fails
				lastErr = err
				allMessagesPersisted = false
				break // Exit inner loop and retry the whole batch
			}
		}

		if allMessagesPersisted {
			if err := x.Commit(ctx); err != nil {
				lastErr = fmt.Errorf("error committing transaction for message batch (attempt %d/%d): %w", i+1, maxRetries, err)
				time.Sleep(initialBackoff * time.Duration(math.Pow(2, float64(i))))
				continue // Retry commit
			}

			// If committed, process cache and Pub/Sub
			for _, msg := range batch {
				// Cache the message
				mw.cacheMessage(ctx, msg)

				// Publish to Redis Pub/Sub for cross-node sync
				event := map[string]interface{}{
					"type":       "message_delivered",
					"message_id": msg.ID,
					"room_id":    msg.RoomID,
					"user_id":    msg.UserID,
					"timestamp":  msg.CreatedAt,
					"content":    msg.Content,
				}
				eventJSON, _ := json.Marshal(event)
				mw.cache.Publish(ctx, "messages_delivered", string(eventJSON))
			}
			return // Successfully persisted and published
		}

		time.Sleep(initialBackoff * time.Duration(math.Pow(2, float64(i))))
	}

	if lastErr != nil {
		log.Printf("Failed to persist message batch after %d retries: %v", maxRetries, lastErr)
		// TODO: Consider a dead-letter queue or other failure handling for unrecoverable errors
	}
}

// cacheMessage caches a message in Redis
func (mw *MessageWriter) cacheMessage(ctx context.Context, msg *models.Message) {
	// Store message in Redis sorted set for quick retrieval
	client := mw.cache.GetClient()
	key := "room:" + msg.RoomID.String() + ":messages"

	// Add to sorted set with timestamp as score
	client.ZAdd(ctx, key, redis.Z{
		Score:  float64(msg.CreatedAt.UnixMilli()),
		Member: msg.ID,
	})

	// Set expiration on key (e.g., 24 hours)
	client.Expire(ctx, key, 24*time.Hour)
}

// GetCachedMessages retrieves cached messages from Redis
func (mw *MessageWriter) GetCachedMessages(ctx context.Context, roomID uuid.UUID, limit int) ([]int64, error) {
	client := mw.cache.GetClient()
	key := "room:" + roomID.String() + ":messages"

	// Get last N message IDs from sorted set
	vals, err := client.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	messageIDs := make([]int64, len(vals))
	for i, v := range vals {
		var id int64
		_, err := parseMessageID(v, &id)
		if err != nil {
			continue
		}
		messageIDs[i] = id
	}

	return messageIDs, nil
}

// parseMessageID parses a message ID from cache
func parseMessageID(s string, id *int64) (int64, error) {
	parsedID, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse message ID from cache: %w", err)
	}
	*id = parsedID
	return parsedID, nil
}
