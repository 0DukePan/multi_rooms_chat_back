package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/auth"
	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"github.com/dukepan/multi-rooms-chat-back/internal/models"
)

// SignupRequest represents signup request payload
type SignupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest represents login request payload
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse represents auth response
type AuthResponse struct {
	Token     string       `json:"token"`
	User      *models.User `json:"user"`
	ExpiresIn int          `json:"expires_in"`
}

// UploadFileResponse represents the response for a file upload
type UploadFileResponse struct {
	FileKey string `json:"file_key"`
	FileURL string `json:"file_url"`
	Message string `json:"message"`
}

// SignupHandler handles user registration
func (r *Router) SignupHandler(w http.ResponseWriter, req *http.Request) {
	var signupReq SignupRequest
	if err := json.NewDecoder(req.Body).Decode(&signupReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate input
	if len(signupReq.Username) < 3 || len(signupReq.Password) < 8 {
		http.Error(w, "Invalid username or password", http.StatusBadRequest)
		return
	}

	// Hash password
	hashedPassword, err := auth.HashPassword(signupReq.Password)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Create user
	user, err := r.db.CreateUser(req.Context(), signupReq.Username, signupReq.Email, hashedPassword)
	if err != nil {
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	// Generate token
	token, err := r.jwtMgr.GenerateToken(user.ID, user.Username, user.Email, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Clear password hash before returning
	user.PasswordHash = ""

	response := AuthResponse{
		Token:     token,
		User:      user,
		ExpiresIn: 86400,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// LoginHandler handles user login
func (r *Router) LoginHandler(w http.ResponseWriter, req *http.Request) {
	var loginReq LoginRequest
	if err := json.NewDecoder(req.Body).Decode(&loginReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get user by username
	user, err := r.db.GetUserByUsername(req.Context(), loginReq.Username)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Verify password
	if !auth.VerifyPassword(user.PasswordHash, loginReq.Password) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate token
	token, err := r.jwtMgr.GenerateToken(user.ID, user.Username, user.Email, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Update user status to online
	r.db.UpdateUserStatus(req.Context(), user.ID, "online")

	// Clear password hash before returning
	user.PasswordHash = ""

	response := AuthResponse{
		Token:     token,
		User:      user,
		ExpiresIn: 86400,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HealthzHandler returns API health status
func (r *Router) HealthzHandler(w http.ResponseWriter, req *http.Request) {
	// Check database connectivity
	if err := r.db.Health(req.Context()); err != nil {
		http.Error(w, "Database unhealthy", http.StatusServiceUnavailable)
		return
	}

	// Check Redis connectivity
	if err := r.cache.GetClient().Ping(req.Context()).Err(); err != nil {
		http.Error(w, "Redis unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UploadFileHandler handles file uploads to local storage
func (r *Router) UploadFileHandler(w http.ResponseWriter, req *http.Request) {
	// 10MB limit for multipart form
	err := req.ParseMultipartForm(10 << 20) // 10 MB
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get file from form: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// --- Conceptual ClamAV Scan Integration (Pre-upload to final storage) ---
	/*
		if r.clamAVClient != nil {
			// Rewind the file reader if needed for multiple reads
			// file.Seek(0, io.SeekStart)
			isClean, scanErr := r.clamAVClient.ScanStream(req.Context(), file)
			if scanErr != nil {
				http.Error(w, fmt.Sprintf("File scan failed: %v", scanErr), http.StatusInternalServerError)
				return
			}
			if !isClean {
				http.Error(w, "File contains malware", http.StatusForbidden)
				return
			}
			// After scanning, rewind again if the file needs to be read by the next step (e.g., SaveFile)
			// file.Seek(0, io.SeekStart)
		}
	*/
	// --------------------------------------------------------------------------

	fileKey, fileURL, err := r.fileStore.SaveFile(file, header.Filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	response := UploadFileResponse{
		FileKey: fileKey,
		FileURL: fileURL,
		Message: "File uploaded successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// AuthMiddleware validates JWT and extracts user from context
func (r *Router) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		tokenString := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		if tokenString == "" {
			http.Error(w, "Authorization token required", http.StatusUnauthorized)
			return
		}

		claims, err := r.jwtMgr.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		// Store user ID in context
		ctx := context.WithValue(req.Context(), contextkey.ContextKeyUserID, claims.UserID)
		req = req.WithContext(ctx)
		next.ServeHTTP(w, req)
	})
}
