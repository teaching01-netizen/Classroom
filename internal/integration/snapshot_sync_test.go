package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	disabled       bool
	operations     []string
	sessionReads   []time.Time
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
		disabled := fixture.disabled
		fixture.mu.Unlock()
		if disabled {
			http.Error(w, "Warwick fixture disabled", http.StatusServiceUnavailable)
			return
		}

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
			fixture.operations = append(fixture.operations, "session_fetch")
			fixture.sessionReads = append(fixture.sessionReads, time.Now())
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
			fixture.operations = append(fixture.operations, "toggle")
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

func (f *snapshotWarwickFixture) setDisabled(disabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled = disabled
}

func (f *snapshotWarwickFixture) resetOperations() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operations = nil
}

func (f *snapshotWarwickFixture) operationLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.operations...)
}

func (f *snapshotWarwickFixture) resetSessionReadTimes() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionReads = nil
}

func (f *snapshotWarwickFixture) sessionReadTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.sessionReads...)
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

func (f *snapshotWarwickFixture) checkedState() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checked
}

func snapshotIntegrationRepository(
	t *testing.T,
) (*pgxpool.Pool, *db.SnapshotRepository, string, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for snapshot integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("snapshot_integration_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `DROP SCHEMA "`+schema+`" CASCADE`)
	})
	parsedURL, err := url.Parse(databaseURL)
	require.NoError(t, err)
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	scopedDatabaseURL := parsedURL.String()
	require.NoError(t, db.RunMigrations(scopedDatabaseURL))
	pool, err := db.NewPool(scopedDatabaseURL, false)
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
	return pool, repository, host, scopedDatabaseURL
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
	pool, repository, host, scopedDatabaseURL := snapshotIntegrationRepository(t)
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
	teacher := service.NewTeacherServiceWithDependencies(
		provider,
		provider,
		client,
		scheduler,
		2,
		true,
	)
	fixture.setDisabled(true)
	requestsBeforeRead := fixture.requestCount()
	course, err := teacher.GetCourseDetail(context.Background(), "course-1")
	require.NoError(t, err)
	require.Equal(t, "course-1", course.CourseID)
	require.Equal(t, requestsBeforeRead, fixture.requestCount(),
		"TeacherService snapshot read must not contact disabled Warwick fixture")
	fixture.setDisabled(false)

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
		scopedDatabaseURL,
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
	fixture.resetOperations()
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
	operations := fixture.operationLog()
	require.GreaterOrEqual(t, len(operations), 2)
	require.Equal(t, "toggle", operations[0],
		"Warwick mutation must complete before snapshot reconciliation fetches")
	require.Equal(t, "session_fetch", operations[1])
	reconciled, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(3), reconciled.Version)

	eventDeadline = time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			metadata, ok := event.Data.(domain.SnapshotMetadata)
			if event.Type == "SnapshotCommitted" &&
				ok &&
				metadata.ResourceKey == sessionRef.ResourceKey &&
				metadata.Version == 3 {
				goto toggleEventReceived
			}
		case <-eventDeadline:
			t.Fatal("toggle reconciliation event not received")
		}
	}

toggleEventReceived:
	listenerRows, err := pool.Query(context.Background(), `
		SELECT pid
		FROM pg_stat_activity
		WHERE application_name='snapshot-notification-listener'
		  AND pid <> pg_backend_pid()`)
	require.NoError(t, err)
	var listenerPIDs []int32
	for listenerRows.Next() {
		var pid int32
		require.NoError(t, listenerRows.Scan(&pid))
		listenerPIDs = append(listenerPIDs, pid)
	}
	require.NoError(t, listenerRows.Err())
	listenerRows.Close()
	require.Len(t, listenerPIDs, 1,
		"the integration test must disconnect the real LISTEN connection")
	var terminated bool
	require.NoError(t, pool.QueryRow(
		context.Background(),
		`SELECT pg_terminate_backend($1)`,
		listenerPIDs[0],
	).Scan(&terminated))
	require.True(t, terminated)

	fixture.setSession(4, true)
	require.NoError(t, scheduler.RefreshNow(context.Background(), sessionRef))
	afterGap, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Equal(t, int64(4), afterGap.Version)
	reconciled = afterGap

	repairEvents := 0
	repairDeadline := time.After(4 * time.Second)
	for repairEvents == 0 {
		select {
		case event := <-events:
			metadata, ok := event.Data.(domain.SnapshotMetadata)
			if event.Type == "SnapshotCommitted" &&
				ok &&
				metadata.ResourceKey == sessionRef.ResourceKey &&
				metadata.Version == 4 {
				repairEvents++
			}
		case <-repairDeadline:
			t.Fatal("listener reconnect did not reconcile the missed committed version")
		}
	}
	noDuplicateDeadline := time.After(250 * time.Millisecond)
	for {
		select {
		case event := <-events:
			metadata, ok := event.Data.(domain.SnapshotMetadata)
			if event.Type == "SnapshotCommitted" &&
				ok &&
				metadata.ResourceKey == sessionRef.ResourceKey &&
				metadata.Version == 4 {
				repairEvents++
			}
		case <-noDuplicateDeadline:
			require.Equal(t, 1, repairEvents,
				"reconnect reconciliation must publish one repair event")
			goto listenerGapVerified
		}
	}

listenerGapVerified:
	type concurrentToggleResult struct {
		response *domain.ToggleCheckinResponse
		err      error
	}
	toggleStart := make(chan struct{})
	toggleResults := make(chan concurrentToggleResult, 2)
	for _, desired := range []bool{false, true} {
		desired := desired
		go func() {
			<-toggleStart
			response, toggleErr := teacher.ToggleCheckin(
				context.Background(),
				"course-1",
				"session-1",
				"student-1",
				desired,
			)
			toggleResults <- concurrentToggleResult{
				response: response,
				err:      toggleErr,
			}
		}()
	}
	close(toggleStart)
	for range 2 {
		result := <-toggleResults
		require.NoError(t, result.err)
		require.NotNil(t, result.response)
	}
	// If one request coalesced behind the other's validation, its desired
	// state is intentionally marked pending. The due marker must allow one
	// bounded repair attempt to converge without any stale-generation commit.
	require.NoError(t, scheduler.RefreshNow(context.Background(), sessionRef))
	concurrentReconciled, err := repository.Current(context.Background(), sessionRef)
	require.NoError(t, err)
	var concurrentDetail domain.SessionDetail
	require.NoError(t, json.Unmarshal(concurrentReconciled.Payload, &concurrentDetail))
	require.Len(t, concurrentDetail.Students, 1)
	require.Equal(t, fixture.checkedState(), concurrentDetail.Students[0].CheckedIn)
	targetAfterToggles, err := repository.Target(context.Background(), sessionRef)
	require.NoError(t, err)
	require.Empty(t, targetAfterToggles.LeaseOwner)
	require.Nil(t, targetAfterToggles.LeaseExpiresAt)
	var runCount, distinctGenerations int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*), COUNT(DISTINCT lease_generation)
		FROM scrape_runs
		WHERE target_id=$1`,
		targetAfterToggles.ID,
	).Scan(&runCount, &distinctGenerations))
	require.Equal(t, runCount, distinctGenerations)
	reconciled = concurrentReconciled

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
			(SELECT COUNT(*) FROM scrape_host_state AS row
			 WHERE to_jsonb(row)::text ILIKE '%integration-cookie%'
			    OR to_jsonb(row)::text ILIKE '%fixture-password%')
			+
			(SELECT COUNT(*) FROM scrape_targets AS row
			 WHERE to_jsonb(row)::text ILIKE '%integration-cookie%'
			    OR to_jsonb(row)::text ILIKE '%fixture-password%')
			+
			(SELECT COUNT(*) FROM scrape_runs AS row
			 WHERE to_jsonb(row)::text ILIKE '%integration-cookie%'
			    OR to_jsonb(row)::text ILIKE '%fixture-password%')
			+
			(SELECT COUNT(*) FROM scrape_snapshots AS row
			 WHERE to_jsonb(row)::text ILIKE '%integration-cookie%'
			    OR to_jsonb(row)::text ILIKE '%fixture-password%')
			+
			(SELECT COUNT(*) FROM scrape_host_permits AS row
			 WHERE to_jsonb(row)::text ILIKE '%integration-cookie%'
			    OR to_jsonb(row)::text ILIKE '%fixture-password%')`,
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

func TestConcurrentColdSnapshotMissesCoalesceByValidationSequence(t *testing.T) {
	_, repository, host, _ := snapshotIntegrationRepository(t)
	fixture := newSnapshotWarwickFixture(t)
	fixture.setResponseDelay(100 * time.Millisecond)
	sessionPool, err := warwick.NewSessionPool(
		"snapshot-cold@example.invalid",
		"fixture-password",
		fixture.login.URL,
		1,
		2,
		1,
	)
	require.NoError(t, err)
	client := warwick.NewClassroomClientFromPool(
		sessionPool,
		warwick.TierTeacher,
	)
	client.SetBaseURL(fixture.api.URL)
	client.SetUserID("cold-user")
	source := warwick.NewSnapshotSource(client, 1<<20)
	scheduler := snapshotScheduler(
		repository,
		source,
		"cold-worker",
		500*time.Millisecond,
	)
	provider := service.NewSnapshotProvider(
		repository,
		scheduler,
		host,
		time.Now,
	)

	type coldResult struct {
		detail *domain.SessionDetail
		err    error
	}
	start := make(chan struct{})
	results := make(chan coldResult, 2)
	for range 2 {
		go func() {
			<-start
			detail, readErr := provider.GetSessionDetail(
				context.Background(),
				"course-cold",
				"cold-session",
			)
			results <- coldResult{detail: detail, err: readErr}
		}()
	}
	close(start)
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		require.NotNil(t, result.detail)
		require.Equal(t, "cold-session", result.detail.SessionID)
	}
	require.Equal(
		t,
		1,
		fixture.requestCount(),
		"concurrent cold misses must share one Warwick validation attempt",
	)
	ref := provider.SessionRef("course-cold", "cold-session")
	current, err := repository.Current(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.Equal(t, int64(1), current.ValidationSeq)
}

func TestTwoSchedulersSharePostgreSQLHostAdmission(t *testing.T) {
	_, firstRepository, host, scopedDatabaseURL := snapshotIntegrationRepository(t)
	secondPool, err := db.NewPool(scopedDatabaseURL, false)
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

func TestTwoSchedulersSharePostgreSQLHostRate(t *testing.T) {
	pool, firstRepository, host, scopedDatabaseURL := snapshotIntegrationRepository(t)
	secondPool, err := db.NewPool(scopedDatabaseURL, false)
	require.NoError(t, err)
	defer secondPool.Close()
	secondRepository := db.NewSnapshotRepository(secondPool)

	fixture := newSnapshotWarwickFixture(t)
	sessionPool, err := warwick.NewSessionPool(
		"snapshot-rate@example.invalid",
		"fixture-password",
		fixture.login.URL,
		1,
		2,
		1,
	)
	require.NoError(t, err)
	client := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
	client.SetBaseURL(fixture.api.URL)
	client.SetUserID("rate-user")
	source := warwick.NewSnapshotSource(client, 1<<20)

	now := time.Now().UTC()
	require.NoError(t, firstRepository.Seed(context.Background(), []domain.TargetSeed{
		snapshotSeed(domain.TargetRef{
			Host: host, Kind: domain.SnapshotSessionDetail,
			ParentKey: "course-rate", ResourceKey: "rate-session-1",
		}, now),
		snapshotSeed(domain.TargetRef{
			Host: host, Kind: domain.SnapshotSessionDetail,
			ParentKey: "course-rate", ResourceKey: "rate-session-2",
		}, now),
	}))
	_, err = pool.Exec(context.Background(), `
		UPDATE scrape_host_state
		SET baseline_requests_per_second=1,
			current_requests_per_second=1,
			burst=1,
			available_tokens=1,
			tokens_updated_at=clock_timestamp(),
			baseline_concurrency=2,
			current_concurrency=2,
			paused_until=NULL,
			updated_at=clock_timestamp()
		WHERE host=$1`,
		host,
	)
	require.NoError(t, err)
	fixture.resetSessionReadTimes()

	firstScheduler := snapshotScheduler(
		firstRepository,
		source,
		"rate-worker-1",
		500*time.Millisecond,
	)
	secondScheduler := snapshotScheduler(
		secondRepository,
		source,
		"rate-worker-2",
		500*time.Millisecond,
	)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, scheduler := range []*scraper.Scheduler{firstScheduler, secondScheduler} {
		scheduler := scheduler
		go func() {
			<-start
			tick, runErr := scheduler.RunDue(context.Background(), 1)
			if runErr == nil && tick.Attempted != 1 {
				runErr = fmt.Errorf("expected one attempt, got %+v", tick)
			}
			results <- runErr
		}()
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)

	readTimes := fixture.sessionReadTimes()
	require.Len(t, readTimes, 2)
	if readTimes[1].Before(readTimes[0]) {
		readTimes[0], readTimes[1] = readTimes[1], readTimes[0]
	}
	require.GreaterOrEqual(t, readTimes[1].Sub(readTimes[0]), 850*time.Millisecond,
		"independent schedulers must share the one-request-per-second host token bucket")
}
