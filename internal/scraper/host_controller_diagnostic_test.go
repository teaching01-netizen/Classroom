package scraper

import (
	"context"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHostPermitContentionDiagnostics records transaction latency under four
// process-like clients. It is opt-in because the result is environment
// dependent and is intended for release/staging diagnostics, not a CI gate.
func TestHostPermitContentionDiagnostics(t *testing.T) {
	if os.Getenv("RUN_SNAPSHOT_DIAGNOSTICS") != "1" {
		t.Skip("set RUN_SNAPSHOT_DIAGNOSTICS=1 with TEST_DATABASE_URL")
	}
	controller, repo, ctx, now, _ := newHostControllerTest(t, 5, 5, 4)
	const (
		instanceCount  = 4
		opsPerInstance = 50
	)
	targets := claimPermitTargets(
		t,
		ctx,
		repo,
		now,
		instanceCount*opsPerInstance,
	)

	var sequence atomic.Int64
	var admitted atomic.Int64
	latencies := make(chan time.Duration, len(targets))
	errorsSeen := make(chan error, len(targets))
	var clients sync.WaitGroup
	for instance := range instanceCount {
		clients.Add(1)
		go func(instance int) {
			defer clients.Done()
			for operation := range opsPerInstance {
				target := targets[instance*opsPerInstance+operation]
				observedAt := now.Add(
					time.Duration(sequence.Add(1)) * time.Second,
				)
				started := time.Now()
				decision, err := controller.Acquire(
					context.Background(),
					target,
					"diagnostic-worker",
					observedAt,
				)
				latencies <- time.Since(started)
				if err != nil {
					errorsSeen <- err
					continue
				}
				if decision.Permit != nil {
					admitted.Add(1)
					if releaseErr := controller.Release(
						context.Background(),
						decision.Permit,
					); releaseErr != nil {
						errorsSeen <- releaseErr
					}
				}
			}
		}(instance)
	}
	clients.Wait()
	close(latencies)
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}

	recorded := make([]time.Duration, 0, len(targets))
	for latency := range latencies {
		recorded = append(recorded, latency)
	}
	require.Len(t, recorded, len(targets))
	sort.Slice(recorded, func(i, j int) bool { return recorded[i] < recorded[j] })
	t.Logf(
		"host permit transaction latency: p50=%s p95=%s admitted=%d/%d instances=%d",
		diagnosticDurationPercentile(recorded, 0.50),
		diagnosticDurationPercentile(recorded, 0.95),
		admitted.Load(),
		len(targets),
		instanceCount,
	)
}

func diagnosticDurationPercentile(
	values []time.Duration,
	fraction float64,
) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * fraction)
	return values[index]
}
