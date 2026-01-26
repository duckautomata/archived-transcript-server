package internal

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestApp creates an App instance with in-memory DB and test config.
// This is shared across multiple test files.
// setupTestApp creates an App instance with in-memory DB and test config.
// This is shared across multiple test files.
func setupTestApp(t *testing.T) *App {
	config := Config{
		APIKey:     "456",
		Membership: []string{"TestStreamer"},
		KeyTTLDays: 30,
		Database: DatabaseConfig{
			// In-memory DBs don't need WAL, but setting it explicitly is fine.
			// Using defaults for others.
			JournalMode:   "MEMORY",
			Synchronous:   "OFF",
			TempStore:     "MEMORY",
			BusyTimeoutMS: 5000,
		},
	}

	db, err := InitDB(":memory:", config.Database)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}

	return NewApp(db, config)
}
