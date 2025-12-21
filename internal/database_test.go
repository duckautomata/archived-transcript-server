package internal

import (
	"context"
	"sort"
	"testing"
)

// seedDBForQueryTests inserts a varied set of transcripts for testing filters.
func seedDBForQueryTests(t *testing.T, app *App) {
	ctx := context.Background()
	inputs := []TranscriptInput{
		{
			ID:            "v1",
			Streamer:      "StreamerA",
			Date:          "2023-01-01",
			StreamType:    "Stream",
			StreamTitle:   "StreamerA First Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nHello from StreamerA\n\n",
		},
		{
			ID:            "v2",
			Streamer:      "StreamerA",
			Date:          "2023-02-01",
			StreamType:    "VOD",
			StreamTitle:   "StreamerA Some VOD",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nThis is a vod content\n\n",
		},
		{
			ID:            "v3",
			Streamer:      "StreamerB",
			Date:          "2023-01-15",
			StreamType:    "Stream",
			StreamTitle:   "StreamerB Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nHello from StreamerB\n\n",
		},
		{
			ID:            "v4",
			Streamer:      "StreamerA",
			Date:          "2023-03-01",
			StreamType:    "Other",
			StreamTitle:   "StreamerA Other Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nOther stream contents\n\n",
		},
		{
			ID:            "v5",
			Streamer:      "StreamerC",
			Date:          "2023-01-01",
			StreamType:    "Stream",
			StreamTitle:   "StreamerC Unique Title",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nSpecific unique word here hel\n\n",
		},
	}

	for _, in := range inputs {
		if err := app.insertTranscript(ctx, &in); err != nil {
			t.Fatalf("Failed to insert %s: %v", in.ID, err)
		}
	}
}

func TestDatabase_InitAndInsert(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// Initial check
	streams, err := app.retrieveAllStreams(ctx)
	if err != nil {
		t.Fatalf("retrieveAllStreams failed: %v", err)
	}
	if len(streams) != 0 {
		t.Errorf("Expected 0 streams initially, got %d", len(streams))
	}

	// Insert one
	input := TranscriptInput{
		ID:            "test1",
		Streamer:      "Tester",
		Date:          "2023-01-01",
		StreamTitle:   "Test Title",
		StreamType:    "Stream",
		SrtTranscript: "1\n00:00:01,000 --> 00:00:05,000\nHello\n\n",
	}
	if err := app.insertTranscript(ctx, &input); err != nil {
		t.Fatalf("insertTranscript failed: %v", err)
	}

	// Retrieve
	out, err := app.retrieveAllStreams(ctx)
	if err != nil {
		t.Fatalf("retrieveAllStreams failed after insert: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("Expected 1 stream, got %d", len(out))
	} else {
		if out[0].ID != "test1" {
			t.Errorf("Expected ID test1, got %s", out[0].ID)
		}
	}
}

func TestDatabase_QueryTranscripts(t *testing.T) {
	app := setupTestApp(t)
	seedDBForQueryTests(t, app)
	defer app.db.Close()
	ctx := context.Background()

	// Streamer and StreamType are case-sensitive. Everything else is case-insensitive.
	tests := []struct {
		name          string
		query         QueryData
		expectedIDs   []string
		expectContext bool
	}{
		{
			name:        "All Streams (No Filter)",
			query:       QueryData{},
			expectedIDs: []string{"v2", "v3", "v1", "v5", "v4"}, // v2(Feb1), v3(Jan15), v1/v5(Jan1), v4(Mar1)
		},
		{
			name:        "Filter by StreamerB",
			query:       QueryData{Streamer: "StreamerB"},
			expectedIDs: []string{"v3"},
		},
		{
			name:        "Filter by StreamerB (Case Insensitive)",
			query:       QueryData{Streamer: "streamerb"},
			expectedIDs: []string{},
		},
		{
			name:        "Filter by StreamerA",
			query:       QueryData{Streamer: "StreamerA"},
			expectedIDs: []string{"v2", "v1", "v4"},
		},
		{
			name:        "Filter by StreamType VOD",
			query:       QueryData{StreamTypes: []string{"VOD"}},
			expectedIDs: []string{"v2"},
		},
		{
			name:        "Filter by StreamType VOD (Case Insensitive)",
			query:       QueryData{StreamTypes: []string{"vod"}},
			expectedIDs: []string{},
		},
		{
			name:        "Filter by StreamType Other",
			query:       QueryData{StreamTypes: []string{"Other"}},
			expectedIDs: []string{"v4"},
		},
		{
			name:        "Filter by StreamType NonExistent",
			query:       QueryData{StreamTypes: []string{"NonExistent"}},
			expectedIDs: []string{},
		},
		{
			name:        "Filter by Date Range",
			query:       QueryData{FromDate: "2023-01-02", ToDate: "2023-02-02"},
			expectedIDs: []string{"v2", "v3"}, // v2 is Feb 1, v3 is Jan 15. v1/v5 are Jan 1 (out). v4 is Mar 1 (out)
		},
		{
			name:        "Filter by out of bounds Date Range",
			query:       QueryData{FromDate: "2000-01-02", ToDate: "2000-02-02"},
			expectedIDs: []string{},
		},
		{
			name:        "Filter by Title",
			query:       QueryData{StreamTitle: "Unique"},
			expectedIDs: []string{"v5"},
		},
		{
			name:        "Filter by Title (Case Insensitive)",
			query:       QueryData{StreamTitle: "unique"},
			expectedIDs: []string{"v5"},
		},
		{
			name:        "Filter by Title (No Match)",
			query:       QueryData{StreamTitle: "Invalid"},
			expectedIDs: []string{},
		},
		{
			name:        "Search Text - Partial",
			query:       QueryData{SearchText: "Hel"},
			expectedIDs: []string{"v5"},
		},
		{
			name:        "Search Text - Full",
			query:       QueryData{SearchText: "Hello"},
			expectedIDs: []string{"v3", "v1"},
		},
		{
			name:        "Search Text - Full (Case Insensitive)",
			query:       QueryData{SearchText: "heLLo"},
			expectedIDs: []string{"v3", "v1"},
		},
		{
			name:        "Search Text - Unique Word",
			query:       QueryData{SearchText: "specific"},
			expectedIDs: []string{"v5"},
		},
		{
			name:        "Search Text - No Match",
			query:       QueryData{SearchText: "invalid"},
			expectedIDs: []string{},
		},
		{
			name:        "Whole Word Match disabled",
			query:       QueryData{SearchText: "content", MatchWholeWord: false},
			expectedIDs: []string{"v2", "v4"},
		},
		{
			name:        "Whole Word Match enabled",
			query:       QueryData{SearchText: "content", MatchWholeWord: true},
			expectedIDs: []string{"v2"},
		},
		{
			name:        "Regex - Case Insensitive",
			query:       QueryData{SearchText: "vod", MatchWholeWord: false},
			expectedIDs: []string{"v2"},
		},
		{
			name:        "Regex - Case Sensitive",
			query:       QueryData{SearchText: "VOD", MatchWholeWord: true},
			expectedIDs: []string{"v2"}, // We still match v2 because it's testing against clean text which has everything lowercased.
		},
		{
			name:        "Regex - No Match Partial",
			query:       QueryData{SearchText: "nonexistent", MatchWholeWord: false},
			expectedIDs: []string{},
		},
		{
			name:        "Regex - Whole Word - No Match (Substr)",
			query:       QueryData{SearchText: "con", MatchWholeWord: true},
			expectedIDs: []string{}, // "content" exists, but "con" (whole word) does not
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := app.queryTranscripts(ctx, tt.query)
			if err != nil {
				t.Fatalf("queryTranscripts failed: %v", err)
			}

			// Extract IDs
			var gotIDs []string
			for _, r := range res.Result {
				gotIDs = append(gotIDs, r.ID)
			}

			sort.Strings(gotIDs)
			wantIDs := make([]string, len(tt.expectedIDs))
			copy(wantIDs, tt.expectedIDs)
			sort.Strings(wantIDs)

			// Detailed check:
			if len(gotIDs) != len(wantIDs) {
				t.Errorf("Got IDs %v, want %v", gotIDs, wantIDs)
			} else {
				// Verify contents
				for i, id := range gotIDs {
					if id != wantIDs[i] {
						t.Errorf("Got ID %s at index %d, want %s", id, i, wantIDs[i])
					}
				}
			}
		})
	}
}

func TestDatabase_GraphQueries(t *testing.T) {
	app := setupTestApp(t)
	seedDBForQueryTests(t, app)
	defer app.db.Close()
	ctx := context.Background()

	// 1. querySingleGraph
	// v1 has "Hello" at 00:00:01.
	t.Run("SingleGraph", func(t *testing.T) {
		start := "00:00:01"
		q := QueryData{SearchText: "Hello"}
		res, err := app.querySingleGraph(ctx, "v1", q)
		if err != nil {
			t.Fatalf("querySingleGraph failed: %v", err)
		}
		if len(res.Result) != 1 {
			t.Fatalf("Expected 1 point, got %d", len(res.Result))
		}
		if res.Result[0].X != start || res.Result[0].Y != 1 {
			t.Errorf("Point mismatch: %v", res.Result[0])
		}
	})

	// 2. queryAllGraphs
	// "Hello" is in v1 (Jan 1) and v3 (Jan 15).
	t.Run("AllGraphs", func(t *testing.T) {
		q := QueryData{SearchText: "Hello"}
		res, err := app.queryAllGraphs(ctx, q)
		if err != nil {
			t.Fatalf("queryAllGraphs failed: %v", err)
		}
		// Expect 2 dates: 2023-01-01 (v1) and 2023-01-15 (v3)
		if len(res.Result) != 2 {
			t.Errorf("Expected 2 points (dates), got %d", len(res.Result))
		}

		// Verify points
		// Sort check (they should be sorted by date asc)
		if res.Result[0].X != "2023-01-01" {
			t.Errorf("Expected first point to be 2023-01-01, got %s", res.Result[0].X)
		}
		if res.Result[1].X != "2023-01-15" {
			t.Errorf("Expected second point to be 2023-01-15, got %s", res.Result[1].X)
		}
	})
}

func TestDatabase_RetrieveTranscript(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	// 1. Setup Data
	ts := TranscriptInput{
		ID:            "t1",
		Streamer:      "Streamer1",
		Date:          "2023-10-27",
		StreamTitle:   "Cool Stream",
		StreamType:    "Stream",
		SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nHello\n\n2\n00:00:03,000 --> 00:00:04,000\nWorld\n\n",
	}
	if err := app.insertTranscript(ctx, &ts); err != nil {
		t.Fatalf("Failed to insert transcript: %v", err)
	}

	// 2. Test Success
	got, noRows, err := app.retrieveTranscript(ctx, "t1")
	if err != nil {
		t.Fatalf("Unexpected error retrieving transcript: %v", err)
	}
	if noRows {
		t.Error("Expected transcript to be found, but got noRows=true")
	}
	if got.ID != "t1" {
		t.Errorf("Expected ID 't1', got %s", got.ID)
	}
	if len(got.TranscriptLines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(got.TranscriptLines))
	} else {
		if got.TranscriptLines[0].Text != "Hello" {
			t.Errorf("Expected line 1 text 'Hello', got '%s'", got.TranscriptLines[0].Text)
		}
		if got.TranscriptLines[1].Text != "World" {
			t.Errorf("Expected line 2 text 'World', got '%s'", got.TranscriptLines[1].Text)
		}
	}

	// 3. Test Not Found
	_, noRows, err = app.retrieveTranscript(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent transcript, got nil")
	}
	if !noRows {
		t.Error("Expected noRows=true for non-existent transcript")
	}
}

func TestDatabase_RetrieveTranscript_ContentOrder(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// Insert a transcript with lines out of order in the SRT
	// "00:00:05" appears before "00:00:01"
	srt := "1\n00:00:05,000 --> 00:00:06,000\nLine 2\n\n2\n00:00:01,000 --> 00:00:02,000\nLine 1\n\n"

	input := TranscriptInput{
		ID:            "order_test",
		Streamer:      "Tester",
		Date:          "2023-01-01",
		StreamTitle:   "Order Test",
		StreamType:    "Stream",
		SrtTranscript: srt,
	}

	if err := app.insertTranscript(ctx, &input); err != nil {
		t.Fatalf("Failed to insert transcript: %v", err)
	}

	// Retrieve
	got, _, err := app.retrieveTranscript(ctx, "order_test")
	if err != nil {
		t.Fatalf("Failed to retrieve transcript: %v", err)
	}

	// Verify order
	if len(got.TranscriptLines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(got.TranscriptLines))
	}

	// Expecting "Line 1" (00:00:01) to be first, "Line 2" (00:00:05) to be second
	if got.TranscriptLines[0].Start != "00:00:01" {
		t.Errorf("Expected first line start to be 00:00:01, got %s", got.TranscriptLines[0].Start)
	}
	if got.TranscriptLines[0].Text != "Line 1" {
		t.Errorf("Expected first line text to be 'Line 1', got '%s'", got.TranscriptLines[0].Text)
	}

	if got.TranscriptLines[1].Start != "00:00:05" {
		t.Errorf("Expected second line start to be 00:00:05, got %s", got.TranscriptLines[1].Start)
	}
	if got.TranscriptLines[1].Text != "Line 2" {
		t.Errorf("Expected second line text to be 'Line 2', got '%s'", got.TranscriptLines[1].Text)
	}
}

func TestDatabase_RetrieveTranscript_FullContentVerification(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	input := TranscriptInput{
		ID:            "full_test",
		Streamer:      "FullTester",
		Date:          "2023-12-25",
		StreamTitle:   "Full Content Test",
		StreamType:    "VOD",
		SrtTranscript: "1\n00:00:10,000 --> 00:00:15,000\nContent A\n\n2\n00:00:20,000 --> 00:00:25,000\nContent B\n\n",
	}

	if err := app.insertTranscript(ctx, &input); err != nil {
		t.Fatalf("Failed to insert transcript: %v", err)
	}

	got, _, err := app.retrieveTranscript(ctx, "full_test")
	if err != nil {
		t.Fatalf("Failed to retrieve transcript: %v", err)
	}

	// Verify Metadata
	if got.ID != input.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, input.ID)
	}
	if got.Streamer != input.Streamer {
		t.Errorf("Streamer mismatch: got %s, want %s", got.Streamer, input.Streamer)
	}
	if got.Date != input.Date {
		t.Errorf("Date mismatch: got %s, want %s", got.Date, input.Date)
	}
	if got.StreamTitle != input.StreamTitle {
		t.Errorf("StreamTitle mismatch: got %s, want %s", got.StreamTitle, input.StreamTitle)
	}
	if got.StreamType != input.StreamType {
		t.Errorf("StreamType mismatch: got %s, want %s", got.StreamType, input.StreamType)
	}

	// Verify Lines
	if len(got.TranscriptLines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(got.TranscriptLines))
	}

	line1 := got.TranscriptLines[0]
	if line1.Start != "00:00:10" {
		t.Errorf("Line 1 Start mismatch: got %s, want 00:00:10", line1.Start)
	}
	if line1.Text != "Content A" {
		t.Errorf("Line 1 Text mismatch: got %s, want Content A", line1.Text)
	}
	if line1.ID == "" {
		t.Error("Line 1 ID should not be empty")
	}

	line2 := got.TranscriptLines[1]
	if line2.Start != "00:00:20" {
		t.Errorf("Line 2 Start mismatch: got %s, want 00:00:20", line2.Start)
	}
	if line2.Text != "Content B" {
		t.Errorf("Line 2 Text mismatch: got %s, want Content B", line2.Text)
	}
}

func TestDatabase_RetrieveAllStreams_ContentAndOrder(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// Insert streams with specific dates to test ordering
	// Expected Order: s3 (2023-03-01), s2 (2023-02-01), s1 (2023-01-01)
	inputs := []TranscriptInput{
		{ID: "s1", Streamer: "A", Date: "2023-01-01", StreamTitle: "Title 1", StreamType: "Stream", SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nhi\n\n"},
		{ID: "s2", Streamer: "A", Date: "2023-02-01", StreamTitle: "Title 2", StreamType: "Stream", SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nhi\n\n"},
		{ID: "s3", Streamer: "A", Date: "2023-03-01", StreamTitle: "Title 3", StreamType: "Stream", SrtTranscript: "1\n00:00:01,000 --> 00:00:02,000\nhi\n\n"},
	}

	for _, in := range inputs {
		if err := app.insertTranscript(ctx, &in); err != nil {
			t.Fatalf("Failed to insert %s: %v", in.ID, err)
		}
	}

	got, err := app.retrieveAllStreams(ctx)
	if err != nil {
		t.Fatalf("retrieveAllStreams failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("Expected 3 streams, got %d", len(got))
	}

	// Verify Order (Date DESC)
	expectedOrder := []string{"s3", "s2", "s1"}
	for i, id := range expectedOrder {
		if got[i].ID != id {
			t.Errorf("Index %d: expected ID %s, got %s", i, id, got[i].ID)
		}
	}

	// Verify Content of one item
	if got[0].ID == "s3" {
		if got[0].StreamTitle != "Title 3" {
			t.Errorf("Expected Title 3, got %s", got[0].StreamTitle)
		}
		if got[0].Date != "2023-03-01" {
			t.Errorf("Expected Date 2023-03-01, got %s", got[0].Date)
		}
	}
}

func TestDatabase_QueryTranscripts_ContentAndOrder(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// Setup data for ordering tests
	// t1: 2023-01-01
	// t2: 2023-02-01
	// t3: 2023-03-01
	inputs := []TranscriptInput{
		{ID: "t1", Streamer: "A", Date: "2023-01-01", StreamTitle: "T1", StreamType: "Stream", SrtTranscript: "1\n00:00:10,000 --> 00:00:11,000\nmatch\n\n"},
		{ID: "t2", Streamer: "A", Date: "2023-02-01", StreamTitle: "T2", StreamType: "Stream", SrtTranscript: "1\n00:00:10,000 --> 00:00:11,000\nmatch\n\n"},
		{ID: "t3", Streamer: "A", Date: "2023-03-01", StreamTitle: "T3", StreamType: "Stream", SrtTranscript: "1\n00:00:10,000 --> 00:00:11,000\nmatch\n\n"},
	}
	for _, in := range inputs {
		app.insertTranscript(ctx, &in)
	}

	// 1. Test Metadata Order (Date DESC)
	q := QueryData{Streamer: "A"} // No search text
	res, err := app.queryTranscripts(ctx, q)
	if err != nil {
		t.Fatalf("queryTranscripts failed: %v", err)
	}
	if len(res.Result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(res.Result))
	}
	expectedIDs := []string{"t3", "t2", "t1"}
	for i, id := range expectedIDs {
		if res.Result[i].ID != id {
			t.Errorf("Metadata Order: Index %d expected %s, got %s", i, id, res.Result[i].ID)
		}
	}

	// 2. Test Context Order (StartTime ASC)
	// Insert a transcript with multiple matches at different times
	// "match" at 00:00:20 and 00:00:10. 10 should come before 20.
	multiMatchInput := TranscriptInput{
		ID:            "multi",
		Streamer:      "B",
		Date:          "2023-04-01",
		StreamTitle:   "Multi",
		StreamType:    "Stream",
		SrtTranscript: "1\n00:00:20,000 --> 00:00:21,000\nmatch second\n\n2\n00:00:10,000 --> 00:00:11,000\nmatch first\n\n",
	}
	app.insertTranscript(ctx, &multiMatchInput)

	q2 := QueryData{Streamer: "B", SearchText: "match"}
	res2, err := app.queryTranscripts(ctx, q2)
	if err != nil {
		t.Fatalf("queryTranscripts (search) failed: %v", err)
	}
	if len(res2.Result) != 1 {
		t.Fatalf("Expected 1 result for streamer B, got %d", len(res2.Result))
	}
	contexts := res2.Result[0].Contexts
	if len(contexts) != 2 {
		t.Fatalf("Expected 2 contexts, got %d", len(contexts))
	}

	// Check Order
	if contexts[0].StartTime != "00:00:10" {
		t.Errorf("Context Order: Expected first context at 00:00:10, got %s", contexts[0].StartTime)
	}
	if contexts[1].StartTime != "00:00:20" {
		t.Errorf("Context Order: Expected second context at 00:00:20, got %s", contexts[1].StartTime)
	}
}

func TestDatabase_GraphQueries_ContentAndOrder(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// Setup Data
	// g1: 2023-01-01. Matches at 00:00:05 and 00:00:01
	// g2: 2023-01-02. Matches at 00:00:10
	inputs := []TranscriptInput{
		{
			ID:            "g1",
			Streamer:      "G",
			Date:          "2023-01-01",
			StreamTitle:   "G1",
			StreamType:    "Stream",
			SrtTranscript: "1\n00:00:05,000 --> 00:00:06,000\nword\n\n2\n00:00:01,000 --> 00:00:02,000\nword\n\n",
		},
		{
			ID:            "g2",
			Streamer:      "G",
			Date:          "2023-01-02",
			StreamTitle:   "G2",
			StreamType:    "Stream",
			SrtTranscript: "1\n00:00:10,000 --> 00:00:11,000\nword\n\n",
		},
	}
	for _, in := range inputs {
		app.insertTranscript(ctx, &in)
	}

	q := QueryData{SearchText: "word"}

	// 1. querySingleGraph Order (StartTime ASC)
	singleRes, err := app.querySingleGraph(ctx, "g1", q)
	if err != nil {
		t.Fatalf("querySingleGraph failed: %v", err)
	}
	if len(singleRes.Result) != 2 {
		t.Fatalf("querySingleGraph: expected 2 points, got %d", len(singleRes.Result))
	}
	if singleRes.Result[0].X != "00:00:01" {
		t.Errorf("querySingleGraph: expected first point at 00:00:01, got %s", singleRes.Result[0].X)
	}
	if singleRes.Result[1].X != "00:00:05" {
		t.Errorf("querySingleGraph: expected second point at 00:00:05, got %s", singleRes.Result[1].X)
	}

	// 2. queryAllGraphs Order (Date ASC)
	allRes, err := app.queryAllGraphs(ctx, q)
	if err != nil {
		t.Fatalf("queryAllGraphs failed: %v", err)
	}
	if len(allRes.Result) != 2 {
		t.Fatalf("queryAllGraphs: expected 2 points, got %d", len(allRes.Result))
	}
	if allRes.Result[0].X != "2023-01-01" {
		t.Errorf("queryAllGraphs: expected first point at 2023-01-01, got %s", allRes.Result[0].X)
	}
	if allRes.Result[1].X != "2023-01-02" {
		t.Errorf("queryAllGraphs: expected second point at 2023-01-02, got %s", allRes.Result[1].X)
	}
}
