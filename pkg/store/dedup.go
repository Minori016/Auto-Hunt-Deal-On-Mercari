// Package store provides a SQLite-based deduplication store.
//
// Uses pure-Go SQLite (modernc.org/sqlite) — no CGO required.
// This allows cross-compilation to ARM (Raspberry Pi) without
// needing a C compiler toolchain.
package store

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// DedupStore tracks which items have already been sent to Telegram.
type DedupStore struct {
	db *sql.DB
}

// NewDedupStore opens (or creates) the SQLite database.
func NewDedupStore(dbPath string) (*DedupStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	// Optimize for RPi: WAL mode = better concurrent reads, less disk I/O
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=1000",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			log.Printf("[STORE] Warning: %s failed: %v", p, err)
		}
	}

	// Create table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS seen_items (
			id       TEXT PRIMARY KEY,
			brand    TEXT NOT NULL,
			name     TEXT DEFAULT '',
			price    INTEGER DEFAULT 0,
			seen_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("creating table: %w", err)
	}

	store := &DedupStore{db: db}

	// Cleanup old entries on startup
	store.cleanup()

	return store, nil
}

// HasSeen checks if an item ID has already been processed.
func (s *DedupStore) HasSeen(itemID string) bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM seen_items WHERE id = ?", itemID).Scan(&count)
	if err != nil {
		log.Printf("[STORE] Error checking item %s: %v", itemID, err)
		return false
	}
	return count > 0
}

// MarkSeen records an item as processed.
func (s *DedupStore) MarkSeen(itemID, brand, name string, price int) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO seen_items (id, brand, name, price, seen_at) VALUES (?, ?, ?, ?, ?)",
		itemID, brand, name, price, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("marking item seen: %w", err)
	}
	return nil
}

// Count returns the total number of seen items.
func (s *DedupStore) Count() int {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM seen_items").Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// cleanup removes entries older than 7 days to prevent unbounded growth.
func (s *DedupStore) cleanup() {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	result, err := s.db.Exec("DELETE FROM seen_items WHERE seen_at < ?", cutoff)
	if err != nil {
		log.Printf("[STORE] Cleanup error: %v", err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("[STORE] Cleaned up %d old entries", rows)
	}
}

// Close closes the database connection.
func (s *DedupStore) Close() error {
	return s.db.Close()
}
