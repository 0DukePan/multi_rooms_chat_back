package rooms

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

// Client is a middleman between the websocket connection and the room.
type Client struct {
	room          *Room
	conn          *websocket.Conn
	send          chan interface{}
	userID        uuid.UUID
	messageWriter MessageWriterService
}

// NewClient creates a new client for a room
func NewClient(room *Room, conn *websocket.Conn, userID uuid.UUID, messageWriter MessageWriterService) *Client {
	return &Client{
		room:          room,
		conn:          conn,
		send:          make(chan interface{}, 256),
		userID:        userID,
		messageWriter: messageWriter,
	}
}

// readPump pumps messages from the websocket connection to the room.
// A goroutine is started for each connection. The application ensures that there is at most one reader per connection by invoking this as a goroutine.
func (c *Client) readPump() {
	defer func() {
		c.room.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("error unmarshaling message: %v", err)
			continue
		}

		messageType, ok := msg["type"].(string)
		if !ok {
			log.Printf("message type not found or invalid")
			continue
		}

		switch messageType {
		case "message":
			content, ok := msg["content"].(string)
			if !ok {
				log.Printf("message content not found or invalid")
				continue
			}
			fileURL, _ := msg["file_url"].(string)             // Optional
			chatMessageType, _ := msg["message_type"].(string) // text, image, file
			c.handleChatMessage(context.Background(), content, chatMessageType, fileURL)
		case "typing_start":
			c.room.HandleTypingEvent(c.userID, true)
		case "typing_stop":
			c.room.HandleTypingEvent(c.userID, false)
		case "read":
			messageID, ok := msg["message_id"].(float64)
			if !ok {
				log.Printf("message_id for read receipt not found or invalid")
				continue
			}
			c.handleRead(context.Background(), int64(messageID))
		case "message_edited", "message_deleted":
			// For edited/deleted messages, simply re-broadcast the raw message to the room
			// The client-side will interpret the 'edited_at' or 'deleted_at' fields
			c.room.broadcast <- msg
		case "reaction_added", "reaction_removed":
			// For reaction updates, simply re-broadcast the raw event to the room
			// The client-side will update the UI accordingly
			c.room.broadcast <- msg
		default:
			log.Printf("unknown message type: %s", messageType)
		}
	}
}

// writePump pumps messages from the room to the websocket connection.
// A goroutine is started for each connection. The application ensures that there is at most one writer per connection by invoking this as a goroutine.
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The room closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			err := c.conn.WriteJSON(message)
			if err != nil {
				log.Printf("error writing message: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleChatMessage processes incoming chat messages from a client
func (c *Client) handleChatMessage(ctx context.Context, content string, messageType string, fileURL string) {
	msg := &models.Message{
		RoomID:      c.room.ID,
		UserID:      c.userID,
		Content:     content,
		MessageType: messageType,
		FileURL:     fileURL,
		CreatedAt:   time.Now(),
	}

	// Queue message for persistence
	c.messageWriter.QueueMessage(msg)
}

// handleRead processes read receipts from a client
func (c *Client) handleRead(ctx context.Context, messageID int64) {
	// Persist read receipt to database
	// Assuming MarkMessageRead is a method on db.Database
	_ = c.room.manager.db.MarkMessageRead(ctx, messageID, c.userID)
}

// Start begins the client's read and write pumps
func (c *Client) Start() {
	// Update user presence to online
	c.room.manager.cache.SetUserPresence(context.Background(), c.userID, cache.PresenceState{
		Status:   "online",
		LastSeen: time.Now(),
	})

	// Publish user status change
	c.room.manager.syncEngine.PublishUserStatus(context.Background(), c.userID, "online")

	go c.writePump()
	go c.readPump()
	// Register the client with the room after starting pumps
	c.room.register <- c
}

// Stop gracefully shuts down the client
func (c *Client) Stop() {
	// Update user presence to offline and last_seen
	c.room.manager.cache.SetUserPresence(context.Background(), c.userID, cache.PresenceState{
		Status:   "offline",
		LastSeen: time.Now(),
	})

	// Publish user status change
	c.room.manager.syncEngine.PublishUserStatus(context.Background(), c.userID, "offline")

	// Close the connection
	c.conn.Close()
}
