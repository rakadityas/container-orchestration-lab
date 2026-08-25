// Package db wraps the Postgres connection used by the API.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type Item struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Connect opens a pooled connection to Postgres and verifies it with a ping.
func Connect(ctx context.Context, dsn string) (*sql.DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return conn, nil
}

// Migrate creates the schema the API needs. In a real project this would be
// a migration tool (goose, atlas, sqlc); a plain CREATE TABLE keeps the demo
// self-contained.
func Migrate(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS items (
			id   BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL
		)
	`)
	return err
}

func InsertItem(ctx context.Context, conn *sql.DB, name string) (Item, error) {
	var item Item
	item.Name = name
	err := conn.QueryRowContext(ctx,
		`INSERT INTO items (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&item.ID)
	return item, err
}

func ListItems(ctx context.Context, conn *sql.DB) ([]Item, error) {
	rows, err := conn.QueryContext(ctx, `SELECT id, name FROM items ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
