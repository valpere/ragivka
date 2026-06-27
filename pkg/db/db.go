package db

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the database connection settings.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string // NFR-9: allow production to enforce SSL (e.g. "require" or "verify-full")
	MaxConns int32
	MinConns int32
}

// NewPool initializes a new PostgreSQL connection pool via pgxpool.
// NFR-3: Connection Pooling to prevent exhaustion during worker spikes.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	// Start with an empty config and set fields directly to prevent DSN injection 
	// from special characters in credentials (e.g., passwords with '@' or ':').
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable" // Default to disable for local dev if not specified
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Path:   cfg.Database,
	}

	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()

	poolConfig, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Configure pool sizing
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	
	// Max connection lifetime to gracefully handle network issues
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	return pool, nil
}
