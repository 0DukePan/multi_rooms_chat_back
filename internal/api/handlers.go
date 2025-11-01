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
	"github.com/google/uuid"
)

// SignupRequest defines the request body for user signup
type SignupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest defines the request body for user login
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse defines the response body for user login
type LoginResponse struct {
	Token   string `json:"token"`
	Message string `json:"message"`
}

// ErrorResponse defines a generic error response structure
type ErrorResponse struct {
	Message string `json:"message"`
}

// HealthzHandler provides a simple health check endpoint
func (r *Router) HealthzHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// SignupHandler handles user registration
func (r *Router) SignupHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var sr SignupRequest
	err := json.NewDecoder(req.Body).Decode(&sr)
	if err != nil {
		r.logger.Error(ctx, "Failed to decode signup request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if sr.Username == "" || sr.Email == "" || sr.Password == "" {
		http.Error(w, "Username, email, and password are required", http.StatusBadRequest)
		return
	}

	hashedPassword, err := auth.HashPassword(sr.Password)
	if err != nil {
		r.logger.Error(ctx, "Failed to hash password: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Change HashedPassword to PasswordHash
	user := models.User{
		ID:           uuid.New(),
		Username:     sr.Username,
		Email:        sr.Email,
		PasswordHash: hashedPassword, // Corrected field name
	}

	// Adjust CreateUser call to match signature and capture returned user
	createdUser, err := r.db.CreateUser(ctx, user.Username, user.Email, user.PasswordHash)
	if err != nil {
		r.logger.Error(ctx, "Failed to create user: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Generate token
	token, err := r.jwtMgr.GenerateToken(createdUser.ID, createdUser.Username, createdUser.Email, 24*time.Hour)
	if err != nil {
		r.logger.Error(ctx, "Failed to generate token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(LoginResponse{Token: token, Message: "User created successfully"})
}

// LoginHandler handles user authentication
func (r *Router) LoginHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var lr LoginRequest
	err := json.NewDecoder(req.Body).Decode(&lr)
	if err != nil {
		r.logger.Error(ctx, "Failed to decode login request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := r.db.GetUserByUsername(ctx, lr.Username)
	if err != nil {
		r.logger.Error(ctx, "Failed to get user by username: %v", err)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Use VerifyPassword and user.PasswordHash
	if !auth.VerifyPassword(user.PasswordHash, lr.Password) {
		r.logger.Error(ctx, "Invalid password for user %s", lr.Username) // Changed from Warn to Error
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate token
	token, err := r.jwtMgr.GenerateToken(user.ID, user.Username, user.Email, 24*time.Hour)
	if err != nil {
		r.logger.Error(ctx, "Failed to generate token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(LoginResponse{Token: token, Message: "Logged in successfully"})
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

// UploadFileHandler handles file uploads to local storage
func (r *Router) UploadFileHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Max upload of 10MB
	err = req.ParseMultipartForm(10 << 20)
	if err != nil {
		r.logger.Error(ctx, "Failed to parse multipart form: %v", err)
		http.Error(w, "File too large or invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		r.logger.Error(ctx, "Failed to get file from form: %v", err)
		http.Error(w, "Failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%s-%s", userID.String(), header.Filename)

	// Placeholder for ClamAV scan. In a real application, you would integrate with ClamAV here.
	scanResult, err := r.clamAVClient.ScanStream(ctx, file) // Added ctx
	if err != nil {
		r.logger.Error(ctx, "ClamAV scan failed: %v", err)
		http.Error(w, "File scan failed", http.StatusInternalServerError)
		return
	}

	// Change logic to check if ScanStream returned false (virus detected in current stub) or error
	if !scanResult {
		r.logger.Error(ctx, "Virus detected in uploaded file: %s", filename) // Changed from Warn to Error
		http.Error(w, "Virus detected", http.StatusForbidden)
		return
	}

	// Reset the file reader to the beginning after scanning
	_, err = file.Seek(0, 0)
	if err != nil {
		r.logger.Error(ctx, "Failed to seek file: %v", err)
		http.Error(w, "Failed to process file", http.StatusInternalServerError)
		return
	}

	fileKey, fileURL, err := r.fileStore.SaveFile(file, filename)
	if err != nil {
		r.logger.Error(ctx, "Failed to save file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "File uploaded successfully",
		"fileKey": fileKey,
		"fileURL": fileURL,
	})
}
