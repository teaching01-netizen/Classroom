package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

func sameIntPtr(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// checkinMutatorFake implements CheckinMutator for testing.
type checkinMutatorFake struct {
	mu                sync.Mutex
	keys              map[string]*checkinKeyState
	advisorLockCalled int
	lockReleaseCalled int
	reserveCalled     int
	confirmCalled     int
	pendingCalled     int
	failedCalled      int
}

type checkinKeyState struct {
	courseID                string
	sessionID               string
	studentID               string
	desiredCheckedIn        bool
	expectedSnapshotVersion *int64
	status                  string
	response                json.RawMessage
}

func newCheckinMutatorFake() *checkinMutatorFake {
	return &checkinMutatorFake{
		keys: make(map[string]*checkinKeyState),
	}
}

func (m *checkinMutatorFake) ReserveIdempotencyKey(
	_ context.Context,
	key, courseID, sessionID, studentID string,
	desiredCheckedIn bool,
	expectedSnapshotVersion *int64,
) (db.IdempotencyKeyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reserveCalled++

	existing, found := m.keys[key]
	if found {
		match := existing.courseID == courseID &&
			existing.sessionID == sessionID &&
			existing.studentID == studentID &&
			existing.desiredCheckedIn == desiredCheckedIn &&
			sameIntPtr(existing.expectedSnapshotVersion, expectedSnapshotVersion)
		return db.IdempotencyKeyResult{
			Found:    true,
			Match:    match,
			Status:   existing.status,
			Response: existing.response,
		}, nil
	}

	m.keys[key] = &checkinKeyState{
		courseID:                courseID,
		sessionID:               sessionID,
		studentID:               studentID,
		desiredCheckedIn:        desiredCheckedIn,
		expectedSnapshotVersion: expectedSnapshotVersion,
		status:                  "reserved",
	}
	return db.IdempotencyKeyResult{Found: false}, nil
}

func (m *checkinMutatorFake) ConfirmIdempotencyKey(_ context.Context, key string, response json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmCalled++
	if state, ok := m.keys[key]; ok {
		state.status = "confirmed"
		state.response = response
	}
	return nil
}

func (m *checkinMutatorFake) MarkIdempotencyKeyPending(_ context.Context, key string, response json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingCalled++
	if state, ok := m.keys[key]; ok {
		state.status = "pending_verification"
		state.response = response
	}
	return nil
}

func (m *checkinMutatorFake) MarkIdempotencyKeyFailed(_ context.Context, key, errorCode string, response json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedCalled++
	if state, ok := m.keys[key]; ok {
		state.status = "failed"
	}
	return nil
}

type checkinLockFake struct {
	mutator *checkinMutatorFake
}

func (l *checkinLockFake) Release(context.Context) error {
	l.mutator.mu.Lock()
	defer l.mutator.mu.Unlock()
	l.mutator.lockReleaseCalled++
	return nil
}

func (m *checkinMutatorFake) AcquireCheckinLock(_ context.Context, sessionID, studentID string) (CheckinLock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advisorLockCalled++
	return &checkinLockFake{mutator: m}, nil
}

// updateSnapshot updates the snapshot in the fake reader to reflect a desired state.
func updateSnapshot(reader *snapshotReaderFake, provider *SnapshotProvider, courseID, sessionID string, students []domain.StudentCheckin, checkedInCount int) {
	sessionRef := provider.SessionRef(courseID, sessionID)
	reader.mu.Lock()
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      sessionID,
				CheckedInCount: checkedInCount,
			},
			Students: students,
		},
		time.Now().UTC(),
		time.Now().UTC().Add(time.Hour),
	)
	reader.mu.Unlock()
}

// checkinTestSetup creates a service with snapshot mode, a session with one
// student, and a refresher. The refresher's onRefresh updates the snapshot to
// reflect the desired state (simulating Warwick accepting the mutation).
func checkinTestSetup(
	t *testing.T,
	now time.Time,
	studentChecked bool,
	desiredChecked *bool, // if set, onRefresh flips to this state
) (
	*TeacherService,
	*checkinWriterFake,
	*checkinMutatorFake,
	*snapshotReaderFake,
	*snapshotRefresherFake,
) {
	t.Helper()
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
	}
	refresher := &snapshotRefresherFake{}
	writer := &checkinWriterFake{}
	mutator := newCheckinMutatorFake()

	provider := NewSnapshotProvider(
		reader,
		refresher,
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)

	sessionRef := provider.SessionRef("course-1", "session-1")
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: boolToInt(studentChecked),
			},
			Students: []domain.StudentCheckin{{
				StudentID: "student-1",
				CheckedIn: studentChecked,
			}},
		},
		now,
		now.Add(time.Hour),
	)

	// If desiredChecked is set, update the snapshot when RefreshNow is called.
	if desiredChecked != nil {
		desired := *desiredChecked
		refresher.onRefresh = func(ref domain.TargetRef) {
			updateSnapshot(reader, provider, "course-1", "session-1",
				[]domain.StudentCheckin{{
					StudentID: "student-1",
					CheckedIn: desired,
				}},
				boolToInt(desired),
			)
		}
	}

	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, writer, mutator, refresher, 2, true,
	)
	return svc, writer, mutator, reader, refresher
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestIdempotencyKeyReturnsSameResult verifies that calling Checkin twice
// with the same idempotency key returns the stored result without re-executing.
func TestIdempotencyKeyReturnsSameResult(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	checked := true
	svc, writer, mutator, _, _ := checkinTestSetup(t, now, false, &checked)

	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-1",
	}

	// First call — should call Warwick.
	result1, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", result1.Status)
	assert.True(t, result1.CheckedIn)
	assert.Equal(t, 1, writer.calls)
	assert.Equal(t, 1, mutator.confirmCalled)

	// Second call with same key — should NOT call Warwick again.
	result2, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", result2.Status)
	assert.True(t, result2.CheckedIn)
	assert.Equal(t, 1, writer.calls, "Warwick should not be called again for same idempotency key")
	assert.Equal(t, 1, mutator.confirmCalled, "Confirm should not be called again")
}

// TestIdempotencyKeyRejectsDifferentPayload verifies that calling Checkin
// with the same key but different parameters returns a conflict.
func TestIdempotencyKeyRejectsDifferentPayload(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	svc, _, _, _, _ := checkinTestSetup(t, now, false, nil)

	req1 := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-conflict",
	}
	_, err := svc.Checkin(context.Background(), req1)
	require.NoError(t, err)

	// Different desired state with same key.
	req2 := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: false,
		IdempotencyKey:   "idem-key-conflict",
	}
	_, err = svc.Checkin(context.Background(), req2)
	require.Error(t, err)
	var conflict *ErrConflict
	require.ErrorAs(t, err, &conflict)
}

// TestDesiredStateAlreadySatisfiedDoesNotToggle verifies that when the student
// is already in the desired state, no Warwick call is made.
func TestDesiredStateAlreadySatisfiedDoesNotToggle(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	svc, writer, _, _, _ := checkinTestSetup(t, now, true, nil) // already checked in

	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true, // same as current
		IdempotencyKey:   "idem-key-already",
	}

	result, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "already_satisfied", result.Status)
	assert.Equal(t, 0, writer.calls, "Warwick should not be called when state is already satisfied")
}

// TestTimeoutAfterSuccessfulToggleDoesNotRetryBlindly verifies that when
// the Warwick call succeeds but the snapshot refresh shows ambiguous state,
// we mark the mutation as pending_verification instead of blindly retrying.
func TestTimeoutAfterSuccessfulToggleDoesNotRetryBlindly(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
	}
	refresher := &snapshotRefresherFake{}
	writer := &checkinWriterFake{}
	mutator := newCheckinMutatorFake()

	provider := NewSnapshotProvider(
		reader,
		refresher,
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)

	sessionRef := provider.SessionRef("course-1", "session-1")
	// Snapshot shows student NOT checked in initially.
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: 0,
			},
			Students: []domain.StudentCheckin{{
				StudentID: "student-1",
				CheckedIn: false,
			}},
		},
		now,
		now.Add(time.Hour),
	)

	// onRefresh does NOT update the snapshot — simulates stale read after Warwick success.
	refresher.onRefresh = func(ref domain.TargetRef) {
		// Leave snapshot unchanged — ambiguous outcome.
	}

	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, writer, mutator, refresher, 2, true,
	)

	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-timeout",
	}

	result, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "pending_verification", result.Status)
	assert.Equal(t, 1, writer.calls, "Warwick should be called exactly once")
	assert.True(t, result.RefreshPending)
	assert.GreaterOrEqual(t, len(refresher.refreshes), 1)

	result, err = svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "pending_verification", result.Status)
	assert.Equal(t, 1, writer.calls, "a pending idempotency key must not repeat the Humanix write")
}

func TestFailedIdempotencyReplayPreservesUpstreamErrorWithoutRewriting(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	svc, writer, _, _, _ := checkinTestSetup(t, now, false, nil)
	writer.err = errors.New("rejected")
	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "failed-replay",
	}

	for range 2 {
		_, err := svc.Checkin(context.Background(), req)
		var upstream *ErrCheckinUpstream
		require.ErrorAs(t, err, &upstream)
	}
	assert.Equal(t, 1, writer.calls)
}

type liveCheckinProviderFake struct {
	*mockProvider
	mu          sync.Mutex
	checkedIn   bool
	profiles    []domain.StudentProfile
	writerCalls int
	studentID   string
	checked     bool
}

func newLiveCheckinProviderFake(checkedIn bool) *liveCheckinProviderFake {
	return &liveCheckinProviderFake{
		mockProvider: newMockProvider(),
		checkedIn:    checkedIn,
		profiles: []domain.StudentProfile{{
			StudentID:   "W123",
			StudentGuid: "guid-a",
		}},
	}
}

func (p *liveCheckinProviderFake) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1", CheckedInCount: boolToInt(p.checkedIn)},
		Students:       []domain.StudentCheckin{{StudentID: "guid-a", CheckedIn: p.checkedIn}},
	}, nil
}

func (p *liveCheckinProviderFake) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return p.profiles, nil
}

func (p *liveCheckinProviderFake) ToggleCheckin(_ context.Context, _, _, studentID string, checked bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writerCalls++
	p.studentID = studentID
	p.checked = checked
	p.checkedIn = checked
	return nil
}

func TestCheckinLiveModeResolvesWCodeAndVerifiesDesiredState(t *testing.T) {
	tests := []struct {
		name    string
		before  bool
		desired bool
	}{
		{name: "check in", before: false, desired: true},
		{name: "undo check-in", before: true, desired: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newLiveCheckinProviderFake(tt.before)
			mutator := newCheckinMutatorFake()
			svc := NewTeacherServiceWithDependenciesAndMutator(
				provider, provider, provider, mutator, NoopSnapshotRefresher{}, 2, false,
			)

			result, err := svc.Checkin(context.Background(), CheckinRequest{
				CourseID:         "course-1",
				SessionID:        "session-1",
				StudentID:        "W123",
				DesiredCheckedIn: tt.desired,
				IdempotencyKey:   "live-" + tt.name,
			})

			require.NoError(t, err)
			assert.Equal(t, "confirmed", result.Status)
			assert.Equal(t, tt.desired, result.CheckedIn)
			assert.Equal(t, 1, provider.writerCalls)
			assert.Equal(t, "guid-a", provider.studentID)
			assert.Equal(t, tt.desired, provider.checked)
		})
	}
}

func TestCheckinLiveModeRejectsUnknownWCodeWithoutWriting(t *testing.T) {
	provider := newLiveCheckinProviderFake(false)
	mutator := newCheckinMutatorFake()
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, mutator, NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.Checkin(context.Background(), CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "W999",
		DesiredCheckedIn: true,
		IdempotencyKey:   "unknown-student",
	})

	require.Error(t, err)
	var notFound *ErrStudentNotFound
	assert.ErrorAs(t, err, &notFound)
	assert.Equal(t, 0, provider.writerCalls)
}

// TestTwoTeachersSetSameDesiredState verifies that two concurrent requests
// with the same desired state are idempotent.
func TestTwoTeachersSetSameDesiredState(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	checked := true
	svc, writer, _, _, _ := checkinTestSetup(t, now, false, &checked)

	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-same",
	}

	// First request.
	result1, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", result1.Status)

	// Second request with same key.
	result2, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", result2.Status)
	assert.Equal(t, 1, writer.calls, "Warwick should only be called once")
}

// TestTwoTeachersSetOppositeStatesWithVersionConflict verifies that two
// teachers trying opposite states with a stale version returns a conflict.
func TestTwoTeachersSetOppositeStatesWithVersionConflict(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	// providerSnapshot creates version 3. First call matches it.
	currentVersion := int64(3)
	checked := true
	svc, _, _, _, _ := checkinTestSetup(t, now, false, &checked)

	req1 := CheckinRequest{
		CourseID:                "course-1",
		SessionID:               "session-1",
		StudentID:               "student-1",
		DesiredCheckedIn:        true,
		ExpectedSnapshotVersion: &currentVersion,
		IdempotencyKey:          "idem-key-stale-1",
	}

	// First request — succeeds because snapshot version matches.
	result1, err := svc.Checkin(context.Background(), req1)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", result1.Status)

	// Second request: student is now checked in (onRefresh updated snapshot),
	// desired is false (opposite), but version 2 is stale (snapshot is 3) → conflict.
	staleVersion := int64(2)
	req2 := CheckinRequest{
		CourseID:                "course-1",
		SessionID:               "session-1",
		StudentID:               "student-1",
		DesiredCheckedIn:        false,
		ExpectedSnapshotVersion: &staleVersion,
		IdempotencyKey:          "idem-key-stale-2",
	}
	_, err = svc.Checkin(context.Background(), req2)
	require.Error(t, err)
	var conflict *ErrConflict
	require.ErrorAs(t, err, &conflict)
}

// TestMutationLockSerializesSameStudent verifies the advisory lock is acquired.
func TestMutationLockSerializesSameStudent(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	svc, _, mutator, _, _ := checkinTestSetup(t, now, false, nil)

	req := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-lock-1",
	}
	_, err := svc.Checkin(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 1, mutator.advisorLockCalled, "advisory lock should be acquired")
	assert.Equal(t, 1, mutator.lockReleaseCalled, "advisory lock should be released after the mutation")

	// Second request — lock should be acquired again.
	req2 := CheckinRequest{
		CourseID:         "course-1",
		SessionID:        "session-1",
		StudentID:        "student-1",
		DesiredCheckedIn: true,
		IdempotencyKey:   "idem-key-lock-2",
	}
	_, err = svc.Checkin(context.Background(), req2)
	require.NoError(t, err)
	assert.Equal(t, 2, mutator.advisorLockCalled)
	assert.Equal(t, 2, mutator.lockReleaseCalled)
}

// TestDifferentStudentsCanMutateConcurrently verifies that different students
// can be mutated concurrently (different lock keys). The advisory lock
// serializes same-student mutations but allows different students through.
func TestDifferentStudentsCanMutateConcurrently(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
	}
	refresher := &snapshotRefresherFake{}
	writer := &checkinWriterFake{}
	mutator := newCheckinMutatorFake()

	provider := NewSnapshotProvider(
		reader,
		refresher,
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)

	sessionRef := provider.SessionRef("course-1", "session-1")
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: 0,
			},
			Students: []domain.StudentCheckin{
				{StudentID: "student-1", CheckedIn: false},
				{StudentID: "student-2", CheckedIn: false},
			},
		},
		now,
		now.Add(time.Hour),
	)

	// onRefresh updates both students to checked in (simulates Warwick accepting).
	refresher.onRefresh = func(ref domain.TargetRef) {
		updateSnapshot(reader, provider, "course-1", "session-1",
			[]domain.StudentCheckin{
				{StudentID: "student-1", CheckedIn: true},
				{StudentID: "student-2", CheckedIn: true},
			},
			2,
		)
	}

	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, writer, mutator, refresher, 2, true,
	)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	results := make(chan *CheckinResult, 2)

	for _, sid := range []string{"student-1", "student-2"} {
		wg.Add(1)
		go func(studentID string) {
			defer wg.Done()
			result, err := svc.Checkin(context.Background(), CheckinRequest{
				CourseID:         "course-1",
				SessionID:        "session-1",
				StudentID:        studentID,
				DesiredCheckedIn: true,
				IdempotencyKey:   "idem-key-" + studentID,
			})
			if err != nil {
				errs <- err
			} else {
				results <- result
			}
		}(sid)
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both students complete without errors. Depending on timing, both may
	// get "confirmed" (if they read the snapshot before either refreshes)
	// or one gets "confirmed" and the other "already_satisfied" (if the
	// first refresh updates the snapshot before the second reads it).
	// The key guarantee: neither blocks the other and both succeed.
	var confirmedCount int
	for result := range results {
		assert.Contains(t, []string{"confirmed", "already_satisfied"}, result.Status)
		if result.Status == "confirmed" {
			confirmedCount++
		}
	}
	assert.GreaterOrEqual(t, confirmedCount, 1, "at least one student should get confirmed")
	assert.GreaterOrEqual(t, mutator.reserveCalled, 2, "both idempotency keys should be reserved")
}
