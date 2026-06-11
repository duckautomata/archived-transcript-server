package main

import (
	"archived-transcript-server/internal"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Set via environment variables VERSION and BUILD_TIME
var (
	Version   = "local"
	BuildTime = "unknown"
)

func main() {
	if v := os.Getenv("VERSION"); v != "" {
		Version = v
	}
	if bt := os.Getenv("BUILD_TIME"); bt != "" {
		BuildTime = bt
	}

	// --- Logging Setup ---
	// lumberjack creates the log directory and file lazily on first write, so no
	// manual MkdirAll/OpenFile is needed. The file rotates at 1MB and rotated
	// backups are kept uncompressed under tmp/_logs.
	logPath := filepath.Join("tmp", "_logs", "server.log")
	logCloser := internal.SetupLogging(logPath)
	defer logCloser.Close()

	slog.Info("========== SERVER START ==========")
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
	app := internal.NewApp(db, config, Version, BuildTime)

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

	// --- Signal handling / graceful shutdown ---
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server listening on port 8080", "func", "main")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "func", "main", "err", err)
			os.Exit(1)
		}
	}()

	<-stop // block until SIGINT/SIGTERM

	slog.Info("========== SERVER STOP ==========")
	slog.Info("shutting down server...", "func", "main")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "func", "main", "err", err)
	}

	slog.Info("server exited cleanly", "func", "main")
}
