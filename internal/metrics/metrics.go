package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ReportComputeDuration tracks how long report computation takes.
	ReportComputeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "report_compute_duration_seconds",
			Help:    "Duration of attendance report computation in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"source"},
	)
)
