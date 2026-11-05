package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(ctx context.Context) (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	database.SetMaxOpenConns(15)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := database.PingContext(pingCtx); err != nil {
		database.Close()
		return nil, fmt.Errorf("PostgreSQL connection failed: %w", err)
	}
	return database, nil
}
