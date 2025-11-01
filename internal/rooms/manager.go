package rooms

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/db"
	"github.com/google/uuid"
)

// Room represents an active chat room
type Room struct {
	ID             uuid.UUID
	clients        map[*Client]bool
	broadcast      chan interface{}
	register       chan *Client
	unregister     chan *Client
	typingTrackers map[uuid.UUID]time.Time
	mu             sync.RWMutex
	manager        *Manager // Add a reference to the Manager
}

// HandleTypingEvent updates the typing status for a user in the room.
func (r *Room) HandleTypingEvent(userID uuid.UUID, isTyping bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if isTyping {
		r.typingTrackers[userID] = time.Now()
	} else {
		delete(r.typingTrackers, userID)
	}

	// Broadcast the typing event to all clients in the room
	event := map[string]interface{}{
		"type":      "typing_update",
		"user_id":   userID.String(),
		"is_typing": isTyping,
	}
	r.broadcast <- event
}

// Manager manages all active rooms
type Manager struct {
	rooms          map[uuid.UUID]*Room
	db             *db.Database
	cache          *cache.Cache
	syncEngine     SyncEngineService // Use interface
	roomsMu        sync.RWMutex
	registerRoom   chan uuid.UUID
	unregisterRoom chan uuid.UUID
	pubsubCancel   context.CancelFunc

	// Add a map to track last activity time for LRU eviction
	lastActivity map[uuid.UUID]time.Time
	// A channel to signal eviction for cold rooms
	evictSignal chan struct{}
	evictDone   chan struct{}
}

// SetSyncEngine sets the sync engine for the manager. This is used for circular dependencies.
func (m *Manager) SetSyncEngine(syncEngine SyncEngineService) {
	m.syncEngine = syncEngine
}

// NewManager creates a new room manager
func NewManager(database *db.Database, redisCache *cache.Cache, syncEngine SyncEngineService) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx // Mark as used to satisfy linter
	m := &Manager{
		rooms:          make(map[uuid.UUID]*Room),
		db:             database,
		cache:          redisCache,
		syncEngine:     syncEngine,
		registerRoom:   make(chan uuid.UUID, 100),
		unregisterRoom: make(chan uuid.UUID, 100),
		pubsubCancel:   cancel,
		lastActivity:   make(map[uuid.UUID]time.Time),
		evictSignal:    make(chan struct{}),
		evictDone:      make(chan struct{}),
	}
	return m
}

// Start begins the manager's event loop
func (m *Manager) Start(ctx context.Context) {
	// Create a cancellable context for background jobs
	ctx, cancel := context.WithCancel(ctx)
	m.pubsubCancel = cancel // Use this to cancel pubsub as well

	// Subscribe to Redis Pub/Sub for cross-node sync
	go m.subscribeToPubSub(ctx)

	// Start room eviction job
	go m.evictColdRooms(ctx, 1*time.Minute, 10*time.Minute)

	for {
		select {
		case <-ctx.Done():
			m.Stop()
			return
		case roomID := <-m.registerRoom:
			m.createRoom(roomID)
		case roomID := <-m.unregisterRoom:
			m.removeRoom(roomID)
		}
	}
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() {
	m.roomsMu.Lock()
	defer m.roomsMu.Unlock()

	for _, room := range m.rooms {
		close(room.broadcast)
	}

	if m.pubsubCancel != nil {
		m.pubsubCancel() // Cancel context for pubsub and eviction
	}
}

// broadcastUserEvent broadcasts join/leave events
func (m *Manager) BroadcastUserEvent(roomID uuid.UUID, userID uuid.UUID, eventType string) {
	m.roomsMu.RLock()
	room, exists := m.rooms[roomID]
	m.roomsMu.RUnlock()

	if exists && room != nil {
		event := map[string]interface{}{
			"type":    eventType,
			"user_id": userID.String(),
			"room_id": roomID.String(),
		}
		room.broadcast <- event
	}
}

// BroadcastMessage broadcasts a message to all clients in a specific room.
func (m *Manager) BroadcastMessage(roomID uuid.UUID, message interface{}) {
	m.roomsMu.RLock()
	room, exists := m.rooms[roomID]
	m.roomsMu.RUnlock()

	if exists && room != nil {
		room.broadcast <- message
	}
}

// GetOrCreateRoom gets an existing room or creates a new one
func (m *Manager) GetOrCreateRoom(roomID uuid.UUID) *Room {
	m.roomsMu.Lock()
	defer m.roomsMu.Unlock()

	if room, exists := m.rooms[roomID]; exists {
		// Update activity on access
		m.lastActivity[roomID] = time.Now()
		return room
	}

	room := &Room{
		ID:             roomID,
		clients:        make(map[*Client]bool),
		broadcast:      make(chan interface{}, 256),
		register:       make(chan *Client, 16),
		unregister:     make(chan *Client, 16),
		typingTrackers: make(map[uuid.UUID]time.Time),
		manager:        m,
	}

	m.rooms[roomID] = room
	m.lastActivity[roomID] = time.Now() // Set initial activity
	go m.handleRoom(room)
	return room
}

// createRoom creates a new room and starts its event loop
func (m *Manager) createRoom(roomID uuid.UUID) {
	m.GetOrCreateRoom(roomID)
}

// removeRoom removes a room and closes all client connections
func (m *Manager) removeRoom(roomID uuid.UUID) {
	m.roomsMu.Lock()
	room, exists := m.rooms[roomID]
	if exists {
		delete(m.rooms, roomID)
	}
	m.roomsMu.Unlock()

	if exists && room != nil {
		close(room.broadcast)
	}
}

// handleRoom manages a single room's message broadcasting
func (m *Manager) handleRoom(room *Room) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-room.register:
			room.mu.Lock()
			room.clients[client] = true
			room.mu.Unlock()
			// Update room activity on client register
			m.roomsMu.Lock()
			m.lastActivity[room.ID] = time.Now()
			m.roomsMu.Unlock()
			// Notify others that user joined
			m.BroadcastUserEvent(room.ID, client.userID, "join")

		case client := <-room.unregister:
			room.mu.Lock()
			if _, exists := room.clients[client]; exists {
				delete(room.clients, client)
				close(client.send)
				m.BroadcastUserEvent(room.ID, client.userID, "leave")
			}
			room.mu.Unlock()

			// If room is empty, schedule for cleanup (now managed by LRU eviction)
			// No need for explicit 10-minute sleep here, LRU will handle it.
			// m.roomsMu.Lock()
			// delete(m.lastActivity, room.ID) // Remove from activity tracking if no clients
			// m.roomsMu.Unlock()

			room.mu.RLock()
			isEmpty := len(room.clients) == 0
			room.mu.RUnlock()
			if isEmpty {
				// Signal manager to check for eviction after a delay
				go func(roomID uuid.UUID) {
					time.Sleep(1 * time.Minute) // Give some buffer before potential eviction
					m.unregisterRoom <- roomID  // Trigger manager to consider for eviction
				}(room.ID)
			}

		case message := <-room.broadcast:
			// Update room activity on message broadcast
			m.roomsMu.Lock()
			m.lastActivity[room.ID] = time.Now()
			m.roomsMu.Unlock()
			room.mu.RLock()
			for client := range room.clients {
				select {
				case client.send <- message:
				default:
					// Client's send channel is full, skip
				}
			}
			room.mu.RUnlock()

		case <-ticker.C:
			// Cleanup stale typing indicators
			room.mu.Lock()
			now := time.Now()
			for userID, lastTyping := range room.typingTrackers {
				if now.Sub(lastTyping) > 3*time.Second {
					delete(room.typingTrackers, userID)
				}
			}
			room.mu.Unlock()
		}
	}
}

// subscribeToPubSub subscribes to Redis Pub/Sub for cross-node sync
func (m *Manager) subscribeToPubSub(ctx context.Context) {
	// Mark ctx as used to satisfy linter
	_ = ctx

	ctx, cancel := context.WithCancel(ctx)
	m.pubsubCancel = cancel

	pubsub := m.cache.Subscribe(ctx, "messages", "rooms", "users")
	defer pubsub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-pubsub.Channel():
			if msg == nil {
				return
			}
			// Handle sync messages from other nodes
			m.handleSyncMessage(msg.Channel, msg.Payload)
		}
	}
}

// handleSyncMessage handles sync messages from Redis
func (m *Manager) handleSyncMessage(channel, payload string) {
	// Implementation for cross-node sync
	// Parse channel and payload and broadcast to relevant rooms
}

// evictColdRooms periodically removes inactive rooms from memory
func (m *Manager) evictColdRooms(ctx context.Context, evictionInterval, inactivityThreshold time.Duration) {
	// Mark ctx as used to satisfy linter
	_ = ctx

	ticker := time.NewTicker(evictionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.roomsMu.Lock()
			now := time.Now()
			for roomID, lastActive := range m.lastActivity {
				if now.Sub(lastActive) > inactivityThreshold {
					// Check if room is actually empty before evicting
					if room, exists := m.rooms[roomID]; exists && len(room.clients) == 0 {
						log.Printf("Evicting cold room: %s", roomID)
						delete(m.rooms, roomID)
						delete(m.lastActivity, roomID)
					}
				}
			}
			m.roomsMu.Unlock()
		}
	}
}
