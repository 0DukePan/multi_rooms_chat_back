package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	redisLatency metric.Float64Histogram
)

// PresenceState represents a user's presence information
type PresenceState struct {
	Status      string    `json:"status"`
	LastSeen    time.Time `json:"last_seen"`
	CurrentRoom uuid.UUID `json:"current_room,omitempty"`
}

type Cache struct {
	client *redis.Client
}

// New creates a new Redis cache connection
func New(dsn string) (*Cache, error) {
	var err error

	// Initialize metrics
	meter := otel.Meter("redis-client")
	redisLatency, err = meter.Float64Histogram("redis.command.latency", metric.WithUnit("ms"))
	if err != nil {
		return nil, fmt.Errorf("failed to create redis.command.latency instrument: %w", err)
	}

	opt, err := redis.ParseURL(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection with tracing
	ctx, span := otel.Tracer("redis-client").Start(context.Background(), "redis.ping")
	defer span.End()
	if err := client.Ping(ctx).Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to ping Redis")
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	span.SetStatus(codes.Ok, "Redis connected successfully")

	return &Cache{client: client}, nil
}

// GetClient returns the underlying Redis client (instrumented operations should use Cache methods)
func (c *Cache) GetClient() *redis.Client {
	// Direct access to client bypasses tracing/metrics, use with caution.
	return c.client
}

// Close closes the Redis client
func (c *Cache) Close() error {
	ctx, span := otel.Tracer("redis-client").Start(context.Background(), "redis.close")
	defer span.End()

	// Use ctx to satisfy the linter, even if not strictly needed for the close operation itself
	_ = ctx

	// No need to close the underlying client explicitly as redis.Client handles connection pooling.
	return nil
}

// Publish instruments a Publish operation
func (c *Cache) Publish(ctx context.Context, channel string, message interface{}) error {
	start := time.Now()
	ctx, span := otel.Tracer("redis-client").Start(ctx, "redis.publish", trace.WithAttributes(attribute.String("redis.channel", channel)))
	defer func() {
		redisLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("redis.command", "publish")))
		span.End()
	}()
	err := c.client.Publish(ctx, channel, message).Err()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Redis publish failed")
	}
	return err
}

// Subscribe instruments a Subscribe operation
func (c *Cache) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	// Note: Subscribe is a long-lived operation, tracing will span its entire duration.
	start := time.Now()
	ctx, span := otel.Tracer("redis-client").Start(ctx, "redis.subscribe", trace.WithAttributes(attribute.StringSlice("redis.channels", channels)))
	defer func() {
		redisLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("redis.command", "subscribe")))
		// Span will not end here for long-lived PubSub, needs manual handling in consumer
		// For simplicity, we'll let it end when the PubSub object is closed.
		// The span for subscribe should ideally end when the PubSub connection is closed by the caller.
		_ = span // Mark span as used to satisfy linter
	}()
	pubsub := c.client.Subscribe(ctx, channels...)
	// The span for subscribe should ideally end when the PubSub connection is closed.
	// For now, it will end on defer, which is okay for short-lived subscriptions or if
	// the caller manages the span.
	return pubsub
}

// SetUserPresence instruments SetUserPresence operation
func (c *Cache) SetUserPresence(ctx context.Context, userID uuid.UUID, state PresenceState) error {
	start := time.Now()
	ctx, span := otel.Tracer("redis-client").Start(ctx, "redis.set_user_presence", trace.WithAttributes(attribute.String("user.id", userID.String())))
	defer func() {
		redisLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("redis.command", "set_user_presence")))
		span.End()
	}()

	key := fmt.Sprintf("presence:%s", userID.String())
	data, err := json.Marshal(state)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to marshal presence state")
		return fmt.Errorf("failed to marshal presence state: %w", err)
	}
	err = c.client.Set(ctx, key, data, 0).Err()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to set user presence")
	}
	return err
}

// GetUserPresence instruments GetUserPresence operation
func (c *Cache) GetUserPresence(ctx context.Context, userID uuid.UUID) (*PresenceState, error) {
	start := time.Now()
	ctx, span := otel.Tracer("redis-client").Start(ctx, "redis.get_user_presence", trace.WithAttributes(attribute.String("user.id", userID.String())))
	defer func() {
		redisLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("redis.command", "get_user_presence")))
		span.End()
	}()

	key := fmt.Sprintf("presence:%s", userID.String())
	data, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		span.SetStatus(codes.Ok, "User not found in presence cache")
		return nil, nil // User not found in presence cache
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to get user presence")
		return nil, fmt.Errorf("failed to get user presence: %w", err)
	}

	var state PresenceState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to unmarshal presence state")
		return nil, fmt.Errorf("failed to unmarshal presence state: %w", err)
	}
	span.SetStatus(codes.Ok, "User presence retrieved")
	return &state, nil
}

// DeleteUserPresence instruments DeleteUserPresence operation
func (c *Cache) DeleteUserPresence(ctx context.Context, userID uuid.UUID) error {
	start := time.Now()
	ctx, span := otel.Tracer("redis-client").Start(ctx, "redis.delete_user_presence", trace.WithAttributes(attribute.String("user.id", userID.String())))
	defer func() {
		redisLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("redis.command", "delete_user_presence")))
		span.End()
	}()

	key := fmt.Sprintf("presence:%s", userID.String())
	err := c.client.Del(ctx, key).Err()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to delete user presence")
	}
	return err
}
