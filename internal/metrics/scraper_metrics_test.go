package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestScraperMetricsUseOnlyBoundedLabels(t *testing.T) {
	WarwickScrapeRunsTotal.WithLabelValues("session_detail", "changed").Inc()
	WarwickSnapshotWebsocketEventsTotal.WithLabelValues("session_detail").Inc()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "warwick_scrape_") &&
			!strings.HasPrefix(family.GetName(), "warwick_snapshot_") {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotContains(t, []string{
					"target",
					"target_id",
					"resource",
					"resource_key",
					"course",
					"session",
					"student",
					"worker_id",
					"error",
				}, label.GetName())
			}
		}
	}
}
