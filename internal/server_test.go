package internal

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestServer_InvalidMethod(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	makeRequest := func(client *http.Client, method string, url string) (*http.Response, error) {
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	}

	client := ts.Client()
	tests := []struct {
		method string
		url    string
		code   int
	}{
		{"GET", ts.URL + "/healthcheck", http.StatusOK},
		{"POST", ts.URL + "/healthcheck", http.StatusMethodNotAllowed},
		{"PUT", ts.URL + "/healthcheck", http.StatusMethodNotAllowed},
		{"DELETE", ts.URL + "/healthcheck", http.StatusMethodNotAllowed},

		{"GET", ts.URL + "/statuscheck", http.StatusOK},
		{"POST", ts.URL + "/statuscheck", http.StatusMethodNotAllowed},
		{"PUT", ts.URL + "/statuscheck", http.StatusMethodNotAllowed},
		{"DELETE", ts.URL + "/statuscheck", http.StatusMethodNotAllowed},

		{"GET", ts.URL + "/info", http.StatusOK},
		{"POST", ts.URL + "/info", http.StatusMethodNotAllowed},
		{"PUT", ts.URL + "/info", http.StatusMethodNotAllowed},
		{"DELETE", ts.URL + "/info", http.StatusMethodNotAllowed},

		{"GET", ts.URL + "/stream/v1", http.StatusNotFound},
		{"POST", ts.URL + "/stream/v1", http.StatusMethodNotAllowed},
		{"PUT", ts.URL + "/stream/v1", http.StatusMethodNotAllowed},
		{"DELETE", ts.URL + "/stream/v1", http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		resp, err := makeRequest(client, test.method, test.url)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != test.code {
			t.Errorf("Error for %s %s: Expected response code %d, got %d", test.method, test.url, test.code, resp.StatusCode)
		}
	}
}

func TestServer_HandlePostTranscript_Upsert(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()

	// 1. Post initial transcript
	v1Input := `{"id":"v1","streamer":"StreamerA","date":"2023-01-01","streamType":"Stream","streamTitle":"Initial Title","srt":"1\n00:00:01,000 --> 00:00:02,000\nInitial text"}`
	req1, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(v1Input))
	req1.Header.Set("X-API-Key", app.config.APIKey)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("First POST failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("First POST expected 201, got %d", resp1.StatusCode)
	}

	// Verify initial state
	ctx := context.Background()
	tr1, _, err := app.retrieveTranscript(ctx, "v1")
	if err != nil {
		t.Fatalf("Failed to retrieve v1 initial: %v", err)
	}
	if tr1.StreamTitle != "Initial Title" {
		t.Errorf("Expected initial title 'Initial Title', got '%s'", tr1.StreamTitle)
	}
	if len(tr1.TranscriptLines) != 1 || tr1.TranscriptLines[0].Text != "Initial text" {
		t.Errorf("Expected initial text 'Initial text', got '%v'", tr1.TranscriptLines)
	}

	// 2. Post updated transcript with SAME ID
	v2Input := `{"id":"v1","streamer":"StreamerA","date":"2023-01-01","streamType":"Stream","streamTitle":"Updated Title","srt":"1\n00:00:01,000 --> 00:00:02,000\nUpdated text\n\n2\n00:00:03,000 --> 00:00:04,000\nNew text"}`
	req2, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(v2Input))
	req2.Header.Set("X-API-Key", app.config.APIKey)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Second POST failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("Second POST expected 201 (Upsert), got %d", resp2.StatusCode)
	}

	// Verify updated state
	tr2, _, err := app.retrieveTranscript(ctx, "v1")
	if err != nil {
		t.Fatalf("Failed to retrieve v1 updated: %v", err)
	}
	if tr2.StreamTitle != "Updated Title" {
		t.Errorf("Expected updated title 'Updated Title', got '%s'", tr2.StreamTitle)
	}
	if len(tr2.TranscriptLines) != 2 {
		t.Errorf("Expected 2 updated lines, got %d", len(tr2.TranscriptLines))
	}
	if tr2.TranscriptLines[0].Text != "Updated text" {
		t.Errorf("Expected updated line 1 'Updated text', got '%s'", tr2.TranscriptLines[0].Text)
	}

	// Verify no duplicates in DB (only 1 transcript with ID v1)
	count := 0
	app.db.QueryRow("SELECT COUNT(*) FROM transcripts WHERE id = 'v1'").Scan(&count)
	if count != 1 {
		t.Errorf("Expected exactly 1 transcript with id v1, got %d", count)
	}

	// Verify no duplicate lines in transcript_lines
	lineCount := 0
	app.db.QueryRow("SELECT COUNT(*) FROM transcript_lines WHERE transcript_id = 'v1'").Scan(&lineCount)
	if lineCount != 2 {
		t.Errorf("Expected exactly 2 transcript lines for v1, got %d", lineCount)
	}
}

func TestServer_CreateTranscript_Validation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name:           "Success",
			body:           `{"id":"v1", "streamer":"S1", "date":"2023-01-01"}`,
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "Missing ID",
			body:           `{"streamer":"S1", "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid ID (empty)",
			body:           `{"id":"", "streamer":"S1", "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid ID (Non-string)",
			body:           `{"id":123, "streamer":"S1", "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Missing Streamer",
			body:           `{"id":"v1", "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid Streamer (empty)",
			body:           `{"id":"v1", "streamer":"", "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid Streamer (non-string)",
			body:           `{"id":"v1", "streamer":123, "date":"2023-01-01"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Missing Date",
			body:           `{"id":"v1", "streamer":"S1"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid Date (empty)",
			body:           `{"id":"v1", "streamer":"S1", "date":""}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid Date (non-string)",
			body:           `{"id":"v1", "streamer":"S1", "date":123}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid Date (invalid format)",
			body:           `{"id":"v1", "streamer":"S1", "date":"invalid-date"}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(tt.body))
			req.Header.Set("X-API-Key", app.config.APIKey)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestServer_GetTranscript_Validation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	// Seed a transcript for success case
	seedBody := `{"id":"v1", "streamer":"S1", "date":"2023-01-01"}`
	req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(seedBody))
	req.Header.Set("X-API-Key", app.config.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to seed transcript: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to seed transcript, got status: %d", resp.StatusCode)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "Success",
			path:           "/transcript/v1",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Not Found",
			path:           "/transcript/missing_id",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Empty ID",
			path:           "/transcript/",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+tt.path, nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestServer_GetGraphByID_Validation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	// Seed a transcript for success case
	seedBody := `{"id":"v1", "streamer":"S1", "date":"2023-01-01", "srt":"1\n00:00:01,000 --> 00:00:02,000\nHello world"}`
	req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(seedBody))
	req.Header.Set("X-API-Key", app.config.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to seed transcript: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to seed transcript, got status: %d", resp.StatusCode)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "Success",
			path:           "/graph/v1?searchText=world",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Missing SearchText",
			path:           "/graph/v1",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+tt.path, nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestServer_GetGraphAll_Validation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	// Seed a transcript for success case
	seedBody := `{"id":"v1", "streamer":"S1", "date":"2023-01-01", "srt":"1\n00:00:01,000 --> 00:00:02,000\nHello world"}`
	req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(seedBody))
	req.Header.Set("X-API-Key", app.config.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to seed transcript: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to seed transcript, got status: %d", resp.StatusCode)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "Success",
			path:           "/graph?searchText=world",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Missing SearchText",
			path:           "/graph",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+tt.path, nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestServer_APIKeyMiddleware(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	reqBody := `{"id":"test","streamer":"test","date":"2023-01-01","srt":""}`

	t.Run("Missing API Key", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(reqBody))
		// No Header Set
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	})

	t.Run("Invalid API Key", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(reqBody))
		req.Header.Set("X-API-Key", "wrong-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	})

	t.Run("Valid API Key", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/transcript", strings.NewReader(reqBody))
		req.Header.Set("X-API-Key", app.config.APIKey)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected 201 Created, got %d", resp.StatusCode)
		}
	})
}

func TestServer_HandlePostTranscript_Gzip(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	// JSON payload
	body := `{"id":"gzip-id", "streamer":"StreamerGzip", "date":"2023-01-01", "srt":"content"}`

	// Compress payload
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte(body))
	gw.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/transcript", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-API-Key", app.config.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d", resp.StatusCode)
	}

	// Verify it exists
	ctx := context.Background()
	tr, noRows, err := app.retrieveTranscript(ctx, "gzip-id")
	if err != nil {
		t.Fatalf("Failed to retrieve transcript: %v", err)
	}
	if noRows {
		t.Error("Expected transcript to be found")
	}
	if tr.Streamer != "StreamerGzip" {
		t.Errorf("Expected streamer 'StreamerGzip', got '%s'", tr.Streamer)
	}
}

func TestServer_HandlePostTranscript_Zstd(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	// JSON payload
	body := `{"id":"zstd-id", "streamer":"StreamerZstd", "date":"2023-01-01", "srt":"content"}`

	// Compress payload
	var buf bytes.Buffer
	enc, _ := zstd.NewWriter(&buf)
	enc.Write([]byte(body))
	enc.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/transcript", &buf)
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("X-API-Key", app.config.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d", resp.StatusCode)
	}

	// Verify it exists
	ctx := context.Background()
	tr, noRows, err := app.retrieveTranscript(ctx, "zstd-id")
	if err != nil {
		t.Fatalf("Failed to retrieve transcript: %v", err)
	}
	if noRows {
		t.Error("Expected transcript to be found")
	}
	if tr.Streamer != "StreamerZstd" {
		t.Errorf("Expected streamer 'StreamerZstd', got '%s'", tr.Streamer)
	}
}
