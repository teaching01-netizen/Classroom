package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

type SchedulerRepository interface {
	ClaimDue(context.Context, db.ClaimRequest) ([]domain.ScrapeTarget, error)
	ClaimOne(context.Context, db.ClaimOneRequest) (domain.ScrapeTarget, error)
	ReleaseLease(context.Context, db.ReleaseLeaseRequest) error
	RenewLease(context.Context, db.RenewLeaseRequest) error
	RescheduleLease(context.Context, int64, int64, time.Time) error
	SetDueNow(context.Context, domain.TargetRef, time.Time) error
	Seed(context.Context, []domain.TargetSeed) error
	Target(context.Context, domain.TargetRef) (domain.ScrapeTarget, error)
	CountDue(context.Context, time.Time) (int, error)
	Prune(context.Context, db.PruneRequest) (db.PruneResult, error)
	FindStaleTargets(context.Context, time.Time) ([]db.StaleTarget, error)
	RepairStaleTargets(context.Context, []db.StaleTarget, time.Time) (int64, error)
}

type PermitController interface {
	Acquire(context.Context, domain.ScrapeTarget, string, time.Time) (domain.PermitDecision, error)
	Release(context.Context, *domain.HostPermit) error
}

type CoordinatorRunner interface {
	RunClaimedWithRelease(
		context.Context,
		domain.ScrapeTarget,
		func(),
	) (RunResult, error)
}

type SchedulerConfig struct {
	WorkerID            string
	MaxConcurrency      int
	PrefetchFactor      int
	LeaseDuration       time.Duration
	CommitGrace         time.Duration
	TickLimit           int
	PollInterval        time.Duration
	RefreshPollInterval time.Duration
	RefreshPollMax      time.Duration
	SnapshotRetention   time.Duration
	RunRetention        time.Duration
	PruneBatchSize      int
	WatchdogInterval    time.Duration
	Clock               func() time.Time
	Random              *rand.Rand
	Wait                func(context.Context, time.Duration) error
}

type Scheduler struct {
	repository          SchedulerRepository
	permits             PermitController
	runner              CoordinatorRunner
	workerID            string
	maxConcurrency      int
	prefetchFactor      int
	leaseDuration       time.Duration
	commitGrace         time.Duration
	tickLimit           int
	pollInterval        time.Duration
	refreshPollInterval time.Duration
	refreshPollMax      time.Duration
	snapshotRetention   time.Duration
	runRetention        time.Duration
	pruneBatchSize      int
	watchdogInterval    time.Duration
	clock               func() time.Time
	wait                func(context.Context, time.Duration) error
	randomMu            sync.Mutex
	random              *rand.Rand
	maintenanceMu       sync.Mutex
	lastMaintenanceDay  string
}

func NewScheduler(
	repository SchedulerRepository,
	permits PermitController,
	runner CoordinatorRunner,
	config SchedulerConfig,
) *Scheduler {
	if repository == nil || permits == nil || runner == nil {
		panic("Scheduler: repository, permits, and runner must not be nil")
	}
	if config.WorkerID == "" {
		panic("Scheduler: worker ID must not be empty")
	}
	if config.MaxConcurrency <= 0 {
		panic("Scheduler: max concurrency must be positive")
	}
	if config.PrefetchFactor <= 0 {
		panic("Scheduler: prefetch factor must be positive")
	}
	if config.LeaseDuration <= 0 || config.CommitGrace < 0 {
		panic("Scheduler: lease duration must be positive")
	}
	if config.TickLimit <= 0 {
		panic("Scheduler: tick limit must be positive")
	}
	if config.PollInterval <= 0 || config.RefreshPollInterval <= 0 {
		panic("Scheduler: poll intervals must be positive")
	}
	if config.RefreshPollMax == 0 {
		config.RefreshPollMax = 500 * time.Millisecond
	}
	if config.RefreshPollMax < config.RefreshPollInterval {
		panic("Scheduler: refresh poll maximum must not be shorter than its minimum")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if config.Wait == nil {
		config.Wait = waitWithTimer
	}
	if config.PruneBatchSize <= 0 {
		config.PruneBatchSize = 1000
	}
	if config.WatchdogInterval <= 0 {
		config.WatchdogInterval = 5 * time.Minute
	}
	return &Scheduler{
		repository:          repository,
		permits:             permits,
		runner:              runner,
		workerID:            config.WorkerID,
		maxConcurrency:      config.MaxConcurrency,
		prefetchFactor:      config.PrefetchFactor,
		leaseDuration:       config.LeaseDuration,
		commitGrace:         config.CommitGrace,
		tickLimit:           config.TickLimit,
		pollInterval:        config.PollInterval,
		refreshPollInterval: config.RefreshPollInterval,
		refreshPollMax:      config.RefreshPollMax,
		snapshotRetention:   config.SnapshotRetention,
		runRetention:        config.RunRetention,
		pruneBatchSize:      config.PruneBatchSize,
		watchdogInterval:    config.WatchdogInterval,
		clock:               config.Clock,
		wait:                config.Wait,
		random:              config.Random,
	}
}

type TickResult struct {
	Claimed      int `json:"claimed"`
	Attempted    int `json:"attempted"`
	Succeeded    int `json:"succeeded"`
	Changed      int `json:"changed"`
	Failed       int `json:"failed"`
	Canceled     int `json:"canceled"`
	RemainingDue int `json:"remaining_due"`
}

func (s *Scheduler) RunDue(ctx context.Context, limit int) (TickResult, error) {
	if limit <= 0 {
		return TickResult{}, errors.New("scheduler tick limit must be positive")
	}
	if limit > s.tickLimit {
		limit = s.tickLimit
	}
	var result TickResult
	var resultMu sync.Mutex
	var firstErr error

	for result.Claimed < limit && ctx.Err() == nil {
		remaining := limit - result.Claimed
		claimLimit := s.maxConcurrency * s.prefetchFactor
		if claimLimit > remaining {
			claimLimit = remaining
		}
		targets, err := s.repository.ClaimDue(ctx, db.ClaimRequest{
			Now:           s.clock().UTC(),
			Limit:         claimLimit,
			WorkerID:      s.workerID,
			LeaseDuration: s.leaseDuration,
		})
		if err != nil {
			firstErr = errors.Join(firstErr, err)
			break
		}
		if len(targets) == 0 {
			break
		}
		result.Claimed += len(targets)

		// Log claim batch with structured fields
		slog.Info("scrape_claim_batch",
			"worker_id", s.workerID,
			"claimed", len(targets),
			"remaining_limit", remaining,
			"concurrency", s.maxConcurrency,
		)

		slots := make(chan struct{}, s.maxConcurrency)
		var workers sync.WaitGroup
		for _, target := range targets {
			target := target
			workers.Add(1)
			go func() {
				defer workers.Done()
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					_ = s.releaseUnstarted(target)
					resultMu.Lock()
					result.Canceled++
					resultMu.Unlock()
					return
				}
				if ctx.Err() != nil {
					_ = s.releaseUnstarted(target)
					resultMu.Lock()
					result.Canceled++
					resultMu.Unlock()
					return
				}
				attempted, runResult, runErr := s.executeClaimed(ctx, target)
				resultMu.Lock()
				defer resultMu.Unlock()
				if attempted {
					result.Attempted++
				}
				if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
					result.Canceled++
					return
				}
				if runErr != nil {
					result.Failed++
					firstErr = errors.Join(firstErr, runErr)
					return
				}
				if !attempted {
					return
				}
				if runResult.Succeeded {
					result.Succeeded++
					if runResult.Changed {
						result.Changed++
					}
				} else {
					result.Failed++
				}
			}()
		}
		workers.Wait()
		if len(targets) < claimLimit {
			break
		}
	}

	s.runDailyMaintenance(ctx)
	countCtx := ctx
	var cancel context.CancelFunc
	if ctx.Err() != nil {
		countCtx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}
	remainingDue, countErr := s.repository.CountDue(countCtx, s.clock().UTC())
	if countErr == nil {
		result.RemainingDue = remainingDue
	} else {
		firstErr = errors.Join(firstErr, countErr)
	}
	if ctx.Err() != nil {
		firstErr = errors.Join(firstErr, ctx.Err())
	}
	return result, firstErr
}

func (s *Scheduler) executeClaimed(
	ctx context.Context,
	target domain.ScrapeTarget,
) (bool, RunResult, error) {
	for {
		if err := ctx.Err(); err != nil {
			_ = s.releaseUnstarted(target)
			return false, RunResult{}, err
		}
		now := s.clock().UTC()
		decision, err := s.permits.Acquire(ctx, target, s.workerID, now)
		if err != nil {
			_ = s.releaseUnstarted(target)
			return false, RunResult{}, err
		}
		if decision.Permit == nil {
			retryAt := decision.RetryAt
			if retryAt.IsZero() || retryAt.Before(now) {
				retryAt = now.Add(s.refreshPollInterval)
			}
			if target.LeaseExpiresAt != nil &&
				!retryAt.Before(target.LeaseExpiresAt.Add(-s.commitGrace)) {
				err := s.repository.RescheduleLease(
					ctx,
					target.ID,
					target.LeaseGeneration,
					retryAt,
				)
				return false, RunResult{}, err
			}
			if err := s.wait(ctx, retryAt.Sub(now)); err != nil {
				_ = s.releaseUnstarted(target)
				return false, RunResult{}, err
			}
			continue
		}

		var releaseOnce sync.Once
		var releaseErr error
		release := func() {
			releaseOnce.Do(func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				releaseErr = s.permits.Release(releaseCtx, decision.Permit)
			})
		}

		// Start lease heartbeat before the run. The heartbeat renews the lease
		// every leaseDuration/3 (capped at 10s) to prevent expiration during
		// long-running fetches. It exits when the done channel is closed (run
		// completes or context cancelled) or when the lease is lost.
		heartbeatDone := make(chan struct{})
		go s.leaseHeartbeat(ctx, target, heartbeatDone)

		runResult, runErr := s.runner.RunClaimedWithRelease(ctx, target, release)
		close(heartbeatDone)
		release()
		if errors.Is(runErr, domain.ErrLeaseLost) {
			metrics.WarwickScrapeLeaseLostTotal.Inc()
			slog.Warn("scrape_lease_lost",
				"target_id", target.ID,
				"target_kind", string(target.Ref.Kind),
				"worker_id", s.workerID,
				"lease_generation", target.LeaseGeneration,
			)
		}
		if runErr != nil &&
			(errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)) {
			_ = s.releaseUnstarted(target)
		}
		return true, runResult, errors.Join(runErr, releaseErr)
	}
}

func (s *Scheduler) releaseUnstarted(target domain.ScrapeTarget) error {
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.repository.ReleaseLease(releaseCtx, db.ReleaseLeaseRequest{
		TargetID:        target.ID,
		LeaseGeneration: target.LeaseGeneration,
	})
}

func (s *Scheduler) leaseHeartbeat(ctx context.Context, target domain.ScrapeTarget, done <-chan struct{}) {
	interval := s.leaseDuration / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-time.After(interval):
			renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.repository.RenewLease(renewCtx, db.RenewLeaseRequest{
				TargetID:        target.ID,
				WorkerID:        s.workerID,
				LeaseGeneration: target.LeaseGeneration,
				LeaseDuration:   s.leaseDuration,
				Now:             s.clock().UTC(),
			})
			cancel()
			if errors.Is(err, domain.ErrLeaseLost) {
				slog.Warn("scrape_heartbeat_lease_lost",
					"target_id", target.ID,
					"target_kind", string(target.Ref.Kind),
					"worker_id", s.workerID,
					"lease_generation", target.LeaseGeneration,
				)
				return
			}
			if err != nil {
				slog.Warn("scrape_heartbeat_renewal_failed",
					"target_id", target.ID,
					"worker_id", s.workerID,
					"error", err,
				)
				return
			}
		}
	}
}

func (s *Scheduler) SetDueNow(ctx context.Context, ref domain.TargetRef) error {
	return s.repository.SetDueNow(ctx, ref, s.clock().UTC())
}

func (s *Scheduler) RefreshNow(ctx context.Context, ref domain.TargetRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	baseline, err := s.repository.Target(ctx, ref)
	if errors.Is(err, domain.ErrSnapshotNotFound) {
		if seedErr := s.repository.Seed(ctx, []domain.TargetSeed{seedForRef(ref, s.clock().UTC())}); seedErr != nil {
			return seedErr
		}
		baseline, err = s.repository.Target(ctx, ref)
	}
	if err != nil {
		return err
	}
	if err := s.SetDueNow(ctx, ref); err != nil {
		return err
	}
	target, err := s.repository.ClaimOne(ctx, db.ClaimOneRequest{
		Ref:           ref,
		Now:           s.clock().UTC(),
		WorkerID:      s.workerID,
		LeaseDuration: s.leaseDuration,
	})
	if err == nil {
		attempted, result, runErr := s.executeClaimed(ctx, target)
		if runErr != nil {
			return runErr
		}
		if !attempted || !result.Succeeded {
			return fmt.Errorf("snapshot refresh %s finished with outcome %s", ref.IdentityKey(), result.Outcome)
		}
		return nil
	}
	if !errors.Is(err, domain.ErrTargetLeased) {
		return err
	}
	metrics.WarwickScrapeClaimConflictsTotal.Inc()

	for {
		if err := s.wait(ctx, s.refreshPollDelay()); err != nil {
			return err
		}
		current, readErr := s.repository.Target(ctx, ref)
		if readErr != nil {
			return readErr
		}
		if current.ValidationSeq > baseline.ValidationSeq {
			return nil
		}
		if current.ConsecutiveFailures > baseline.ConsecutiveFailures {
			return fmt.Errorf(
				"%w: coalesced attempt for %s ended without validation",
				domain.ErrSnapshotRefreshFailed,
				ref.IdentityKey(),
			)
		}
		now := s.clock().UTC()
		if current.LeaseExpiresAt != nil && current.LeaseExpiresAt.After(now) {
			continue
		}
		target, claimErr := s.repository.ClaimOne(ctx, db.ClaimOneRequest{
			Ref:           ref,
			Now:           now,
			WorkerID:      s.workerID,
			LeaseDuration: s.leaseDuration,
		})
		if errors.Is(claimErr, domain.ErrTargetLeased) {
			continue
		}
		if claimErr != nil {
			return claimErr
		}
		attempted, result, runErr := s.executeClaimed(ctx, target)
		if runErr != nil {
			return runErr
		}
		if !attempted || !result.Succeeded {
			return fmt.Errorf(
				"snapshot refresh %s finished with outcome %s",
				ref.IdentityKey(),
				result.Outcome,
			)
		}
		return nil
	}
}

func (s *Scheduler) refreshPollDelay() time.Duration {
	if s.refreshPollMax == s.refreshPollInterval {
		return s.refreshPollInterval
	}
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	span := s.refreshPollMax - s.refreshPollInterval
	return s.refreshPollInterval + time.Duration(s.random.Int63n(int64(span)+1))
}

func seedForRef(ref domain.TargetRef, now time.Time) domain.TargetSeed {
	status := domain.SessionStatusActive
	policy := PolicyFor(ref.Kind, status)
	return domain.TargetSeed{
		Ref:             ref,
		InitialInterval: policy.Initial,
		MinInterval:     policy.Min,
		MaxInterval:     policy.Max,
		MaxServeAge:     policy.MaxServeAge,
		NextRunAt:       now,
	}
}

func (s *Scheduler) runDailyMaintenance(ctx context.Context) {
	if s.snapshotRetention <= 0 || s.runRetention <= 0 {
		return
	}
	now := s.clock().UTC()
	day := now.Format("2006-01-02")
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.lastMaintenanceDay == day {
		return
	}
	if _, err := s.repository.Prune(ctx, db.PruneRequest{
		Now:               now,
		SnapshotRetention: s.snapshotRetention,
		RunRetention:      s.runRetention,
		BatchSize:         s.pruneBatchSize,
	}); err != nil && ctx.Err() == nil {
		slog.Warn("snapshot_maintenance_failed", "error", err)
		return
	}
	if ctx.Err() == nil {
		s.lastMaintenanceDay = day
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	watchdogTicker := time.NewTicker(s.watchdogInterval)
	defer watchdogTicker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		select {
		case <-watchdogTicker.C:
			s.RunWatchdog(ctx)
		default:
		}

		if _, err := s.RunDue(ctx, s.tickLimit); err != nil && ctx.Err() == nil {
			slog.Warn("snapshot_scheduler_tick_failed", "error", err)
		}
		if err := s.wait(ctx, s.pollInterval); err != nil {
			return
		}
	}
}

// RunWatchdog queries for stale targets and repairs impossible scheduling
// state: next_run_at too far in the future, expired leases still held, etc.
// It emits structured repair events via slog for operational visibility.
func (s *Scheduler) RunWatchdog(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := s.clock().UTC()

	staleTargets, err := s.repository.FindStaleTargets(ctx, now)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("watchdog_query_failed", "error", err)
		}
		return
	}
	if len(staleTargets) == 0 {
		return
	}

	repaired, err := s.repository.RepairStaleTargets(ctx, staleTargets, now)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("watchdog_repair_failed", "error", err, "candidates", len(staleTargets))
		}
		return
	}

	slog.Info("watchdog_repair_complete",
		"candidates", len(staleTargets),
		"repaired", repaired,
		"now", now,
	)

	for _, t := range staleTargets {
		reason := "unknown"
		var maxIntervalSeconds int64
		if t.CurrentIntervalSeconds > 0 {
			maxIntervalSeconds = t.CurrentIntervalSeconds * 3
		}
		if t.NextRunAt.After(now.Add(time.Duration(maxIntervalSeconds) * time.Second)) {
			reason = "next_run_at_impossibly_far"
		} else if t.LeaseExpiresAt != nil && t.LeaseExpiresAt.Before(now.Add(-time.Hour)) {
			reason = "lease_expired_orphaned"
		} else if t.LifecycleState != "active" {
			reason = "lifecycle_mismatch"
		}
		slog.Info("watchdog_repair_event",
			"target_id", t.ID,
			"host", t.Host,
			"kind", t.Kind,
			"resource_key", t.ResourceKey,
			"reason", reason,
			"old_next_run_at", t.NextRunAt,
			"old_lease_owner", t.LeaseOwner,
		)
	}
}

func waitWithTimer(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
