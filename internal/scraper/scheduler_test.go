package scraper

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

type schedulerRepository struct {
	mu              sync.Mutex
	targets         []domain.ScrapeTarget
	claimLimits     []int
	releases        []db.ReleaseLeaseRequest
	reschedules     []time.Time
	remainingDue    int
	countDueCalls   int
	targetReads     []domain.ScrapeTarget
	claimOneTarget  domain.ScrapeTarget
	claimOneErr     error
	claimOneTargets []domain.ScrapeTarget
	claimOneErrors  []error
	claimOneCalls   int
	seeds           [][]domain.TargetSeed
	prunes          int
	pruneErrors     []error
	staleTargets    []db.StaleTarget
	repairCalled    int64
	repairErr       error
}

func (r *schedulerRepository) ClaimDue(_ context.Context, request db.ClaimRequest) ([]domain.ScrapeTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimLimits = append(r.claimLimits, request.Limit)
	if len(r.targets) == 0 {
		return nil, nil
	}
	count := request.Limit
	if count > len(r.targets) {
		count = len(r.targets)
	}
	result := append([]domain.ScrapeTarget(nil), r.targets[:count]...)
	r.targets = r.targets[count:]
	return result, nil
}

func (r *schedulerRepository) ClaimOne(context.Context, db.ClaimOneRequest) (domain.ScrapeTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimOneCalls++
	if len(r.claimOneTargets) > 0 || len(r.claimOneErrors) > 0 {
		var target domain.ScrapeTarget
		var err error
		if len(r.claimOneTargets) > 0 {
			target = r.claimOneTargets[0]
			r.claimOneTargets = r.claimOneTargets[1:]
		}
		if len(r.claimOneErrors) > 0 {
			err = r.claimOneErrors[0]
			r.claimOneErrors = r.claimOneErrors[1:]
		}
		return target, err
	}
	return r.claimOneTarget, r.claimOneErr
}

func (r *schedulerRepository) ReleaseLease(_ context.Context, request db.ReleaseLeaseRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases = append(r.releases, request)
	return nil
}

func (r *schedulerRepository) RenewLease(_ context.Context, _ db.RenewLeaseRequest) error {
	return nil
}

func (r *schedulerRepository) RescheduleLease(_ context.Context, _ int64, _ int64, next time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reschedules = append(r.reschedules, next)
	return nil
}

func (r *schedulerRepository) SetDueNow(context.Context, domain.TargetRef, time.Time) error {
	return nil
}

func (r *schedulerRepository) Seed(_ context.Context, seeds []domain.TargetSeed) error {
	r.seeds = append(r.seeds, append([]domain.TargetSeed(nil), seeds...))
	return nil
}

func (r *schedulerRepository) Target(context.Context, domain.TargetRef) (domain.ScrapeTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.targetReads) == 0 {
		return domain.ScrapeTarget{}, domain.ErrSnapshotNotFound
	}
	value := r.targetReads[0]
	if len(r.targetReads) > 1 {
		r.targetReads = r.targetReads[1:]
	}
	return value, nil
}

func (r *schedulerRepository) CountDue(context.Context, time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countDueCalls++
	return r.remainingDue, nil
}

func (r *schedulerRepository) Prune(context.Context, db.PruneRequest) (db.PruneResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prunes++
	if len(r.pruneErrors) > 0 {
		err := r.pruneErrors[0]
		r.pruneErrors = r.pruneErrors[1:]
		return db.PruneResult{}, err
	}
	return db.PruneResult{}, nil
}

func (r *schedulerRepository) FindStaleTargets(ctx context.Context, _ time.Time) ([]db.StaleTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]db.StaleTarget(nil), r.staleTargets...), nil
}

func (r *schedulerRepository) RepairStaleTargets(ctx context.Context, _ []db.StaleTarget, _ time.Time) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.repairErr != nil {
		return 0, r.repairErr
	}
	r.repairCalled++
	return r.repairCalled, nil
}

type schedulerPermitController struct {
	mu        sync.Mutex
	decisions []domain.PermitDecision
	released  []int64
}

func (c *schedulerPermitController) Acquire(
	_ context.Context,
	target domain.ScrapeTarget,
	_ string,
	now time.Time,
) (domain.PermitDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.decisions) > 0 {
		decision := c.decisions[0]
		c.decisions = c.decisions[1:]
		return decision, nil
	}
	return domain.PermitDecision{Permit: &domain.HostPermit{
		ID: target.ID, Host: target.Ref.Host, TargetID: target.ID,
		LeaseGeneration: target.LeaseGeneration, ExpiresAt: now.Add(time.Minute),
	}}, nil
}

func (c *schedulerPermitController) Release(_ context.Context, permit *domain.HostPermit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.released = append(c.released, permit.ID)
	return nil
}

type schedulerRunner struct {
	active    atomic.Int32
	maxActive atomic.Int32
	attempts  atomic.Int32
	block     <-chan struct{}
	started   chan<- int64
	results   map[int64]RunResult
}

func (r *schedulerRunner) RunClaimedWithRelease(
	ctx context.Context,
	target domain.ScrapeTarget,
	release func(),
) (RunResult, error) {
	r.attempts.Add(1)
	active := r.active.Add(1)
	for {
		maximum := r.maxActive.Load()
		if active <= maximum || r.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	defer r.active.Add(-1)
	if r.started != nil {
		select {
		case r.started <- target.ID:
		default:
		}
	}
	release()
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		}
	}
	if result, ok := r.results[target.ID]; ok {
		return result, nil
	}
	return RunResult{
		TargetID: target.ID, LeaseGeneration: target.LeaseGeneration,
		Outcome: "unchanged", Succeeded: true,
	}, nil
}

func schedulerTargets(count int, now time.Time) []domain.ScrapeTarget {
	targets := make([]domain.ScrapeTarget, count)
	for index := range count {
		targets[index] = domain.ScrapeTarget{
			ID: int64(index + 1),
			Ref: domain.TargetRef{
				Host: "warwick.humantix.cloud", Kind: domain.SnapshotCourseDetail,
				ResourceKey: string(rune('a' + index)),
			},
			LeaseOwner: "worker", LeaseGeneration: 1,
			LeaseExpiresAt: pointerTime(now.Add(2 * time.Minute)),
		}
	}
	return targets
}

func pointerTime(value time.Time) *time.Time { return &value }

func newSchedulerTest(
	repository *schedulerRepository,
	controller *schedulerPermitController,
	runner *schedulerRunner,
	now time.Time,
) *Scheduler {
	return NewScheduler(repository, controller, runner, SchedulerConfig{
		WorkerID:            "worker",
		MaxConcurrency:      2,
		PrefetchFactor:      2,
		LeaseDuration:       2 * time.Minute,
		CommitGrace:         15 * time.Second,
		TickLimit:           50,
		PollInterval:        time.Second,
		RefreshPollInterval: 100 * time.Millisecond,
		RefreshPollMax:      500 * time.Millisecond,
		SnapshotRetention:   30 * 24 * time.Hour,
		RunRetention:        30 * 24 * time.Hour,
		PruneBatchSize:      1000,
		Clock:               func() time.Time { return now },
		Random:              rand.New(rand.NewSource(7)),
		Wait: func(ctx context.Context, _ time.Duration) error {
			return ctx.Err()
		},
	})
}

func TestSchedulerBoundsConcurrencyPrefetchAndTickLimit(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{
		targets: schedulerTargets(9, now), remainingDue: 7,
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	result, err := scheduler.RunDue(context.Background(), 5)
	require.NoError(t, err)
	require.Equal(t, 5, result.Claimed)
	require.Equal(t, 5, result.Attempted)
	require.Equal(t, 5, result.Succeeded)
	require.Equal(t, 7, result.RemainingDue)
	require.LessOrEqual(t, runner.maxActive.Load(), int32(2))
	require.NotEmpty(t, repository.claimLimits)
	for _, limit := range repository.claimLimits {
		require.LessOrEqual(t, limit, 4, "claim cannot exceed available slots times prefetch")
	}
	require.Equal(t, 1, repository.countDueCalls)
}

func TestSchedulerCancellationReleasesUnstartedClaims(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{targets: schedulerTargets(2, now)}
	controller := &schedulerPermitController{}
	block := make(chan struct{})
	started := make(chan int64, 2)
	runner := &schedulerRunner{block: block, started: started}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	scheduler.maxConcurrency = 1

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = scheduler.RunDue(ctx, 2)
		close(done)
	}()
	startedID := <-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not drain after cancellation")
	}
	require.NotEmpty(t, repository.releases)
	releasedIDs := make([]int64, 0, len(repository.releases))
	for _, release := range repository.releases {
		releasedIDs = append(releasedIDs, release.TargetID)
	}
	require.Contains(t, releasedIDs, int64(3)-startedID)
}

func TestSchedulerReschedulesBeforeLeaseExpiresDuringPermitWait(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	repository := &schedulerRepository{targets: []domain.ScrapeTarget{target}}
	controller := &schedulerPermitController{decisions: []domain.PermitDecision{{
		RetryAt: *target.LeaseExpiresAt,
	}}}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	result, err := scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, result.Attempted)
	require.Len(t, repository.reschedules, 1)
	require.Equal(t, *target.LeaseExpiresAt, repository.reschedules[0])
}

func TestSchedulerPermitWaitUsesInjectedContextAwareWait(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	target.LeaseExpiresAt = pointerTime(now.Add(10 * time.Minute))
	repository := &schedulerRepository{targets: []domain.ScrapeTarget{target}}
	controller := &schedulerPermitController{decisions: []domain.PermitDecision{
		{RetryAt: now.Add(time.Second)},
		{Permit: &domain.HostPermit{ID: 1, ExpiresAt: now.Add(time.Minute)}},
	}}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	var waits []time.Duration
	scheduler.wait = func(context.Context, time.Duration) error {
		waits = append(waits, time.Second)
		return nil
	}

	result, err := scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Attempted)
	require.Len(t, waits, 1)
}

func TestRefreshNowCoalescesOnValidationSequence(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	ref := schedulerTargets(1, now)[0].Ref
	baseline := schedulerTargets(1, now)[0]
	baseline.Ref = ref
	baseline.ValidationSeq = 4
	advanced := baseline
	advanced.ValidationSeq = 5
	repository := &schedulerRepository{
		targetReads: []domain.ScrapeTarget{baseline, baseline, advanced},
		claimOneErr: domain.ErrTargetLeased,
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	scheduler.wait = func(context.Context, time.Duration) error { return nil }

	require.NoError(t, scheduler.RefreshNow(context.Background(), ref))
	require.Zero(t, runner.attempts.Load(), "coalesced refresh must not issue duplicate upstream work")
}

func TestRefreshNowPollJitterIsDeterministicAndBounded(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	newScheduler := func() *Scheduler {
		return newSchedulerTest(
			&schedulerRepository{},
			&schedulerPermitController{},
			&schedulerRunner{},
			now,
		)
	}

	first := newScheduler()
	firstDelays := []time.Duration{
		first.refreshPollDelay(),
		first.refreshPollDelay(),
		first.refreshPollDelay(),
	}
	second := newScheduler()
	secondDelays := []time.Duration{
		second.refreshPollDelay(),
		second.refreshPollDelay(),
		second.refreshPollDelay(),
	}

	require.Equal(t, firstDelays, secondDelays, "injected random source must make tests deterministic")
	for _, delay := range firstDelays {
		require.GreaterOrEqual(t, delay, 100*time.Millisecond)
		require.LessOrEqual(t, delay, 500*time.Millisecond)
	}
	require.NotEqual(t, firstDelays[0], firstDelays[1], "polling must be jittered")
}

func TestRefreshNowExecutesClaimedTargetAndAcceptsUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	repository := &schedulerRepository{
		targetReads:    []domain.ScrapeTarget{target},
		claimOneTarget: target,
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	require.NoError(t, scheduler.RefreshNow(context.Background(), target.Ref))
	require.Equal(t, int32(1), runner.attempts.Load())
}

func TestRefreshNowReturnsFailedAttempt(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	repository := &schedulerRepository{
		targetReads:    []domain.ScrapeTarget{target},
		claimOneTarget: target,
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{results: map[int64]RunResult{
		target.ID: {TargetID: target.ID, Outcome: "transient_error", Succeeded: false},
	}}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	err := scheduler.RefreshNow(context.Background(), target.Ref)
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
}

func TestRefreshNowCallerTimeoutDoesNotIssueDuplicateWork(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	target.ValidationSeq = 4
	repository := &schedulerRepository{
		targetReads:    []domain.ScrapeTarget{target, target},
		claimOneErr:    domain.ErrTargetLeased,
		claimOneTarget: domain.ScrapeTarget{},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)
	scheduler.wait = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := scheduler.RefreshNow(ctx, target.Ref)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, runner.attempts.Load(),
		"a timed-out coalesced caller must not launch duplicate upstream work")
}

func TestRefreshNowReturnsHostPauseWithoutPolling(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	repository := &schedulerRepository{
		targetReads: []domain.ScrapeTarget{target},
		claimOneErr: domain.ErrHostPaused,
	}
	scheduler := newSchedulerTest(
		repository,
		&schedulerPermitController{},
		&schedulerRunner{},
		now,
	)
	var waits atomic.Int32
	scheduler.wait = func(context.Context, time.Duration) error {
		waits.Add(1)
		return nil
	}

	err := scheduler.RefreshNow(context.Background(), target.Ref)
	require.ErrorIs(t, err, domain.ErrHostPaused)
	require.Zero(t, waits.Load())
	require.Equal(t, 1, repository.claimOneCalls)
}

func TestRefreshNowReclaimsAfterContendedLeaseDisappears(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	baseline := target
	baseline.ValidationSeq = 4
	released := baseline
	released.LeaseOwner = ""
	released.LeaseExpiresAt = nil
	claimed := released
	claimed.LeaseOwner = "worker"
	claimed.LeaseGeneration++
	claimed.LeaseExpiresAt = pointerTime(now.Add(2 * time.Minute))
	repository := &schedulerRepository{
		targetReads:     []domain.ScrapeTarget{baseline, released},
		claimOneTargets: []domain.ScrapeTarget{{}, claimed},
		claimOneErrors:  []error{domain.ErrTargetLeased, nil},
	}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(
		repository,
		&schedulerPermitController{},
		runner,
		now,
	)
	scheduler.wait = func(context.Context, time.Duration) error { return nil }

	require.NoError(t, scheduler.RefreshNow(context.Background(), target.Ref))
	require.Equal(t, 2, repository.claimOneCalls)
	require.Equal(t, int32(1), runner.attempts.Load())
}

func TestRefreshNowReturnsCoalescedTerminalFailureWithoutDuplicateWork(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := schedulerTargets(1, now)[0]
	target.ValidationSeq = 4
	target.ConsecutiveFailures = 2
	failed := target
	failed.LeaseOwner = ""
	failed.LeaseExpiresAt = nil
	failed.ConsecutiveFailures = 3
	failed.NextRunAt = now.Add(4 * time.Minute)
	repository := &schedulerRepository{
		targetReads:     []domain.ScrapeTarget{target, failed},
		claimOneTargets: []domain.ScrapeTarget{{}},
		claimOneErrors:  []error{domain.ErrTargetLeased},
	}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(
		repository,
		&schedulerPermitController{},
		runner,
		now,
	)
	scheduler.wait = func(context.Context, time.Duration) error { return nil }

	err := scheduler.RefreshNow(context.Background(), target.Ref)

	require.ErrorIs(t, err, domain.ErrSnapshotRefreshFailed)
	require.Equal(t, 1, repository.claimOneCalls)
	require.Zero(t, runner.attempts.Load(),
		"a coalesced terminal failure must not amplify upstream work")
}

func TestSchedulerRunsMaintenanceOncePerUTCDay(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	current := now
	repository := &schedulerRepository{}
	scheduler := newSchedulerTest(
		repository,
		&schedulerPermitController{},
		&schedulerRunner{},
		now,
	)
	scheduler.clock = func() time.Time { return current }

	_, err := scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	_, err = scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, repository.prunes)

	current = now.Add(24 * time.Hour)
	_, err = scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 2, repository.prunes)
}

func TestSchedulerRetriesFailedMaintenanceOnSameUTCDay(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{pruneErrors: []error{errors.New("temporary prune failure")}}
	scheduler := newSchedulerTest(
		repository,
		&schedulerPermitController{},
		&schedulerRunner{},
		now,
	)

	_, err := scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	_, err = scheduler.RunDue(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 2, repository.prunes)
}
