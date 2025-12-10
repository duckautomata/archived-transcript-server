package main

import (
	"archived-transcript-server/internal"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mattn/go-sqlite3"
)

// To be set via ldflags
var (
	Version   = "local"
	BuildTime = "unknown"
)

func main() {
	// --- Logging Setup ---
	if err := os.MkdirAll("tmp", 0755); err != nil {
		slog.Error("failed to create log directory", "func", "main", "path", "tmp", "err", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	fileName := fmt.Sprintf("%s-server.log", timestamp)
	filePath := filepath.Join("tmp", fileName)

	// Open the log file.
	logFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		slog.Error("unable to open log file", "func", "main", "path", filePath, "err", err)
	}
	defer logFile.Close()

	internal.SetupLogging(logFile)

	slog.Info("server starting up", "version", Version, "build_time", BuildTime)

	// --- Database Setup ---
	// Use WAL mode for high read concurrency
	dbPath := filepath.Join("tmp", "transcripts.db")
	dbSource := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath)
	connector := &sqliteConnectorWithRegexp{
		dsn: dbSource,
	}
	db := sql.OpenDB(connector)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		slog.Error("Failed to get initial connection for check", "func", "main", "err", err)
		return
	}
	// Verify regexp works on this connection
	var works bool
	err = conn.QueryRowContext(ctx, "SELECT REGEXP('a', 'abc')").Scan(&works)
	if err != nil {
		slog.Error("Failed to verify REGEXP function on initial connection", "func", "main", "err", err)
		return
	}
	conn.Close() // Release the test connection
	slog.Info("Database opened and REGEXP function verified.", "func", "main", "path", dbSource)
	defer db.Close()

	// Set connection pool settings for performance
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	app, err := internal.NewApp(db)
	if err != nil {
		slog.Error("failed to initialize app", "func", "main", "err", err)
		os.Exit(1)
	}
	err = app.InitDB()
	if err != nil {
		slog.Error("failed to initialize database", "func", "main", "err", err)
		os.Exit(1)
	}

	// --- Server Setup ---
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	corsHandler := internal.CorsMiddleware(mux)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsHandler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  4 * time.Minute,
	}

	slog.Info("server listening on port 8080", "func", "main")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

type sqliteConnectorWithRegexp struct {
	dsn string
}

func (c *sqliteConnectorWithRegexp) Connect(ctx context.Context) (driver.Conn, error) {
	// Establish the base connection
	baseConn, err := (&sqlite3.SQLiteDriver{}).Open(c.dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open base sqlite connection: %w", err)
	}

	// Get the underlying *sqlite3.SQLiteConn
	sqliteConn := baseConn.(*sqlite3.SQLiteConn)

	// Register the function on this specific connection
	err = sqliteConn.RegisterFunc("REGEXP", regexpImpl, true)
	if err != nil {
		baseConn.Close() // Close connection if registration fails
		return nil, fmt.Errorf("failed to register REGEXP function on new connection: %w", err)
	}

	// Return the connection (which is now driver.Conn compatible)
	return baseConn, nil
}

func (c *sqliteConnectorWithRegexp) Driver() driver.Driver {
	return &sqlite3.SQLiteDriver{}
}

func regexpImpl(pattern, s string) (bool, error) {
	// Compile the regex pattern passed from SQL
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Log the error for visibility
		slog.Error("Failed to compile regex in custom REGEXP function", "pattern", pattern, "err", err)
		// Return false and NO error to SQL. SQL functions shouldn't usually return errors.
		// Returning an error here might abort the whole SQL query.
		return false, nil
	}

	// Perform the match
	matched := re.MatchString(s)

	return matched, nil // Return the boolean result and nil error
}
