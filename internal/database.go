package internal

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// initDB creates the necessary tables and FTS5 virtual table
func (a *App) InitDB() error {
	// For initialization, we use context.Background() as it's not
	// tied to a specific request.
	ctx := context.Background()

	// Use a transaction for schema creation to ensure atomicity.
	tx, err := a.db.BeginTx(ctx, nil) // Use BeginTx
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Defer a rollback. If the transaction is successfully committed, this is a no-op.
	defer tx.Rollback()

	schema := `
	CREATE TABLE IF NOT EXISTS transcripts (
		id TEXT PRIMARY KEY,
		streamer TEXT,
		date TEXT,
		title TEXT,
		stream_type TEXT
	);
	
	CREATE TABLE IF NOT EXISTS transcript_lines (
		rowid INTEGER PRIMARY KEY,
		transcript_id TEXT NOT NULL,
		start_time TEXT,
		text TEXT,
		clean_text TEXT,
		FOREIGN KEY(transcript_id) REFERENCES transcripts(id) ON DELETE CASCADE
	);

	-- Create FTS5 virtual table to index the 'clean_text' column
	CREATE VIRTUAL TABLE IF NOT EXISTS transcript_search USING fts5(
		clean_text,
		content='transcript_lines',
		content_rowid='rowid',
		tokenize = 'porter unicode61 remove_diacritics 2'
	);

	-- Triggers to keep FTS table in sync with transcript_lines
	CREATE TRIGGER IF NOT EXISTS tl_ai AFTER INSERT ON transcript_lines BEGIN
		INSERT INTO transcript_search(rowid, clean_text) VALUES (new.rowid, new.clean_text);
	END;
	CREATE TRIGGER IF NOT EXISTS tl_ad AFTER DELETE ON transcript_lines BEGIN
		INSERT INTO transcript_search(transcript_search, rowid, clean_text) VALUES ('delete', old.rowid, old.clean_text);
	END;
	CREATE TRIGGER IF NOT EXISTS tl_au AFTER UPDATE ON transcript_lines BEGIN
		INSERT INTO transcript_search(transcript_search, rowid, clean_text) VALUES ('delete', old.rowid, old.clean_text);
		INSERT INTO transcript_search(rowid, clean_text) VALUES (new.rowid, new.clean_text);
	END;
	`

	_, err = tx.ExecContext(ctx, schema) // Use ExecContext
	if err != nil {
		return fmt.Errorf("failed to create database schema: %w", err)
	}

	return tx.Commit()
}

// insertTranscript now accepts a context
func (a *App) insertTranscript(ctx context.Context, data *TranscriptInput) error {
	// We delete the old one (if it exists) and insert the new one.
	// The ON DELETE CASCADE in the schema handles child `transcript_lines`
	// and the triggers handle the FTS table.

	// The request mentions that inserting with an existing ID should replace the old data.
	// Using a transaction ensures this is an atomic operation.
	tx, err := a.db.BeginTx(ctx, nil) // Use BeginTx
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Delete existing transcript (which cascades to lines and FTS)
	_, err = tx.ExecContext(ctx, "DELETE FROM transcripts WHERE id = ?", data.ID) // Use ExecContext
	if err != nil {
		return fmt.Errorf("failed to delete existing transcript: %w", err)
	}

	// 2. Insert new transcript metadata
	_, err = tx.ExecContext(ctx, "INSERT INTO transcripts (id, streamer, date, title, stream_type) VALUES (?, ?, ?, ?, ?)", // Use ExecContext
		data.ID, data.Streamer, data.Date, data.StreamTitle, data.StreamType)
	if err != nil {
		return fmt.Errorf("failed to insert new transcript metadata: %w", err)
	}

	// Parse the SRT content to get individual lines.
	lines := parseSRTForLines(data.SrtTranscript)
	if len(lines) == 0 {
		// It's valid to have a transcript with no lines, so just commit metadata.
		return tx.Commit()
	}

	// Prepare statement for efficient bulk insertion of transcript lines.
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO transcript_lines (transcript_id, start_time, text, clean_text) VALUES (?, ?, ?, ?)") // Use PrepareContext
	if err != nil {
		return fmt.Errorf("failed to prepare statement for lines: %w", err)
	}
	defer stmt.Close()

	for _, line := range lines {
		cleanText := normalizeText(line.Text)
		_, err := stmt.ExecContext(ctx, data.ID, line.Start, line.Text, cleanText) // Use ExecContext
		if err != nil {
			return fmt.Errorf("failed to insert transcript line: %w", err)
		}
	}

	// Commit the transaction to save all changes.
	return tx.Commit()
}

// retrieveTranscript now accepts a context
func (a *App) retrieveTranscript(ctx context.Context, id string) (TranscriptOutput, bool, error) {
	var output TranscriptOutput

	// Retrieve transcript metadata
	row := a.db.QueryRowContext(ctx, // Use QueryRowContext
		"SELECT id, streamer, date, title, stream_type FROM transcripts WHERE id = ?",
		id,
	)
	err := row.Scan(&output.ID, &output.Streamer, &output.Date, &output.StreamTitle, &output.StreamType)
	if err == sql.ErrNoRows {
		return TranscriptOutput{}, true, fmt.Errorf("transcript with id '%s' not found", id)
	}
	if err != nil {
		return TranscriptOutput{}, false, fmt.Errorf("failed to retrieve transcript metadata: %w", err)
	}

	// Retrieve all lines for the transcript, ordered by time.
	rows, err := a.db.QueryContext(ctx, "SELECT start_time, text FROM transcript_lines WHERE transcript_id = ? ORDER BY start_time", id) // Use QueryContext
	if err != nil {
		return TranscriptOutput{}, false, fmt.Errorf("failed to query transcript lines: %w", err)
	}
	defer rows.Close()

	var lines []TranscriptLine
	lineId := 0
	for rows.Next() {
		var line TranscriptLine
		if err := rows.Scan(&line.Start, &line.Text); err != nil {
			return TranscriptOutput{}, false, fmt.Errorf("failed to scan transcript line: %w", err)
		}
		line.ID = fmt.Sprintf("%d", lineId)
		lineId++
		lines = append(lines, line)
	}

	if err := rows.Err(); err != nil {
		return TranscriptOutput{}, false, fmt.Errorf("error during rows iteration: %w", err)
	}

	output.TranscriptLines = lines
	return output, false, nil
}

// retrieveTranscript now accepts a context
func (a *App) retrieveStreamMetadata(ctx context.Context, id string) (StreamMetadataOutput, bool, error) {
	var output StreamMetadataOutput

	// Retrieve transcript metadata
	row := a.db.QueryRowContext(ctx, // Use QueryRowContext
		"SELECT id, streamer, date, title, stream_type FROM transcripts WHERE id = ?",
		id,
	)
	err := row.Scan(&output.ID, &output.Streamer, &output.Date, &output.StreamTitle, &output.StreamType)
	if err == sql.ErrNoRows {
		return StreamMetadataOutput{}, true, fmt.Errorf("stream with id '%s' not found", id)
	}
	if err != nil {
		return StreamMetadataOutput{}, false, fmt.Errorf("failed to retrieve stream metadata: %w", err)
	}

	return output, false, nil
}

// queryTranscripts already uses context, so no changes were needed here.
func (a *App) queryTranscripts(ctx context.Context, queryData QueryData) (TranscriptSearchOutput, error) {
	// --- Build Metadata Query (unchanged from previous version) ---
	var qParams strings.Builder
	var sqlArgs []any
	buildFilterQuery(&qParams, &sqlArgs, queryData) // Builds WHERE clause for filters

	var query strings.Builder
	query.WriteString("SELECT t.id, t.streamer, t.date, t.title, t.stream_type FROM transcripts t")

	var ftsQuery string
	if queryData.SearchText != "" {
		ftsQuery = buildFTSQuery(queryData.SearchText) // Creates `"clean search text"`
		query.WriteString(`
			JOIN transcript_lines tl ON t.id = tl.transcript_id
			JOIN transcript_search ts ON tl.rowid = ts.rowid
		`)
		qParams.WriteString(" AND ts.clean_text MATCH ?")
		sqlArgs = append(sqlArgs, ftsQuery)
	}

	query.WriteString(qParams.String())
	if queryData.SearchText != "" {
		query.WriteString(" GROUP BY t.id")
	}
	query.WriteString(" ORDER BY t.date DESC")

	rows, err := a.db.QueryContext(ctx, query.String(), sqlArgs...)
	if err != nil {
		return TranscriptSearchOutput{}, fmt.Errorf("failed to query transcripts metadata: %w", err)
	}

	resultsList := make([]*TranscriptSearch, 0) // Initialize as empty slice
	resultsMap := make(map[string]*TranscriptSearch)
	var idArgs []any

	for rows.Next() {
		var res TranscriptSearch
		if err := rows.Scan(&res.ID, &res.Streamer, &res.Date, &res.Title, &res.StreamType); err != nil {
			rows.Close()
			return TranscriptSearchOutput{}, fmt.Errorf("failed to scan metadata row: %w", err)
		}
		res.Contexts = []SearchContext{} // Initialize contexts

		resPtr := &res
		resultsList = append(resultsList, resPtr)
		resultsMap[res.ID] = resPtr
		idArgs = append(idArgs, res.ID)
	}
	rows.Close()

	if len(resultsList) == 0 {
		return TranscriptSearchOutput{Result: resultsList}, nil
	}

	// --- 2. If searchText was provided, run ONE query for all contexts ---
	if queryData.SearchText != "" {
		inQuery := strings.Repeat("?,", len(idArgs)-1) + "?"

		// --- Build the context query dynamically ---
		var contextQuery strings.Builder
		contextQuery.WriteString(`
			WITH RankedContexts AS (
				SELECT
					tl.transcript_id,
					tl.start_time,
					tl.text, -- Only need original text now
					ROW_NUMBER() OVER(
						PARTITION BY tl.transcript_id
						ORDER BY tl.start_time ASC
					) as rn
				FROM transcript_lines tl
				JOIN transcript_search ts ON tl.rowid = ts.rowid
				WHERE ts.clean_text MATCH ?  -- FTS match on clean text
				  AND tl.transcript_id IN (%s) -- Match transcript IDs
		`) // Note: inQuery will be interpolated later

		var wholeWordRegexPattern string
		if queryData.MatchWholeWord {
			wholeWordRegexPattern = `(?i)\b` + regexp.QuoteMeta(queryData.SearchText) + `\b`
			contextQuery.WriteString(" AND regexp(?, tl.text)") // Add regexp check
		}

		contextQuery.WriteString(`
			)
			SELECT transcript_id, start_time, text -- Select only needed columns
			FROM RankedContexts
			WHERE rn <= 20
			ORDER BY transcript_id, start_time;
		`)

		finalContextQuery := fmt.Sprintf(contextQuery.String(), inQuery)

		// --- Build the arguments ---
		contextSqlArgs := make([]any, 0, 2+len(idArgs))
		contextSqlArgs = append(contextSqlArgs, ftsQuery)  // FTS query text
		contextSqlArgs = append(contextSqlArgs, idArgs...) // Transcript IDs
		if queryData.MatchWholeWord {
			contextSqlArgs = append(contextSqlArgs, wholeWordRegexPattern) // Regexp pattern (optional)
		}

		contextRows, err := a.db.QueryContext(ctx, finalContextQuery, contextSqlArgs...)
		if err != nil {
			if strings.Contains(err.Error(), "no such function: regexp") {
				slog.Error("SQLite regexp function not available.", "err", err)
			}
			return TranscriptSearchOutput{}, fmt.Errorf("failed to execute context query: %w", err)
		}
		defer contextRows.Close()

		for contextRows.Next() {
			var transcriptID string
			var context SearchContext // Use SearchContext directly

			// --- Scan only startTime and the original text (Line) ---
			if err := contextRows.Scan(&transcriptID, &context.StartTime, &context.Line); err != nil {
				return TranscriptSearchOutput{}, fmt.Errorf("failed to scan context row: %w", err)
			}

			// Find the corresponding transcript object
			if res, ok := resultsMap[transcriptID]; ok {
				// --- No snippet creation needed ---
				res.Contexts = append(res.Contexts, context)
			}
		}
		// Check for errors during row iteration
		if err := contextRows.Err(); err != nil {
			return TranscriptSearchOutput{}, fmt.Errorf("error iterating context rows: %w", err)
		}
	}

	// If we performed a strict whole word search, remove transcripts
	// that had metadata matches but no actual whole word context matches.
	if queryData.MatchWholeWord && queryData.SearchText != "" {
		filteredResultsList := make([]*TranscriptSearch, 0, len(resultsList)) // Pre-allocate capacity
		for _, res := range resultsList {
			// Keep the transcript only if it has contexts
			if len(res.Contexts) > 0 {
				filteredResultsList = append(filteredResultsList, res)
			}
		}
		resultsList = filteredResultsList // Replace the original list with the filtered one
	}

	return TranscriptSearchOutput{Result: resultsList}, nil
}

// querySingleGraph now accepts a context
func (a *App) querySingleGraph(ctx context.Context, id string, queryData QueryData) (GraphOutput, error) {
	searchRe, err := a.getRegex(queryData.SearchText, queryData.MatchWholeWord)
	if err != nil {
		return GraphOutput{}, fmt.Errorf("failed to compile regex: %w", err)
	}
	ftsQuery := buildFTSQuery(queryData.SearchText)

	query := `
		SELECT tl.start_time, tl.clean_text
		FROM transcript_lines tl
		JOIN transcript_search ts ON tl.rowid = ts.rowid
		WHERE tl.transcript_id = ? AND ts.clean_text MATCH ?
		ORDER BY tl.start_time
	`
	rows, err := a.db.QueryContext(ctx, query, id, ftsQuery) // Use QueryContext
	if err != nil {
		return GraphOutput{}, fmt.Errorf("failed to query graph data: %w", err)
	}
	defer rows.Close()

	timeCounts := make(map[string]int)
	for rows.Next() {
		var startTime, cleanText string
		if err := rows.Scan(&startTime, &cleanText); err != nil {
			return GraphOutput{}, fmt.Errorf("failed to scan graph data row: %w", err)
		}
		matches := searchRe.FindAllStringIndex(cleanText, -1)
		if len(matches) > 0 {
			timeCounts[startTime] += len(matches)
		}
	}

	graphData := make([]GraphDataPoint, 0, len(timeCounts))
	for timestamp, count := range timeCounts {
		graphData = append(graphData, GraphDataPoint{X: timestamp, Y: count})
	}

	sort.Slice(graphData, func(i, j int) bool {
		return graphData[i].X < graphData[j].X
	})

	return GraphOutput{Result: graphData}, nil
}

// queryAllGraphs now accepts a context
func (a *App) queryAllGraphs(ctx context.Context, queryData QueryData) (GraphOutput, error) {
	// --- Get Regex for counting ---
	searchRe, err := a.getRegex(queryData.SearchText, queryData.MatchWholeWord)
	if err != nil {
		return GraphOutput{}, fmt.Errorf("failed to compile regex: %w", err)
	}
	ftsQuery := buildFTSQuery(queryData.SearchText)

	// --- Filter Criteria (same as /transcripts) ---
	var qParams strings.Builder
	var sqlArgs []any
	buildFilterQuery(&qParams, &sqlArgs, queryData)

	// --- Build the main query ---
	var query strings.Builder
	query.WriteString(`
		SELECT t.date, tl.clean_text
		FROM transcripts t
		JOIN transcript_lines tl ON t.id = tl.transcript_id
		JOIN transcript_search ts ON tl.rowid = ts.rowid
	`)

	// Add the search text filter
	qParams.WriteString(" AND ts.clean_text MATCH ?")
	sqlArgs = append(sqlArgs, ftsQuery)

	// Add all other filters (streamer, from, to, etc.)
	query.WriteString(qParams.String())

	// --- Execute query ---
	rows, err := a.db.QueryContext(ctx, query.String(), sqlArgs...) // Use QueryContext
	if err != nil {
		return GraphOutput{}, fmt.Errorf("failed to query graph data: %w", err)
	}
	defer rows.Close()

	// --- Aggregate counts by date ---
	dateCounts := make(map[string]int)
	for rows.Next() {
		var date, cleanText string
		if err := rows.Scan(&date, &cleanText); err != nil {
			return GraphOutput{}, fmt.Errorf("failed to scan graph data row: %w", err)
		}
		// Use the regex (which respects matchWholeWord) to count
		matches := searchRe.FindAllStringIndex(cleanText, -1)
		if len(matches) > 0 {
			dateCounts[date] += len(matches)
		}
	}

	// --- Format and return data ---
	graphData := make([]GraphDataPoint, 0, len(dateCounts))
	for date, count := range dateCounts {
		graphData = append(graphData, GraphDataPoint{X: date, Y: count})
	}

	sort.Slice(graphData, func(i, j int) bool {
		return graphData[i].X < graphData[j].X
	})

	return GraphOutput{Result: graphData}, nil
}
