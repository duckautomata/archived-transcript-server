package main

import (
	"archived-transcript-server/internal"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
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
	// Use WAL mode for high read concurrency, mmap for faster reads, and synchronous=NORMAL for speed.
	dbPath := filepath.Join("tmp", "transcripts.db")

	config, err := internal.GetConfig()
	if err != nil {
		slog.Error("unable to read in config", "func", "main", "err", err)
		os.Exit(1)
	}

	db, err := internal.InitDB(dbPath, config.Database)
	if err != nil {
		slog.Error("unable to initialize database", "func", "main", "path", dbPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Verify connection and REGEXP support
	ctx := context.Background()
	var works bool
	err = db.QueryRowContext(ctx, "SELECT REGEXP('a', 'abc')").Scan(&works)
	if err != nil {
		slog.Error("Failed to verify REGEXP function on initial connection", "func", "main", "err", err)
		// We can choose to exit or continue, but if regex fails, search will break.
		os.Exit(1)
	}
	slog.Info("Database opened and REGEXP function verified.", "func", "main", "path", dbPath)

	// App Setup
	app := internal.NewApp(db, config)

	// Ensure membership keys exist for configured channels
	if err := app.EnsureMembershipKeys(ctx); err != nil {
		slog.Error("failed to ensure membership keys", "func", "main", "err", err)
	}

	// --- Server Setup ---
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	corsHandler := internal.CorsMiddleware(mux)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           corsHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	slog.Info("server listening on port 8080", "func", "main")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
