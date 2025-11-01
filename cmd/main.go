package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/api"
	"github.com/dukepan/multi-rooms-chat-back/internal/cache"
	"github.com/dukepan/multi-rooms-chat-back/internal/config"
	"github.com/dukepan/multi-rooms-chat-back/internal/db"
	"github.com/dukepan/multi-rooms-chat-back/internal/filescan"
	"github.com/dukepan/multi-rooms-chat-back/internal/filestore"
	"github.com/dukepan/multi-rooms-chat-back/internal/observability"
	"github.com/dukepan/multi-rooms-chat-back/internal/persistence"
	"github.com/dukepan/multi-rooms-chat-back/internal/rooms"
	"github.com/dukepan/multi-rooms-chat-back/internal/utils"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize OpenTelemetry
	otelCleanup, err := observability.InitOpenTelemetry("gochat-backend", "1.0.0")
	if err != nil {
		log.Fatalf("Failed to initialize OpenTelemetry: %v", err)
	}
	defer func() {
		if err := otelCleanup(context.Background()); err != nil {
			log.Printf("Error shutting down OpenTelemetry: %v", err)
		}
	}()

	// Initialize structured logger
	logger := utils.NewLogger(cfg.LogLevel)

	// Initialize database
	database, err := db.New(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal(context.Background(), "Failed to initialize database: %v", err)
	}

	// Initialize cache (Redis)
	redisCache, err := cache.New(cfg.RedisURL)
	if err != nil {
		logger.Fatal(context.Background(), "Failed to initialize cache: %v", err)
	}

	// Initialize persistence engine
	messageWriter := persistence.NewMessageWriter(database, redisCache)
	go messageWriter.Start(context.Background())

	// Initialize sync engine
	// Temporarily pass nil for roomMgr, will set it after roomMgr init
	syncEngine := persistence.NewSyncEngine(database, redisCache, nil)
	go syncEngine.Start(context.Background())

	// Initialize room manager, passing syncEngine (as rooms.SyncEngineService)
	roomMgr := rooms.NewManager(database, redisCache, syncEngine)
	go roomMgr.Start(context.Background())

	// Now that roomMgr is initialized, set it in syncEngine
	// This is effectively breaking the explicit circular dependency while maintaining interaction
	syncEngine.SetRoomManager(roomMgr)

	// Start background jobs
	syncEngine.RunCleanupJob(context.Background(), 24*time.Hour)     // Run daily
	syncEngine.RunArchivingJob(context.Background(), 7*24*time.Hour) // Run weekly
	syncEngine.RunIndexingJob(context.Background(), 1*time.Hour)     // Run hourly

	// Initialize ClamAV client (if address is provided)
	var clamAVClient *filescan.ClamAVClient
	if cfg.ClamAVAddress != "" {
		clamAVTimeout, err := time.ParseDuration(cfg.ClamAVTimeout)
		if err != nil {
			logger.Fatal(context.Background(), "Invalid ClamAV timeout duration: %v", err)
		}
		clamAVClient, err = filescan.NewClamAVClient(cfg.ClamAVAddress, clamAVTimeout)
		if err != nil {
			logger.Fatal(context.Background(), "Failed to initialize ClamAV client: %v", err)
		}
		logger.Info(context.Background(), "ClamAV client initialized for address: %s", cfg.ClamAVAddress)
	}

	// Initialize Local File Store
	localFileStore, err := filestore.NewLocalFileStore(cfg.FileStoragePath, cfg.BaseFileURL)
	if err != nil {
		logger.Fatal(context.Background(), "Failed to initialize local file store: %v", err)
	}

	// Setup HTTP router
	router := api.NewRouter(database, redisCache, roomMgr, messageWriter, syncEngine, clamAVClient, localFileStore, cfg)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info(context.Background(), "Starting server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal(context.Background(), "Server error: %v", err)
		}
	}()

	// Graceful shutdown setup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received
	<-sigChan

	// Centralized graceful shutdown function
	gracefulShutdown(context.Background(), logger, server, database, redisCache, roomMgr, messageWriter, syncEngine, clamAVClient, otelCleanup)

	logger.Info(context.Background(), "Application stopped.")
}

// gracefulShutdown handles the graceful shutdown of all components
func gracefulShutdown(ctx context.Context, logger *utils.Logger, server *http.Server, db *db.Database, cache *cache.Cache, roomMgr *rooms.Manager, messageWriter rooms.MessageWriterService, syncEngine rooms.SyncEngineService, clamAVClient *filescan.ClamAVClient, otelCleanup func(context.Context) error) {
	logger.Info(ctx, "Shutting down server...")

	// Create a context with a timeout for shutdown operations
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 1. Shut down HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server shutdown error: %v", err)
	} else {
		logger.Info(ctx, "HTTP server stopped.")
	}

	// 2. Stop Room Manager (closes client connections)
	roomMgr.Stop()
	logger.Info(ctx, "Room Manager stopped.")

	// 3. Stop Message Writer (flushes remaining messages)
	messageWriter.Stop()
	logger.Info(ctx, "Message Writer stopped.")

	// 4. Stop Sync Engine
	syncEngine.Stop()
	logger.Info(ctx, "Sync Engine stopped.")

	// 5. Close Database connection
	if err := db.Close(); err != nil {
		logger.Error(ctx, "Database close error: %v", err)
	} else {
		logger.Info(ctx, "Database connection closed.")
	}

	// 6. Close Redis cache connection
	if err := cache.Close(); err != nil {
		logger.Error(ctx, "Redis cache close error: %v", err)
	} else {
		logger.Info(ctx, "Redis cache connection closed.")
	}

	// 7. Shutdown OpenTelemetry
	if otelCleanup != nil {
		if err := otelCleanup(shutdownCtx); err != nil {
			logger.Error(ctx, "OpenTelemetry shutdown error: %v", err)
		} else {
			logger.Info(ctx, "OpenTelemetry shut down.")
		}
	}

	logger.Info(ctx, "Graceful shutdown complete.")
}
