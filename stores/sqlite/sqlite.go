// Package sqlite provides a persistent budget store backed by SQLite. Unlike
// InMemoryBudgetStore it survives process restarts, and unlike RedisBudgetStore
// it requires no external server — ideal for single-node production services.
//
// Usage:
//
//	store, err := sqlite.NewSQLiteBudgetStore("budget.db", 24*time.Hour)
//	if err != nil { log.Fatal(err) }
//	bc := &otellix.BudgetConfig{Store: store, PerUserDailyLimit: 1.0, FallbackAction: otellix.FallbackBlock}
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteBudgetStore implements otellix.BudgetStore using a SQLite database.
type SQLiteBudgetStore struct {
	db       *sql.DB
	interval time.Duration
}

// NewSQLiteBudgetStore opens (creating if needed) a SQLite database at path and
// prepares the budget_spend table. interval is the rolling budget window.
func NewSQLiteBudgetStore(path string, interval time.Duration) (*SQLiteBudgetStore, error) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite store: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite store: ping %s: %w", path, err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS budget_spend (
			key        TEXT    NOT NULL,
			amount     REAL    NOT NULL,
			created_at TEXT    NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_budget_key_time ON budget_spend(key, created_at);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite store: migrate: %w", err)
	}
	return &SQLiteBudgetStore{db: db, interval: interval}, nil
}

// Close closes the underlying database.
func (s *SQLiteBudgetStore) Close() error { return s.db.Close() }

// GetSpend returns the total spend for key within the rolling window.
func (s *SQLiteBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	cutoff := time.Now().Add(-s.interval).UTC().Format(time.RFC3339Nano)
	var total float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM budget_spend WHERE key = ? AND created_at > ?`,
		key, cutoff,
	).Scan(&total)
	return total
}

// AddSpend records an amount of spend for key and prunes rows outside the window.
func (s *SQLiteBudgetStore) AddSpend(ctx context.Context, key string, amount float64) {
	now := time.Now()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO budget_spend (key, amount, created_at) VALUES (?, ?, ?)`,
		key, amount, now.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return
	}
	// Prune rows outside the window to bound table growth.
	cutoff := now.Add(-s.interval).UTC().Format(time.RFC3339Nano)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM budget_spend WHERE created_at <= ?`, cutoff)
}

// GetResetTime returns when the current rolling window resets for key.
func (s *SQLiteBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	var earliest string
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM budget_spend WHERE key = ?`,
		key,
	).Scan(&earliest)
	if err != nil || earliest == "" {
		return time.Now().Add(s.interval)
	}
	t, perr := time.Parse(time.RFC3339Nano, earliest)
	if perr != nil {
		return time.Now().Add(s.interval)
	}
	return t.Add(s.interval)
}
