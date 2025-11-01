package middleware

import (
	"context"
	"net/http"

	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"github.com/google/uuid"
)

// RequestIDMiddleware generates a unique request ID and adds it to the context
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestID := uuid.New()
		ctx := context.WithValue(req.Context(), contextkey.ContextKeyRequestID, requestID)
		req = req.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID.String())
		next.ServeHTTP(w, req)
	})
}
