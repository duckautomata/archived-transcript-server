package internal

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Initializes all server endpoints and any protected middleware
func (a *App) InitServerEndpoints(mux *http.ServeMux) {
	// API Key protected routes
	mux.HandleFunc("POST /transcript", a.apiKeyMiddleware(a.gzipMiddleware(a.handlePostTranscript)))
	mux.HandleFunc("GET /membership/{channelName}", a.apiKeyMiddleware(a.handleGetMembershipKeys))
	mux.HandleFunc("POST /membership/{channelName}", a.apiKeyMiddleware(a.handleCreateMembershipKey))
	mux.HandleFunc("DELETE /membership/{channelName}", a.apiKeyMiddleware(a.handleDeleteMembershipKeys))
	mux.HandleFunc("GET /membership", a.apiKeyMiddleware(a.handleGetAllMembershipKeys))

	// Public routes
	mux.HandleFunc("GET /info", a.handleGetInfo)
	mux.HandleFunc("GET /statuscheck", a.handleStatusCheck)
	mux.HandleFunc("GET /healthcheck", a.handleHealthCheck)
	mux.Handle("/metrics", promhttp.Handler())

	// Membership protected public routes (only the members transcript is protected)
	mux.HandleFunc("GET /stream/{id}", a.membershipMiddleware(a.handleGetStreamMetadata))
	mux.HandleFunc("GET /transcript/{id}", a.membershipMiddleware(a.handleGetTranscript))
	mux.HandleFunc("GET /transcripts", a.membershipMiddleware(a.handleSearchTranscripts))
	mux.HandleFunc("GET /graph/{id}", a.membershipMiddleware(a.handleGetGraphByID))
	mux.HandleFunc("GET /graph", a.membershipMiddleware(a.handleGetGraphAll))
	mux.HandleFunc("GET /membership/verify", a.membershipMiddleware(a.handleVerifyMembershipKey))
}

// Checks if the membership key is valid. If valid, injects the authorized channel into the request context
// Continues if key is valid or invalid.
//
//	r.Context().Value(AuthorizedChannelKey) -> channel string, ok bool
func (a *App) membershipMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Membership-Key")
		if key == "" {
			next(w, r)
			return
		}

		ctx := r.Context()
		channel, err := a.VerifyMembershipKey(ctx, key)
		if err != nil {
			slog.Error("failed to verify membership key", "key", key, "err", err)
			next(w, r)
			return
		}
		if channel == "" { // Invalid Key
			next(w, r)
			return
		}

		// Valid Key -> Inject AuthorizedChannel
		ctx = context.WithValue(ctx, AuthorizedChannelKey, channel)
		next(w, r.WithContext(ctx))
	}
}

// Checks if the API key is valid. If invalid, returns 401.
func (a *App) apiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.config.APIKey == "" {
			next(w, r)
			return
		}
		key := r.Header.Get("X-API-Key")
		if key != a.config.APIKey {
			http.Error(w, "Forbidden", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Check for gzip content encoding and wrap body if present
func (a *App) gzipMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") == "gzip" {
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				Http400Errors.Inc()
				writeError(w, http.StatusBadRequest, "Invalid gzip body")
				return
			}
			defer reader.Close()
			r.Body = reader
		}
		next(w, r)
	}
}

// Adds the necessary CORS headers to all responses.
// Accepts any origin.
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set the allowed origin.
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Set the allowed methods
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		// Set the allowed headers
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Membership-Key, Authorization")

		// Handle preflight OPTIONS requests
		// This is sent by the browser to check permissions *before*
		// sending the actual request.
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Serve the next handler in the chain
		next.ServeHTTP(w, r)
	})
}

// Adds the transcript to the database. Handles duplicate transcripts. Protected by API key.
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
	PostTranscriptProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	PostTranscriptRequests.Inc()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"status": "ok", "id": input.ID})
}

// Returns a single transcript in json format. Membership is protected.
func (a *App) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		Http400Errors.Inc()
		return
	}

	transcript, noRows, err := a.retrieveTranscript(ctx, id)
	if err != nil {
		if noRows {
			Http400Errors.Inc()
			writeError(w, http.StatusNotFound, err.Error())
			return
		}

		slog.Error("failed to retrieve transcript", "id", id, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to retrieve transcript")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	GetTranscriptProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetTranscriptRequests.Inc()
	writeJSON(w, transcript)
}

// Performs a filtered search across all transcripts. Membership is protected.
func (a *App) handleSearchTranscripts(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	queryData := parseQueryData(r)
	results, err := a.queryTranscripts(ctx, queryData)
	if err != nil {
		slog.Error("failed to query transcripts", "params", queryData, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to search transcripts")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	SearchTranscriptsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	SearchTranscriptsRequests.Inc()
	writeJSON(w, results)
}

// Returns time-frequency data for one transcript based on the query parameters. Membership is protected.
func (a *App) handleGetGraphByID(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		return
	}

	queryData := parseQueryData(r)
	if queryData.SearchText == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Search text is required")
		return
	}

	graphData, err := a.querySingleGraph(ctx, id, queryData)
	if err != nil {
		slog.Error("failed to query single graph", "id", id, "params", queryData, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get graph data")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	GetGraphProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetGraphRequests.Inc()
	writeJSON(w, graphData)
}

// Returns date-frequency data across all filtered transcripts. Membership is protected.
func (a *App) handleGetGraphAll(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	queryData := parseQueryData(r)
	if queryData.SearchText == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Search text is required")
		return
	}

	graphData, err := a.queryAllGraphs(ctx, queryData)
	if err != nil {
		slog.Error("failed to query all graphs", "params", queryData, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get graph data")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	GetAllGraphProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetAllGraphRequests.Inc()
	writeJSON(w, graphData)
}

// Returns the metadata for a single stream. Membership is protected.
func (a *App) handleGetStreamMetadata(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Transcript ID is required")
		return
	}

	streamData, noRows, err := a.retrieveStreamMetadata(ctx, id)
	if err != nil {
		if noRows {
			Http400Errors.Inc()
			writeError(w, http.StatusNotFound, err.Error())
			return
		}

		slog.Error("failed to query stream metadata", "id", id, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get stream metadata")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	GetStreamMetadataProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	GetStreamMetadataRequests.Inc()
	writeJSON(w, streamData)
}

// Returns a list of all streams. Open
func (a *App) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	results, err := a.retrieveAllStreams(ctx)
	if err != nil {
		slog.Error("failed to retrieve all streams", "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to retrieve streams")
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	writeJSON(w, results)
}

// Returns the total number of transcripts. Open
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

// Returns a simple OK. Open
func (a *App) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // Get the request's context
	w.Header().Set("Content-Type", "application/json")

	// Use PingContext to respect request timeouts/cancellations
	if err := a.db.PingContext(ctx); err != nil {
		Http500Errors.Inc()
		slog.Error("failed to ping database", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// Returns all keys for a channel. Protected by API key.
func (a *App) handleGetMembershipKeys(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channelName")
	if channel == "" {
		writeError(w, http.StatusBadRequest, "Channel name is required")
		return
	}

	keys, err := a.GetMembershipKeys(r.Context(), channel)
	if err != nil {
		slog.Error("failed to get membership keys", "channel", channel, "err", err)
		writeError(w, http.StatusInternalServerError, "Failed to get keys")
		return
	}

	var resp []KeyResponse
	for k, v := range keys {
		resp = append(resp, KeyResponse{Key: k, ExpiresAt: v.Format(time.RFC3339)})
	}
	writeJSON(w, resp)
}

// Creates a new key for a channel. Protected by API key.
func (a *App) handleCreateMembershipKey(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channelName")
	if channel == "" {
		writeError(w, http.StatusBadRequest, "Channel name is required")
		return
	}

	// Only allow keys for channels in the config.
	found := slices.Contains(a.config.Membership, channel)
	if !found {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Channel not found in config")
		return
	}

	key, expiry, err := a.CreateMembershipKey(r.Context(), channel)
	if err != nil {
		slog.Error("failed to create membership key", "channel", channel, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to create key")
		return
	}

	writeJSON(w, map[string]string{
		"key":       key,
		"expiresAt": expiry.Format(time.RFC3339),
	})
}

// Deletes all keys for a channel. Protected by API key.
func (a *App) handleDeleteMembershipKeys(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channelName")
	if channel == "" {
		Http400Errors.Inc()
		writeError(w, http.StatusBadRequest, "Channel name is required")
		return
	}

	if err := a.DeleteMembershipKeys(r.Context(), channel); err != nil {
		slog.Error("failed to delete membership keys", "channel", channel, "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to delete keys")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Returns all keys for all channels. Protected by API key.
func (a *App) handleGetAllMembershipKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.GetAllMembershipKeys(r.Context())
	if err != nil {
		slog.Error("failed to get all membership keys", "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Failed to get all keys")
		return
	}

	resp := make(map[string][]KeyResponse)
	for channel, kMap := range keys {
		for k, v := range kMap {
			resp[channel] = append(resp[channel], KeyResponse{Key: k, ExpiresAt: v.Format(time.RFC3339)})
		}
	}
	writeJSON(w, resp)
}

// Verifies if a key is valid for a channel. Open
func (a *App) handleVerifyMembershipKey(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()
	key := r.Header.Get("X-Membership-Key")
	if key == "" {
		Http400Errors.Inc()
		http.Error(w, "Missing X-Membership-Key header", http.StatusUnauthorized)
		return
	}

	channel, ok := ctx.Value(AuthorizedChannelKey).(string)
	if !ok {
		Http400Errors.Inc()
		http.Error(w, "Invalid or expired key", http.StatusUnauthorized)
		return
	}

	// when we get here, we know the key is valid for the channel. Now we just need to grab the expiry time.

	keys, err := a.GetMembershipKeys(ctx, channel)
	if err != nil {
		slog.Error("failed to get keys for verified channel", "err", err)
		Http500Errors.Inc()
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	expiry, ok := keys[key]
	if !ok {
		slog.Error("Key was verified, but GetMembershipKeys return map did not have it", "channel", channel, "key", key, "keys", keys)
		Http500Errors.Inc()
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	RequestsProcessingDuration.Observe(time.Since(startTime).Seconds())
	VerifyMembershipProcessingDuration.Observe(time.Since(startTime).Seconds())
	TotalRequests.Inc()
	VerifyMembershipRequests.Inc()
	writeJSON(w, map[string]string{
		"channel":   channel,
		"expiresAt": expiry.Format(time.RFC3339),
	})
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
