package internal

import (
	"bufio"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// parseSRTForLines parses raw SRT content into a slice of TranscriptLine.
// It's designed to be fast and handle common SRT format variations.
func parseSRTForLines(srtContent string) []TranscriptLine {
	// Normalize line endings and trim whitespace
	srtContent = strings.ReplaceAll(srtContent, "\r\n", "\n")
	srtContent = strings.TrimSpace(srtContent)

	blocks := strings.Split(srtContent, "\n\n")
	// Pre-allocate with a reasonable capacity
	lines := make([]TranscriptLine, 0, len(blocks))

	for _, block := range blocks {
		parts := strings.SplitN(block, "\n", 3)
		if len(parts) < 3 {
			continue // Invalid block
		}

		// parts[0] is the index (e.g., "1")
		// parts[1] is the timestamp (e.g., "00:00:01,000 --> 00:00:04,000")
		// parts[2] is the text content

		// Get hh:mm:ss from "hh:mm:ss,ms --> ..."
		if len(parts[1]) < 8 {
			continue // Invalid timestamp line
		}
		startTime := parts[1][0:8]

		// Clean up text: remove newlines within a single block
		text := strings.ReplaceAll(parts[2], "\n", " ")
		text = strings.TrimSpace(text)

		if text != "" {
			lines = append(lines, TranscriptLine{
				Start: startTime,
				Text:  text,
			})
		}
	}
	return lines
}

// buildFilterQuery dynamically builds the WHERE clause and arg list for filters.
func buildFilterQuery(qParams *strings.Builder, sqlArgs *[]any, q url.Values) {
	qParams.WriteString(" WHERE 1=1")

	if streamer := q.Get("streamer"); streamer != "" {
		qParams.WriteString(" AND t.streamer = ?")
		*sqlArgs = append(*sqlArgs, streamer)
	}
	if title := q.Get("streamTitle"); title != "" {
		qParams.WriteString(" AND t.title LIKE ?")
		*sqlArgs = append(*sqlArgs, "%"+title+"%")
	}
	if fromDate := q.Get("fromDate"); fromDate != "" {
		qParams.WriteString(" AND t.date >= ?")
		*sqlArgs = append(*sqlArgs, fromDate)
	}
	if toDate := q.Get("toDate"); toDate != "" {
		qParams.WriteString(" AND t.date <= ?")
		*sqlArgs = append(*sqlArgs, toDate)
	}
	if streamTypes := q["streamType"]; len(streamTypes) > 0 {
		var placeholders strings.Builder
		for i, st := range streamTypes {
			if i > 0 {
				placeholders.WriteString(", ")
			}
			placeholders.WriteString("?")
			*sqlArgs = append(*sqlArgs, st) // Append to the pointer
		}
		fmt.Fprintf(qParams, " AND t.stream_type IN (%s)", placeholders.String())
	}
}

// getRegex memoizes compiled regexes for performance.
func (a *App) getRegex(searchText string, matchWholeWord bool) (*regexp.Regexp, error) {
	key := fmt.Sprintf("%t:%s", matchWholeWord, searchText)

	a.regexCacheMu.Lock()
	re, ok := a.regexCache[key]
	a.regexCacheMu.Unlock()

	if ok {
		return re, nil
	}

	cleanSearchText := normalizeText(searchText)
	reStr := regexp.QuoteMeta(cleanSearchText)
	if matchWholeWord {
		reStr = `\b` + reStr + `\b`
	}

	newRe, err := regexp.Compile(reStr)
	if err != nil {
		return nil, err
	}

	a.regexCacheMu.Lock()
	a.regexCache[key] = newRe
	a.regexCacheMu.Unlock()

	return newRe, nil
}

// buildFTSQuery formats a search string for FTS5.
func buildFTSQuery(searchText string) string {
	cleanText := normalizeText(searchText)
	return `"` + cleanText + `"`
}

// normalizeText cleans text for searching.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsPunct(r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// parseSRT converts raw SRT text into structured TranscriptLines.
func parseSRT(srtContent string) []TranscriptLine {
	var lines []TranscriptLine
	scanner := bufio.NewScanner(strings.NewReader(srtContent))

	var currentLine TranscriptLine
	var textBuilder strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, " --> ") {
			if textBuilder.Len() > 0 {
				currentLine.Text = strings.TrimSpace(textBuilder.String())
				lines = append(lines, currentLine)
			}
			textBuilder.Reset()
			startTime := strings.Split(line, " --> ")[0]
			if len(startTime) >= 8 {
				startTime = startTime[:8]
			}
			currentLine = TranscriptLine{Start: startTime}
			continue
		}

		if line == "" {
			continue
		}
		if _, err := fmt.Sscan(line, new(int)); err == nil {
			if textBuilder.Len() == 0 {
				continue
			}
		}

		if textBuilder.Len() > 0 {
			textBuilder.WriteString(" ")
		}
		textBuilder.WriteString(line)
	}

	if textBuilder.Len() > 0 {
		currentLine.Text = strings.TrimSpace(textBuilder.String())
		lines = append(lines, currentLine)
	}

	return lines
}

// createSnippet finds the search text in the clean text, maps its word position
// to the original text, and extracts a snippet with a word buffer.
func createSnippet(originalText, cleanText, searchText string, wordBuffer int) string {
	originalWords := strings.Fields(originalText)
	cleanWords := strings.Fields(cleanText)
	cleanSearch := normalizeText(searchText)
	cleanSearchWords := strings.Fields(cleanSearch)

	// Basic safety checks
	if len(cleanSearchWords) == 0 || len(originalWords) == 0 {
		return originalText
	}

	// --- Find Match Index in Clean Text ---
	cleanMatchWordIndex := -1 // Starting index of the phrase in cleanWords
	for i := 0; i <= len(cleanWords)-len(cleanSearchWords); i++ {
		match := true
		for j := 0; j < len(cleanSearchWords); j++ {
			if i+j >= len(cleanWords) || cleanWords[i+j] != cleanSearchWords[j] {
				match = false
				break
			}
		}
		if match {
			cleanMatchWordIndex = i
			break
		}
	}

	// If no match found (defensive check)
	if cleanMatchWordIndex == -1 {
		limit := wordBuffer * 4 // Fallback: return truncated original text
		if len(originalWords) > limit {
			return strings.Join(originalWords[:limit], " ") + "___"
		}
		return originalText
	}

	// --- Estimate Index and Length in Original Text ---
	// Use the clean index as the best guess for the start in originalWords
	matchStartIndexOriginal := cleanMatchWordIndex
	// Use the length of the *clean search phrase* as the best guess for match length
	matchLengthOriginal := len(cleanSearchWords)

	// --- Calculate Ideal Boundaries (Indices for originalWords) ---
	// Ideal start index: 'wordBuffer' words before the match starts
	idealStart := matchStartIndexOriginal - wordBuffer
	// Ideal end index: 'wordBuffer' words after the match ends
	idealEnd := matchStartIndexOriginal + matchLengthOriginal + wordBuffer // end is the exclusive index

	// --- Clamp Boundaries to Valid Slice Indices ---
	// startClamped: must be >= 0
	startClamped := max(0, idealStart)
	// endClamped: must be <= len(originalWords)
	endClamped := min(len(originalWords), idealEnd)

	// --- Final Sanity Check for Slice Validity ---
	// Ensure startClamped is strictly less than endClamped
	if startClamped >= endClamped {
		// Attempt to fallback to just the estimated match words
		startClamped = max(0, matchStartIndexOriginal)
		endClamped = min(len(originalWords), matchStartIndexOriginal+matchLengthOriginal)

		// If even this is invalid (e.g., zero length match somehow?), return original
		if startClamped >= endClamped {
			return originalText
		}
	}

	// --- Slice and Join ---
	snippetWords := originalWords[startClamped:endClamped]
	snippet := strings.Join(snippetWords, " ")

	// --- Add Ellipsis ---
	prefix := ""
	suffix := ""
	// Add prefix ellipsis if the actual start is later than the ideal start (meaning we trimmed the beginning)
	if startClamped > 0 { // More precise: check if idealStart was < 0 and startClamped became 0
		prefix = "___ "
	}
	// Add suffix ellipsis if the actual end is earlier than the ideal end (meaning we trimmed the end)
	if endClamped < len(originalWords) { // More precise: check if idealEnd was > len(originalWords) and endClamped got capped
		suffix = " ___"
	}

	return prefix + snippet + suffix
}
