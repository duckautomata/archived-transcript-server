package internal

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Server Based
	TotalRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_total_requests",
		Help: "The total number of requests.",
	})
	RequestsProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_requests_processing_duration_seconds",
		Help: "The duration of requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})
	Http400Errors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_400_errors",
		Help: "The total number of HTTP 4xx client errors.",
	})
	Http500Errors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_500_errors",
		Help: "The total number of HTTP 5xx server errors.",
	})

	PostTranscriptRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_post_transcript_requests",
		Help: "The number of POST /transcript requests.",
	})
	GetTranscriptRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_transcript_requests",
		Help: "The number of GET /transcript/:id requests.",
	})
	SearchTranscriptsRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_search_transcripts_requests",
		Help: "The number of GET /transcripts requests.",
	})
	GetGraphRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_graph_requests",
		Help: "The number of GET /graph/:id requests.",
	})
	GetAllGraphRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_all_graph_requests",
		Help: "The number of GET /graph requests.",
	})
	GetStreamMetadataRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_stream_metadata_requests",
		Help: "The number of GET /stream/:id requests.",
	})

	MemoryUsage = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "at_memory_usage_bytes",
		Help: "The current memory usage.",
	},
		func() float64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return float64(m.Alloc)
		},
	)
)
