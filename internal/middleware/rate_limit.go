package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"math"
)

// RateLimiter implements a token bucket rate limiting mechanism using Redis.
type RateLimiter struct {
	redisClient *redis.Client
	// Token bucket parameters
	capacity  int64         // Maximum number of tokens the bucket can hold
	rate      float64       // Tokens added per second
	tokenLock sync.Mutex    // Protects lastRefillTime and currentTokens
}

// NewRateLimiter creates a new RateLimiter instance.
func NewRateLimiter(redisClient *redis.Client) *RateLimiter {
	return &RateLimiter{
		redisClient: redisClient,
		capacity:    5,
		rate:        1.0, // 1 token per second
	}
}

// Middleware applies rate limiting to HTTP requests.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Extract user ID from context
		userID, ok := req.Context().Value(contextkey.ContextKeyUserID).(uuid.UUID)
		if !ok || userID == uuid.Nil {
			http.Error(w, "Unauthorized: User ID not found in context", http.StatusUnauthorized)
			return
		}

		if !rl.Allow(req.Context(), userID.String()) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, req)
	})
}

// Allow checks if a request is allowed for a given user ID.
func (rl *RateLimiter) Allow(ctx context.Context, userID string) bool {
	key := fmt.Sprintf("rate_limit:%s", userID)

	// Get current tokens and last refill time from Redis
	val, err := rl.redisClient.HMGet(ctx, key, "tokens", "last_refill").Result()
	if err != nil {
		// Log error but allow request to proceed to avoid blocking in case of Redis issues
		fmt.Printf("Error getting rate limit info from Redis: %v\n", err)
		return true
	}

	currentTokens := rl.capacity
	lastRefillTime := time.Now()

	if val[0] != nil && val[1] != nil {
		if t, err := strconv.ParseFloat(val[0].(string), 64); err == nil {
			currentTokens = int64(t)
		}
		if t, err := time.Parse(time.RFC3339Nano, val[1].(string)); err == nil {
			lastRefillTime = t
		}
	}

	// Refill tokens
	now := time.Now()
	diff := now.Sub(lastRefillTime).Seconds()
	tokensToAdd := int64(diff * rl.rate)
	currentTokens = int64(math.Min(float64(rl.capacity), float64(currentTokens+tokensToAdd)))
	lastRefillTime = now

	// Consume token
	if currentTokens >= 1 {
		currentTokens--
		// Update Redis with new token count and last refill time
		_, err = rl.redisClient.HMSet(ctx, key, "tokens", currentTokens, "last_refill", lastRefillTime.Format(time.RFC3339Nano)).Result()
		if err != nil {
			fmt.Printf("Error setting rate limit info to Redis: %v\n", err)
			return true // Allow request even if Redis update fails
		}
		return true
	}

	return false
}
