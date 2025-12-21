package internal

import (
	"database/sql"
	"regexp"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestApp creates an App instance with in-memory DB and test config.
// This is shared across multiple test files.
func setupTestApp(t *testing.T) *App {
	db, err := sql.Open("sqlite3_with_regex", ":memory:")

	if err != nil {
		t.Fatalf("Failed to open memory db: %v", err)
	}

	app := &App{
		db: db,
		config: Config{
			APIKey:     "456",
			Membership: []string{"TestStreamer"},
			KeyTTLDays: 30,
		},
		regexCache:   make(map[string]*regexp.Regexp),
		regexCacheMu: sync.Mutex{},
	}

	if err := app.InitDB(); err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}

	return app
}
