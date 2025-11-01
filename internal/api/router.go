package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dukepan/multi-rooms-chat-back/internal/auth"
	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/config"
	"github.com/dukepan/multi-rooms-chat-back/internal/db"
	"github.com/dukepan/multi-rooms-chat-back/internal/filescan"
	"github.com/dukepan/multi-rooms-chat-back/internal/filestore"
	"github.com/dukepan/multi-rooms-chat-back/internal/middleware"
	"github.com/dukepan/multi-rooms-chat-back/internal/rooms"
)

type Router struct {
	mux           *http.ServeMux
	db            *db.Database
	cache         *cache.Cache
	roomMgr       *rooms.Manager
	jwtMgr        *auth.JWTManager
	cfg           *config.Config
	messageWriter rooms.MessageWriterService // Use interface
	syncEngine    rooms.SyncEngineService    // Add SyncEngineService
	fileStore     *filestore.LocalFileStore  // Use local file store
	clamAVClient  *filescan.ClamAVClient
}

// NewRouter creates a new HTTP router with configured handlers and middleware
func NewRouter(database *db.Database, redisCache *cache.Cache, roomMgr *rooms.Manager, messageWriter rooms.MessageWriterService, syncEngine rooms.SyncEngineService, clamAVClient *filescan.ClamAVClient, localFileStore *filestore.LocalFileStore, cfg *config.Config) http.Handler {
	jwtMgr, err := auth.NewJWTManager(cfg.JWTPrivateKey, cfg.JWTPublicKey)
	if err != nil {
		log.Fatalf("Failed to initialize JWT manager: %v", err)
	}

	// Initialize Rate Limiter
	rateLimiter := middleware.NewRateLimiter(redisCache.GetClient())

	// The fileStore is initialized in main.go and passed here. No need to re-initialize.

	r := &Router{
		mux:           http.NewServeMux(),
		db:            database,
		cache:         redisCache,
		roomMgr:       roomMgr,
		jwtMgr:        jwtMgr,
		cfg:           cfg,
		messageWriter: messageWriter,
		syncEngine:    syncEngine,     // Initialize syncEngine
		fileStore:     localFileStore, // Use local file store
		clamAVClient:  clamAVClient,
	}

	// Apply Request ID middleware to all requests
	routerWithMiddleware := middleware.RequestIDMiddleware(r.mux)

	// Apply Tracing middleware to all requests after Request ID
	routerWithMiddleware = middleware.TracingMiddleware(routerWithMiddleware)

	// Public endpoints
	r.mux.HandleFunc("/auth/signup", r.SignupHandler)
	r.mux.HandleFunc("/auth/login", r.LoginHandler)
	r.mux.HandleFunc("/healthz", r.HealthzHandler)
	r.mux.Handle("/metrics", promhttp.Handler()) // Prometheus metrics endpoint
	// Serve static files from local storage
	r.mux.Handle(fmt.Sprintf("%s/", cfg.BaseFileURL), http.StripPrefix(cfg.BaseFileURL, http.FileServer(http.Dir(cfg.FileStoragePath))))

	// Protected endpoints with AuthMiddleware and RateLimiter
	r.mux.Handle("/rooms", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.GetRoomsHandler))))
	r.mux.Handle("/rooms", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.CreateRoomHandler))))
	r.mux.Handle("/rooms/{id}", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.GetRoomHandler))))
	r.mux.Handle("/rooms/{id}/messages", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.GetRoomMessagesHandler))))
	r.mux.Handle("/rooms/{id}/search", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.SearchMessagesHandler))))
	r.mux.Handle("/rooms/{id}/messages/{messageID}", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.EditMessageHandler))))
	r.mux.Handle("/rooms/{id}/messages/{messageID}", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.SoftDeleteMessageHandler))))
	r.mux.Handle("/rooms/{id}/messages/{messageID}/reactions", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.AddReactionHandler))))            // Add reaction
	r.mux.Handle("/rooms/{id}/messages/{messageID}/reactions/{emoji}", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.RemoveReactionHandler)))) // Remove reaction
	r.mux.Handle("/files/upload", r.AuthMiddleware(rateLimiter.Middleware(http.HandlerFunc(r.UploadFileHandler))))                                          // New upload endpoint
	// WebSocket endpoint will handle rate limiting internally or at a different layer if needed
	r.mux.Handle("/ws", http.HandlerFunc(r.WebSocketHandler))

	return routerWithMiddleware
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
