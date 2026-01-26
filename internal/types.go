package internal

import (
	"database/sql"
	"regexp"
	"sync"
)

type Config struct {
	APIKey     string         `yaml:"api_key"`
	Membership []string       `yaml:"membership"`
	KeyTTLDays int            `yaml:"key_ttl_days"`
	Database   DatabaseConfig `yaml:"database"`
}

type DatabaseConfig struct {
	JournalMode   string `yaml:"journal_mode"`
	BusyTimeoutMS int    `yaml:"busy_timeout_ms"`
	Synchronous   string `yaml:"synchronous"`
	CacheSizeKB   int    `yaml:"cache_size_kb"`
	TempStore     string `yaml:"temp_store"`
	MmapSizeBytes int64  `yaml:"mmap_size_bytes"`
}

// App holds application-wide dependencies
type App struct {
	db     *sql.DB
	config Config
	// We still need a regex cache for counting matches *within* a line
	regexCache   map[string]*regexp.Regexp
	regexCacheMu sync.Mutex
}

func NewApp(db *sql.DB, config Config) *App {
	return &App{
		db:           db,
		config:       config,
		regexCache:   make(map[string]*regexp.Regexp),
		regexCacheMu: sync.Mutex{},
	}
}

// TranscriptInput is the structure for the POST /transcript request.
type TranscriptInput struct {
	Streamer      string `json:"streamer"`
	Date          string `json:"date"` // YYYYMMDD
	StreamType    string `json:"streamType"`
	StreamTitle   string `json:"streamTitle"`
	ID            string `json:"id"`
	SrtTranscript string `json:"srt"`
}

// TranscriptOutput is the structure for the GET /transcript/:id response.
type TranscriptOutput struct {
	Streamer        string           `json:"streamer"`
	Date            string           `json:"date"` // YYYY-MM-DD
	StreamType      string           `json:"streamType"`
	StreamTitle     string           `json:"streamTitle"`
	ID              string           `json:"id"`
	TranscriptLines []TranscriptLine `json:"transcriptLines"`
}

// TranscriptSearchOutput is the response for the GET /transcripts search.
type TranscriptSearchOutput struct {
	Result []*TranscriptSearch `json:"result"`
}

// GraphOutput is the response for the GET /graph and GET /graph/:id response.
type GraphOutput struct {
	Result []GraphDataPoint `json:"result"`
}

type StreamMetadataOutput struct {
	Streamer    string `json:"streamer"`
	Date        string `json:"date"` // YYYY-MM-DD
	StreamType  string `json:"streamType"`
	StreamTitle string `json:"streamTitle"`
	ID          string `json:"id"`
}

// TranscriptSearch is the data for a single transcript that matches the criteria
type TranscriptSearch struct {
	ID         string          `json:"id"`
	Streamer   string          `json:"streamer"`
	Date       string          `json:"date"` // YYYY-MM-DD
	StreamType string          `json:"streamType"`
	Title      string          `json:"title"`
	Contexts   []SearchContext `json:"contexts"`
}

// SearchContext is returned in the /transcripts search results.
type SearchContext struct {
	StartTime string `json:"startTime"`
	Line      string `json:"line"`
}

// TranscriptLine is the structure for a single line of a transcript.
type TranscriptLine struct {
	ID    string `json:"id"`
	Start string `json:"start"` // hh:mm:ss
	Text  string `json:"text"`
}

// GraphDataPoint is a generic struct for all graph data.
type GraphDataPoint struct {
	X string `json:"x"` // Can be "hh:mm:ss" or "YYYY-MM-DD"
	Y int    `json:"y"`
}

type KeyResponse struct {
	Key       string `json:"key"`
	ExpiresAt string `json:"expiresAt"`
}

type QueryData struct {
	SearchText        string
	MatchWholeWord    bool
	Streamer          string
	StreamTitle       string
	FromDate          string
	ToDate            string
	StreamTypes       []string
	AuthorizedChannel string
}

type ContextKey string

const AuthorizedChannelKey ContextKey = "authorizedChannel"
