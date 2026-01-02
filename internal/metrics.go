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
	PostTranscriptProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_post_transcript_processing_duration_seconds",
		Help: "The duration of POST /transcript requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	GetTranscriptRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_transcript_requests",
		Help: "The number of GET /transcript/:id requests.",
	})
	GetTranscriptProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_get_transcript_processing_duration_seconds",
		Help: "The duration of GET /transcript/:id requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	SearchTranscriptsRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_search_transcripts_requests",
		Help: "The number of GET /transcripts requests.",
	})
	SearchTranscriptsProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_search_transcripts_processing_duration_seconds",
		Help: "The duration of GET /transcripts requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	GetGraphRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_graph_requests",
		Help: "The number of GET /graph/:id requests.",
	})
	GetGraphProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_get_graph_processing_duration_seconds",
		Help: "The duration of GET /graph/:id requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	GetAllGraphRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_all_graph_requests",
		Help: "The number of GET /graph requests.",
	})
	GetAllGraphProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_get_all_graph_processing_duration_seconds",
		Help: "The duration of GET /graph requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	GetStreamMetadataRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_get_stream_metadata_requests",
		Help: "The number of GET /stream/:id requests.",
	})
	GetStreamMetadataProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_get_stream_metadata_processing_duration_seconds",
		Help: "The duration of GET /stream/:id requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	// Membership Stuff
	VerifyMembershipRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "at_verify_membership_requests",
		Help: "The number of GET /membership/verify requests.",
	})
	VerifyMembershipProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "at_verify_membership_processing_duration_seconds",
		Help: "The duration of GET /membership/verify requests in seconds.",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.05, 0.1, 0.5, // ms
			1, 2, 3, 4, 5, // seconds
			10, 15, 20, // seconds
		},
	})

	// Other
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
