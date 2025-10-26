package internal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (a *App) InitServerEndpoints(mux *http.ServeMux) {
	mux.Handle("POST /transcript", a.apiKeyMiddleware(http.HandlerFunc(a.handlePostTranscript)))
	mux.HandleFunc("GET /transcript/{id}", a.handleGetTranscript)
	mux.HandleFunc("GET /transcripts", a.handleSearchTranscripts)
	mux.HandleFunc("GET /graph/{id}", a.handleGetGraphByID)
	mux.HandleFunc("GET /stream/{id}", a.handleGetStreamMetadata)
	mux.HandleFunc("GET /graph", a.handleGetGraphAll)
	mux.HandleFunc("GET /statuscheck", a.handleStatusCheck)
	mux.HandleFunc("GET /healthcheck", a.handleHealthCheck)
	mux.Handle("/metrics", promhttp.Handler())
}

func (a *App) apiKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.config.Credentials.Upload == "" {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("X-API-Key")
		if key != a.config.Credentials.Upload {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds the necessary CORS headers to all responses.
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set the allowed origin.
		// Use "*" for development to allow any origin.
		// For production, you should lock this down to your frontend's domain:
		// w.Header().Set("Access-Control-Allow-Origin", "http://your-frontend-domain.com")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Set the allowed methods
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		// Set the allowed headers
		// This is crucial for your X-API-Key and JSON POSTs
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")

		// Handle preflight OPTIONS requests
		// This is sent by the browser to check permissions *before*
		// sending the actual POST request with the X-API-Key.
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Serve the next handler in the chain
		next.ServeHTTP(w, r)
	})
}

// handlePostTranscript ingests a new transcript.
func (a *App) handlePostTranscript(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	var input TranscriptInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		slog.Error("failed to decode post transcript body", "err", err)
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.ID == "" || input.Streamer == "" || input.Date == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Missing required fields: id, streamer, date")
		return
	}

	if _, err := time.Parse("2006-01-02", input.Date); err != nil {
		Http400Errors.Inc()
		http.Error(w, "Invalid date format. Expected YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	// Insert into Database
	if err := a.insertTranscript(ctx, &input); err != nil {
		slog.Error("failed to insert transcript", "id", input.ID, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to save transcript")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	PostTranscriptRequests.Inc()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"status": "ok", "id": input.ID})
}

// handleGetTranscript returns a single transcript's formatted lines.
func (a *App) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		Http400Errors.Inc()
		return
	}

	transcript, err := a.retrieveTranscript(ctx, id)
	if err != nil {
		// Check if the error indicates "not found"
		if strings.Contains(err.Error(), "not found") {
			Http400Errors.Inc()
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			slog.Error("failed to retrieve transcript", "id", id, "err", err)
			Http500Errors.Inc()
			writeError(w, http.StatusInternalServerError, "Failed to retrieve transcript")
		}
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetTranscriptRequests.Inc()
	writeJSON(w, transcript)
}

// handleSearchTranscripts performs a filtered search across transcripts.
func (a *App) handleSearchTranscripts(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	results, err := a.queryTranscripts(ctx, r.URL.Query())
	if err != nil {
		slog.Error("failed to query transcripts", "params", r.URL.Query(), "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to search transcripts")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	SearchTranscriptsRequests.Inc()
	writeJSON(w, results)
}

// handleGetGraphByID returns time-frequency data for one transcript.
func (a *App) handleGetGraphByID(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		return
	}

	query := r.URL.Query()
	searchText := query.Get("searchText")
	matchWholeWord, _ := strconv.ParseBool(query.Get("matchWholeWord"))

	graphData, err := a.querySingleGraph(ctx, id, searchText, matchWholeWord)
	if err != nil {
		slog.Error("failed to query single graph", "id", id, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get graph data")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetGraphRequests.Inc()
	writeJSON(w, graphData)
}

// handleGetGraphAll returns date-frequency data across all filtered transcripts.
func (a *App) handleGetGraphAll(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	graphData, err := a.queryAllGraphs(ctx, r.URL.Query())
	if err != nil {
		slog.Error("failed to query all graphs", "params", r.URL.Query(), "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get graph data")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetAllGraphRequests.Inc()
	writeJSON(w, graphData)
}

// handleStatusCheck returns the total number of transcripts.
func (a *App) handleGetStreamMetadata(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		return
	}

	streamhData, err := a.retrieveStreamMetadata(ctx, id)
	if err != nil {
		slog.Error("failed to query stream metadata", "id", id, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get stream metadata")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetStreamMetadataRequests.Inc()
	writeJSON(w, streamhData)
}

// handleStatusCheck returns the total number of transcripts.
func (a *App) handleStatusCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // Get the request's context
	var count int

	// Use QueryRowContext to respect request timeouts/cancellations
	err := a.db.QueryRowContext(ctx, "SELECT COUNT(id) FROM transcripts").Scan(&count)
	if err != nil {
		slog.Error("failed to get status count", "err", err)
		writeError(w, http.StatusInternalServerError, "Failed to get status")
		return
	}
	writeJSON(w, map[string]int{"transcriptCount": count})
}

// handleHealthCheck returns a simple OK.
func (a *App) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // Get the request's context
	w.Header().Set("Content-Type", "application/json")

	// Use PingContext to respect request timeouts/cancellations
	if err := a.db.PingContext(ctx); err != nil {
		http.Error(w, `{"status":"error","db_error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// --- HTTP Helper Functions ---

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to write JSON response", "err", err)
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": message})
}

// --- Utility Functions ---

// formatDate converts "YYYYMMDD" to "YYYY-MM-DD"
func formatDate(yyyymmdd string) (string, error) {
	if len(yyyymmdd) != 8 {
		return "", nil // Return empty string, let query ignore it
	}
	// Use a lightweight builder, no need for time.Parse
	var sb strings.Builder
	sb.Grow(10)
	sb.WriteString(yyyymmdd[0:4])
	sb.WriteByte('-')
	sb.WriteString(yyyymmdd[4:6])
	sb.WriteByte('-')
	sb.WriteString(yyyymmdd[6:8])
	return sb.String(), nil
}
