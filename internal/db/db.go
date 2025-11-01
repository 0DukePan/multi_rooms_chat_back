package db

import (
	"context"
	"fmt"
	"time"

	"github.com/dukepan/multi-rooms-chat-back/internal/contextkey"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxpgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var (
	dbLatency           metric.Float64Histogram
	dbActiveConnections metric.Int64UpDownCounter
)

type Database struct {
	pool *pgxpool.Pool
}

// New creates a new database connection
func New(dsn string) (*Database, error) {
	var err error

	// Initialize metrics
	meter := otel.Meter("db-client")
	dbLatency, err = meter.Float64Histogram("db.query.latency", metric.WithUnit("ms"))
	if err != nil {
		return nil, fmt.Errorf("failed to create db.query.latency instrument: %w", err)
	}
	dbActiveConnections, err = meter.Int64UpDownCounter("db.active.connections", metric.WithUnit("connections"))
	if err != nil {
		return nil, fmt.Errorf("failed to create db.active.connections instrument: %w", err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Configure BeforeAcquire to set app.user_id for RLS and trace connection acquisition
	config.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		_, span := otel.Tracer("db-client").Start(ctx, "db.connection.acquire")
		defer span.End()
		dbActiveConnections.Add(ctx, 1)

		userID, ok := ctx.Value(contextkey.ContextKeyUserID).(uuid.UUID)
		if ok {
			_, err := conn.Exec(ctx, "SELECT set_config('app.user_id', $1, false)", userID.String())
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "Failed to set RLS user ID")
				// Log error, but don't prevent connection from being acquired
				fmt.Printf("Error setting app.user_id for RLS: %v\n", err)
			}
		}
		return true
	}

	// Configure AfterRelease to trace connection release
	config.AfterRelease = func(conn *pgx.Conn) bool {
		dbActiveConnections.Add(context.Background(), -1)
		return true
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection with tracing
	ctx, span := otel.Tracer("db-client").Start(context.Background(), "db.ping")
	defer span.End()
	if err := pool.Ping(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to ping database")
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	span.SetStatus(codes.Ok, "Database connected successfully")
	return &Database{pool: pool}, nil
}

func (db *Database) GetPool() *pgxpool.Pool {
	return db.pool
}

func (db *Database) Close() error {
	db.pool.Close()
	return nil
}

func (db *Database) Health(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// QueryRow instruments a QueryRow operation
func (db *Database) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	start := time.Now()
	ctx, span := otel.Tracer("db-client").Start(ctx, "db.query.row")
	defer func() {
		dbLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("db.query", query)))
		span.End()
	}()
	return db.pool.QueryRow(ctx, query, args...)
}

// Query instruments a Query operation
func (db *Database) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	start := time.Now()
	ctx, span := otel.Tracer("db-client").Start(ctx, "db.query")
	defer func() {
		dbLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("db.query", query)))
		span.End()
	}()
	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Database query failed")
	}
	return rows, err
}

// Exec instruments an Exec operation
func (db *Database) Exec(ctx context.Context, query string, args ...interface{}) (pgxpgconn.CommandTag, error) {
	start := time.Now()
	ctx, span := otel.Tracer("db-client").Start(ctx, "db.exec")
	defer func() {
		dbLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("db.query", query)))
		span.End()
	}()
	cmdTag, err := db.pool.Exec(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Database exec failed")
	}
	return cmdTag, err
}

// Begin instruments a Begin operation
func (db *Database) Begin(ctx context.Context) (pgx.Tx, error) {
	start := time.Now()
	ctx, span := otel.Tracer("db-client").Start(ctx, "db.transaction.begin")
	defer func() {
		dbLatency.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attribute.String("db.operation", "begin")))
		span.End()
	}()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to begin transaction")
	}
	return tx, err
}
