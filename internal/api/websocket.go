package api

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/dukepan/multi-rooms-chat-back/internal/rooms"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, validate origin more strictly
		return true
	},
}

// WebSocketHandler handles WebSocket upgrade and connection
func (r *Router) WebSocketHandler(w http.ResponseWriter, req *http.Request) {
	ctx, span := otel.Tracer("websocket-server").Start(req.Context(), "WebSocketConnection")
	defer span.End()

	// Extract JWT from query parameter
	token := req.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		span.SetStatus(codes.Error, "Missing token")
		return
	}

	// Validate token
	claims, err := r.jwtMgr.ValidateToken(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		span.SetStatus(codes.Error, fmt.Sprintf("Invalid token: %v", err))
		return
	}

	span.SetAttributes(attribute.String("user.id", claims.UserID.String()))

	// Extract room ID from query parameter
	roomIDStr := req.URL.Query().Get("room_id")
	if roomIDStr == "" {
		http.Error(w, "Missing room_id", http.StatusBadRequest)
		span.SetStatus(codes.Error, "Missing room_id")
		return
	}

	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room_id", http.StatusBadRequest)
		span.SetStatus(codes.Error, fmt.Sprintf("Invalid room_id: %v", err))
		return
	}

	span.SetAttributes(attribute.String("room.id", roomID.String()))

	// Check room membership
	isMember, err := r.db.IsRoomMember(ctx, roomID, claims.UserID)
	if err != nil || !isMember {
		http.Error(w, "Not a member of this room", http.StatusForbidden)
		span.SetStatus(codes.Error, fmt.Sprintf("Not a member of room %s: %v", roomID, err))
		return
	}

	// Upgrade connection
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		span.SetStatus(codes.Error, fmt.Sprintf("Failed to upgrade WebSocket connection: %v", err))
		return
	}
	defer conn.Close()

	span.SetStatus(codes.Ok, "WebSocket connection established")

	// Create and start client
	room := r.roomMgr.GetOrCreateRoom(roomID)
	client := rooms.NewClient(room, conn, claims.UserID, r.messageWriter)
	client.Start()

	// Keep connection alive
	select {}
}
