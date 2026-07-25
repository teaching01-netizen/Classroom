package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/api"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/scraper"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

type snapshotWarwickFixture struct {
	mu             sync.Mutex
	sessionVersion int
	checked        bool
	toggleCalls    int
	requests       int
	rateLimited    bool
	responseDelay  time.Duration
	activeSessions int
	maxActive      int
	api            *httptest.Server
	login          *httptest.Server
}

func newSnapshotWarwickFixture(t *testing.T) *snapshotWarwickFixture {
	t.Helper()
	fixture := &snapshotWarwickFixture{sessionVersion: 1}
	fixture.login = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "ASP.NET_SessionId=integration-cookie; Path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(fixture.login.Close)
	fixture.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		fixture.mu.Lock()
		fixture.requests++
		version := fixture.sessionVersion
		checked := fixture.checked
		rateLimited := fixture.rateLimited
		fixture.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "ClassAttendanceSearch") &&
			!strings.Contains(r.URL.Path, "Detail"):
			w.Header().Set("ETag", `"catalog-v1"`)
			_, _ = w.Write([]byte(`{
				"draw":1,"recordsTotal":1,"recordsFiltered":1,
				"data":[{
					"ID":"course-1","CourseName":"Snapshot Course","Cycle":"",
					"Enrolled":1,"StartDate":"2026-01-01T00:00:00",
					"EndDate":"2026-12-31T00:00:00"
				}]
			}`))
		case strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch"):
			w.Header().Set("ETag", `"course-v1"`)
			_, _ = w.Write([]byte(`{
				"draw":1,"recordsTotal":1,"recordsFiltered":1,
				"data":[{"dID":"session-1","dName":"Session 1","dStatus":"Active"}]
			}`))
		case strings.Contains(r.URL.Path, "ClassAttendanceStudentCheckInSearch"):
			sessionID := r.Form.Get("CourseCampaignID")
			fixture.mu.Lock()
			fixture.activeSessions++
			if fixture.activeSessions > fixture.maxActive {
				fixture.maxActive = fixture.activeSessions
			}
			responseDelay := fixture.responseDelay
			fixture.mu.Unlock()
			defer func() {
				fixture.mu.Lock()
				fixture.activeSessions--
				fixture.mu.Unlock()
			}()
			if responseDelay > 0 {
				select {
				case <-time.After(responseDelay):
				case <-r.Context().Done():
					return
				}
			}
			if sessionID == "timeout" {
				select {
				case <-time.After(250 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
			if sessionID == "session-1" && rateLimited {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			etag := fmt.Sprintf(`"session-v%d"`, version)
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = fmt.Fprintf(w, `{
				"draw":1,"recordsTotal":1,"recordsFiltered":1,
				"data":[{
					"StudentID":"student-1","StudentName":"Student",
					"StudentNickName":"","StudentSchool":"Science",
					"StudentImg":"https://example.invalid/avatar.png",
					"StudentCheckIn":%t,"StudentPPoint":0
				}]
			}`, checked)
		case strings.Contains(r.URL.Path, "ToggleCheckin"):
			fixture.mu.Lock()
			fixture.toggleCalls++
			fixture.checked = r.Form.Get("checked") == "1"
			fixture.sessionVersion++
			fixture.mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fixture.api.Close)
	return fixture
}

func (f *snapshotWarwickFixture) setSession(version int, checked bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionVersion = version
	f.checked = checked
}

func (f *snapshotWarwickFixture) setRateLimited(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rateLimited = enabled
}

func (f *snapshotWarwickFixture) setResponseDelay(delay time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responseDelay = delay
}

func (f *snapshotWarwickFixture) maxActiveSessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func (f *snapshotWarwickFixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *snapshotWarwickFixture) toggleCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.toggleCalls
}

func snapshotIntegrationRepository(
	t *testing.T,
) (*pgxpool.Pool, *db.SnapshotRepository, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for snapshot integration tests")
	}
	require.NoError(t, db.RunMigrations(databaseURL))
	pool, err := db.NewPool(databaseURL, false)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	host := fmt.Sprintf("snapshot-integration-%d.invalid", time.Now().UnixNano())
	repository := db.NewSnapshotRepository(pool)
	require.NoError(t, repository.SeedHost(context.Background(), db.HostStateSeed{
		Host:                      host,
		BaselineRequestsPerSecond: 5,
		Burst:                     5,
		BaselineConcurrency:       2,
		Now:                       time.Now().UTC(),
	}))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM scrape_targets WHERE host=$1`, host)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM scrape_host_state WHERE host=$1`, host)
	})
	return pool, repository, host
}

func snapshotSeed(ref domain.TargetRef, now time.Time) domain.TargetSeed {
	policy := scraper.PolicyFor(ref.Kind, domain.SessionStatusActive)
	return domain.TargetSeed{
		Ref:             ref,
		InitialInterval: policy.Initial,
		MinInterval:     policy.Min,
		MaxInterval:     policy.Max,
		MaxServeAge:     policy.MaxServeAge,
		NextRunAt:       now,
	}
}

func snapshotScheduler(
	repository *db.SnapshotRepository,
	source *warwick.SnapshotSource,
	workerID string,
	fetchTimeout time.Duration,
) *scraper.Scheduler {
	controller := scraper.NewHostController(repository, fetchTimeout+time.Second)
	coordinator := scraper.NewCoordinator(
		source,
		repository,
		controller,
		scraper.CoordinatorConfig{
			FetchTimeout:          fetchTimeout,
			CanonicalPayloadLimit: 1 << 20,
		},
	)
	return scraper.NewScheduler(
		repository,
		controller,
		coordinator,
		scraper.SchedulerConfig{
			WorkerID:            workerID,
			MaxConcurrency:      2,
			PrefetchFactor:      2,
			LeaseDuration:       fetchTimeout + 3*time.Second,
			CommitGrace:         time.Second,
			TickLimit:           20,
			PollInterval:        time.Second,
			RefreshPollInterval: 10 * time.Millisecond,
			SnapshotRetention:   30 * 24 * time.Hour,
			RunRetention:        30 * 24 * time.Hour,
			PruneBatchSize:      100,
		},
	)
}

func TestPostgreSQLSnapshotPipelineAndLiveRollbackContracts(t *testing.T) {
	pool, repository, host := snapshotIntegrationRepository(t)
	fixture := newSnapshotWarwickFixture(t)
	sessionPool, err := warwick.NewSessionPool(
		"snapshot-integration@example.invalid",
		"fixture-password",
		fixture.login.URL,
		1,
		2,
		1,
	)
	require.NoError(t, err)
	client := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
	client.SetBaseURL(fixture.api.URL)
	client.SetUserID("integration-user")
	source := warwick.NewSnapshotSource(client, 1<<20)
	scheduler := snapshotScheduler(repository, source, "integration-worker", 500*time.Millisecond)
	now := time.Now().UTC()
	catalogRef := domain.TargetRef{
		Host: host, Kind: domain.SnapshotCourseCatalog, ResourceKey: "catalog",
	}
	require.NoError(t, repository.Seed(
		context.Background(),
		[]domain.TargetSeed{snapshotSeed(catalogRef, now)},
	))

	require.NoError(t, scheduler.RefreshNow(context.Background(), catalogRef))
	courseRef := domain.TargetRef{
		Host: host, Kind: domain.SnapshotCourseDetail, ResourceKey: "course-1",
	}
	require.NoError(t, scheduler.RefreshNow(context.Background(), courseRef))
	sessionRef := domain.TargetRef{
		Host: host, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "session-1",
	}
	require.NoError(t, scheduler.RefreshNow(context.Background(), sessionRef))
	first, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Version)
	require.Equal(t, int64(1), first.ValidationSeq)

	provider := service.NewSnapshotProvider(repository, scheduler, host, time.Now)
	requestsBeforeRead := fixture.requestCount()
	detail, err := provider.GetSessionDetail(
		context.Background(),
		"course-1",
		"session-1",
	)
	require.NoError(t, err)
	require.Equal(t, "student-1", detail.Students[0].StudentID)
	require.Equal(t, requestsBeforeRead, fixture.requestCount(),
		"snapshot teacher read must not contact Warwick")

	require.NoError(t, scheduler.RefreshNow(context.Background(), sessionRef))
	notModified, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(1), notModified.Version)
	require.Equal(t, int64(2), notModified.ValidationSeq)
	require.True(t, notModified.ValidatedAt.After(first.ValidatedAt) ||
		notModified.ValidatedAt.Equal(first.ValidatedAt))

	hub := service.NewEventHub(32, 32)
	defer hub.Close()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	listener := service.NewSnapshotNotificationListener(
		os.Getenv("TEST_DATABASE_URL"),
		repository,
		hub,
	)
	listenerCtx, stopListener := context.WithCancel(context.Background())
	defer stopListener()
	listenerDone := make(chan error, 1)
	go func() { listenerDone <- listener.Run(listenerCtx) }()
	select {
	case <-listener.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot listener did not become ready")
	}

	fixture.setSession(2, true)
	require.NoError(t, scheduler.RefreshNow(context.Background(), sessionRef))
	second, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Version)
	require.Equal(t, int64(3), second.ValidationSeq)
	eventDeadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			metadata, ok := event.Data.(domain.SnapshotMetadata)
			if event.Type == "SnapshotCommitted" &&
				ok &&
				metadata.ResourceKey == sessionRef.ResourceKey &&
				metadata.Version == 2 {
				goto snapshotEventReceived
			}
		case <-eventDeadline:
			t.Fatal("committed snapshot event not received")
		}
	}

snapshotEventReceived:
	teacher := service.NewTeacherServiceWithDependencies(
		provider,
		provider,
		client,
		scheduler,
		2,
		true,
	)
	toggle, err := teacher.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		false,
	)
	require.NoError(t, err)
	require.False(t, toggle.SnapshotRefreshPending)
	require.Equal(t, 1, fixture.toggleCount())
	reconciled, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(3), reconciled.Version)

	timeoutRef := domain.TargetRef{
		Host: host, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "timeout",
	}
	_, err = pool.Exec(
		context.Background(),
		`UPDATE scrape_host_state
		 SET available_tokens=burst, tokens_updated_at=NOW(), updated_at=NOW()
		 WHERE host=$1`,
		host,
	)
	require.NoError(t, err)
	require.NoError(t, repository.Seed(
		context.Background(),
		[]domain.TargetSeed{snapshotSeed(timeoutRef, time.Now().UTC())},
	))
	timeoutScheduler := snapshotScheduler(
		repository,
		source,
		"timeout-worker",
		50*time.Millisecond,
	)
	timeoutStarted := time.Now().UTC()
	timeoutErr := timeoutScheduler.RefreshNow(context.Background(), timeoutRef)
	require.Error(t, timeoutErr)
	timeoutTarget, err := repository.Target(context.Background(), timeoutRef)
	require.NoError(t, err)
	require.Equal(t, 1, timeoutTarget.ConsecutiveFailures)
	require.False(t, timeoutTarget.NextRunAt.Before(timeoutStarted))
	require.True(t, timeoutTarget.NextRunAt.Before(timeoutStarted.Add(61*time.Second)))

	staleRef := domain.TargetRef{
		Host: host, Kind: domain.SnapshotCourseDetail, ResourceKey: "stale-course",
	}
	require.NoError(t, repository.Seed(
		context.Background(),
		[]domain.TargetSeed{snapshotSeed(staleRef, time.Now().UTC())},
	))
	oldClaim, err := repository.ClaimOne(context.Background(), db.ClaimOneRequest{
		Ref: staleRef, Now: time.Now().UTC(), WorkerID: "old-worker",
		LeaseDuration: time.Second,
	})
	require.NoError(t, err)
	_, err = pool.Exec(
		context.Background(),
		`UPDATE scrape_targets SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`,
		oldClaim.ID,
	)
	require.NoError(t, err)
	_, err = repository.ClaimOne(context.Background(), db.ClaimOneRequest{
		Ref: staleRef, Now: time.Now().UTC(), WorkerID: "new-worker",
		LeaseDuration: time.Second,
	})
	require.NoError(t, err)
	_, err = repository.Commit(context.Background(), db.CommitInput{
		TargetID: oldClaim.ID, WorkerID: "old-worker",
		LeaseGeneration: oldClaim.LeaseGeneration,
		Outcome:         "transient_error", StartedAt: time.Now().Add(-time.Second),
		FinishedAt: time.Now(), NextRunAt: time.Now().Add(time.Minute),
		CurrentInterval:     oldClaim.CurrentInterval,
		ConsecutiveFailures: 1,
	})
	require.ErrorIs(t, err, domain.ErrLeaseLost)

	versionBeforeRateLimit := reconciled.Version
	_, err = pool.Exec(
		context.Background(),
		`UPDATE scrape_host_state
		 SET available_tokens=burst, tokens_updated_at=NOW(), updated_at=NOW()
		 WHERE host=$1`,
		host,
	)
	require.NoError(t, err)
	fixture.setRateLimited(true)
	require.Error(t, scheduler.RefreshNow(context.Background(), sessionRef))
	lastGood, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, versionBeforeRateLimit, lastGood.Version)
	status, err := repository.ScraperStatus(context.Background(), host, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, status.HostPausedUntil)
	require.True(t, status.HostPausedUntil.After(time.Now().Add(14*time.Minute)))

	_, err = pool.Exec(
		context.Background(),
		`UPDATE scrape_targets
		 SET last_validated_at=NOW()-INTERVAL '3 hours'
		 WHERE host=$1 AND kind='session_detail'
		   AND parent_key='course-1' AND resource_key='session-1'`,
		host,
	)
	require.NoError(t, err)
	roomManager := service.NewRoomManager(nil, nil)
	defer roomManager.EventHub().Close()
	router, limiters := api.NewRouter(
		roomManager,
		teacher,
		nil,
		nil,
		api.RouterOptions{WSMaxConns: 10},
	)
	defer limiters.Stop()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/teacher/courses/course-1/sessions/session-1",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "snapshot expired")

	var secretRows int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT
			(SELECT COUNT(*) FROM scrape_snapshots
			 WHERE payload::text ILIKE '%integration-cookie%'
			    OR payload::text ILIKE '%fixture-password%')
			+
			(SELECT COUNT(*) FROM scrape_runs
			 WHERE COALESCE(error_message, '') ILIKE '%integration-cookie%'
			    OR COALESCE(error_message, '') ILIKE '%fixture-password%')`,
	).Scan(&secretRows))
	require.Zero(t, secretRows)
	var forbiddenColumns int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_name IN (
			'scrape_targets','scrape_runs','scrape_snapshots',
			'scrape_host_state','scrape_host_permits'
		)
		  AND column_name ~* '(cookie|authorization|password|raw_html|raw_response)'`,
	).Scan(&forbiddenColumns))
	require.Zero(t, forbiddenColumns)

	stopListener()
	require.ErrorIs(t, <-listenerDone, context.Canceled)
}

func TestLiveRollbackSnapshotReadsDisabledKeepsWarwickAsReadSource(t *testing.T) {
	fixture := newLiveFixture(t)
	liveReader := fixture.client

	first, err := liveReader.GetCourses(context.Background())
	require.NoError(t, err)
	second, err := liveReader.GetCourses(context.Background())
	require.NoError(t, err)

	require.NotEqual(t, first[0].Name, second[0].Name)
	require.Equal(t, int32(2), fixture.courseReads.Load(),
		"SNAPSHOT_READS_ENABLED=false must preserve live Warwick reads")
}

func TestTwoSchedulersSharePostgreSQLHostAdmission(t *testing.T) {
	_, firstRepository, host := snapshotIntegrationRepository(t)
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	secondPool, err := db.NewPool(databaseURL, false)
	require.NoError(t, err)
	defer secondPool.Close()
	secondRepository := db.NewSnapshotRepository(secondPool)

	fixture := newSnapshotWarwickFixture(t)
	fixture.setResponseDelay(100 * time.Millisecond)
	sessionPool, err := warwick.NewSessionPool(
		"snapshot-admission@example.invalid",
		"fixture-password",
		fixture.login.URL,
		1,
		2,
		1,
	)
	require.NoError(t, err)
	client := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
	client.SetBaseURL(fixture.api.URL)
	client.SetUserID("admission-user")
	source := warwick.NewSnapshotSource(client, 1<<20)

	now := time.Now().UTC()
	seeds := make([]domain.TargetSeed, 4)
	for index := range seeds {
		seeds[index] = snapshotSeed(domain.TargetRef{
			Host:        host,
			Kind:        domain.SnapshotSessionDetail,
			ParentKey:   "course-1",
			ResourceKey: fmt.Sprintf("admission-session-%d", index),
		}, now)
	}
	require.NoError(t, firstRepository.Seed(context.Background(), seeds))
	firstScheduler := snapshotScheduler(
		firstRepository,
		source,
		"admission-worker-1",
		500*time.Millisecond,
	)
	secondScheduler := snapshotScheduler(
		secondRepository,
		source,
		"admission-worker-2",
		500*time.Millisecond,
	)

	type schedulerResult struct {
		tick scraper.TickResult
		err  error
	}
	start := make(chan struct{})
	results := make(chan schedulerResult, 2)
	run := func(scheduler *scraper.Scheduler) {
		<-start
		tick, runErr := scheduler.RunDue(context.Background(), 2)
		results <- schedulerResult{tick: tick, err: runErr}
	}
	go run(firstScheduler)
	go run(secondScheduler)
	close(start)

	totalAttempts := 0
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		totalAttempts += result.tick.Attempted
	}
	require.Equal(t, 4, totalAttempts)
	require.Equal(t, 4, fixture.requestCount())
	require.Equal(t, 2, fixture.maxActiveSessions(),
		"two scheduler instances must share the configured host concurrency")
}
