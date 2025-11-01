package utils

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"github.com/google/uuid"
)

// Logger provides structured logging
type Logger struct {
	slog *slog.Logger
}

// NewLogger creates a new structured logger.
// It can be enriched with context-specific attributes like request ID and user ID.
func NewLogger(logLevel string) *Logger {
	level := new(slog.Level)
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		*level = slog.LevelInfo // Default to info if parsing fails
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     level,
	})

	return &Logger{
		slog: slog.New(handler),
	}
}

// WithContext creates a child logger with request and user IDs from the context.
func (l *Logger) WithContext(ctx context.Context) *slog.Logger {
	handler := l.slog.Handler()

	// Extract request ID from context
	if reqID, ok := ctx.Value(contextkey.ContextKeyRequestID).(uuid.UUID); ok {
		handler = handler.WithGroup("request").WithAttrs([]slog.Attr{
			slog.String("id", reqID.String()),
		})
	}

	// Extract user ID from context
	if userID, ok := ctx.Value(contextkey.ContextKeyUserID).(uuid.UUID); ok {
		handler = handler.WithGroup("auth").WithAttrs([]slog.Attr{
			slog.String("user_id", userID.String()),
		})
	}

	return slog.New(handler)
}

// Info logs an info message.
func (l *Logger) Info(ctx context.Context, msg string, args ...interface{}) {
	l.WithContext(ctx).Info(fmt.Sprintf(msg, args...))
}

// Error logs an error message.
func (l *Logger) Error(ctx context.Context, msg string, args ...interface{}) {
	l.WithContext(ctx).Error(fmt.Sprintf(msg, args...))
}

// Debug logs a debug message.
func (l *Logger) Debug(ctx context.Context, msg string, args ...interface{}) {
	l.WithContext(ctx).Debug(fmt.Sprintf(msg, args...))
}

// Fatal logs a fatal message and exits. This should be used sparingly for unrecoverable errors.
func (l *Logger) Fatal(ctx context.Context, msg string, args ...interface{}) {
	l.WithContext(ctx).Error(fmt.Sprintf(msg, args...))
	os.Exit(1)
}
