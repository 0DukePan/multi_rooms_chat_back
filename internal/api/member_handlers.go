package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// AddMemberRequest represents adding a member to a room
type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// AddMemberHandler adds a user to a room
func (r *Router) AddMemberHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	requesterID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	_ = requesterID // Temporarily mark as used

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	var addReq AddMemberRequest
	if err := json.NewDecoder(req.Body).Decode(&addReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	memberID, err := uuid.Parse(addReq.UserID)
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	// Authorization check: Verify if requesterID has admin status in roomID
	// For now, this is a placeholder. In a real implementation, you would query the database
	// to check the role of requesterID in roomID.
	// E.g., is_admin, err := r.db.IsRoomAdmin(req.Context(), roomID, requesterID)
	// if err != nil || !is_admin {
	// 	http.Error(w, "Forbidden: Not authorized to add members to this room", http.StatusForbidden)
	// 	return
	// }

	// Add member to room
	err = r.db.AddRoomMember(req.Context(), roomID, memberID, addReq.Role)
	if err != nil {
		http.Error(w, "Failed to add member", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// RemoveMemberHandler removes a user from a room
func (r *Router) RemoveMemberHandler(w http.ResponseWriter, req *http.Request) {
	userIDStr := req.Header.Get("X-User-ID")
	requesterID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	_ = requesterID // Temporarily mark as used

	roomIDStr := req.PathValue("id")
	roomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	memberIDStr := req.PathValue("user_id")
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	// Authorization check: Verify if requesterID has admin status in roomID
	// For now, this is a placeholder. In a real implementation, you would query the database
	// to check the role of requesterID in roomID.
	// E.g., is_admin, err := r.db.IsRoomAdmin(req.Context(), roomID, requesterID)
	// if err != nil || !is_admin {
	// 	http.Error(w, "Forbidden: Not authorized to remove members from this room", http.StatusForbidden)
	// 	return
	// }

	// Remove member from room
	err = r.db.RemoveRoomMember(req.Context(), roomID, memberID)
	if err != nil {
		http.Error(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
