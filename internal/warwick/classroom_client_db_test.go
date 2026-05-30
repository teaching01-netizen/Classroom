//go:build integration

package warwick

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/domain"
)

// mockCheckinRepo implements db.SessionCheckinRepository for testing DB-backed cache behavior.
type mockCheckinRepo struct {
	mu           sync.Mutex
	students     map[string][]domain.StudentCheckin // sessionID -> students
	maxToggledAt map[string]*time.Time              // sessionID -> max toggled at
}

func (m *mockCheckinRepo) GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.students[sessionID]
	if s == nil {
		return []domain.StudentCheckin{}, nil
	}
	result := make([]domain.StudentCheckin, len(s))
	copy(result, s)
	return result, nil
}

func (m *mockCheckinRepo) UpsertFromWarwick(ctx context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.students[sessionID]
	if existing == nil {
		existing = make([]domain.StudentCheckin, 0)
	}

	for _, s := range students {
		found := false
		for i, e := range existing {
			if e.StudentID == s.StudentID {
				// If any toggle has occurred in this session, preserve checked_in
				if m.maxToggledAt[sessionID] != nil {
					existing[i].CheckedIn = e.CheckedIn
				} else {
					existing[i].CheckedIn = s.CheckedIn
				}
				existing[i].Name = s.Name
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, s)
		}
	}
	m.students[sessionID] = existing
	return nil
}

func (m *mockCheckinRepo) UpsertStudent(ctx context.Context, sessionID string, student domain.StudentCheckin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if m.maxToggledAt == nil {
		m.maxToggledAt = make(map[string]*time.Time)
	}
	m.maxToggledAt[sessionID] = &now

	existing := m.students[sessionID]
	found := false
	for i, e := range existing {
		if e.StudentID == student.StudentID {
			existing[i].CheckedIn = student.CheckedIn
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, student)
	}
	m.students[sessionID] = existing
	return nil
}

func (m *mockCheckinRepo) GetMaxToggledAtForSession(ctx context.Context, sessionID string) (*time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maxToggledAt == nil {
		return nil, nil
	}
	t := m.maxToggledAt[sessionID]
	if t == nil {
		return nil, nil
	}
	return t, nil
}

// --- Test helpers ---

// makeSessionDetailResponse creates a minimal StudentCheckInSearchResponse JSON.
func makeSessionDetailResponse(students []StudentCheckInRow) []byte {
	resp := StudentCheckInSearchResponse{
		Draw:            1,
		RecordsTotal:    len(students),
		RecordsFiltered: len(students),
		Data:            students,
	}
	b, _ := json.Marshal(resp)
	return b
}

// newTestLoginServer creates an httptest server that returns a Warwick login cookie.
func newTestLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s
}

// --- Test 1: Read path with DB repo returns cached data after first fetch ---

func TestClassroomClient_GetSessionDetail_WithDBRepo_CachesAfterFirstFetch(t *testing.T) {
	mc := cache.New()
	repo := &mockCheckinRepo{
		students:     make(map[string][]domain.StudentCheckin),
		maxToggledAt: make(map[string]*time.Time),
	}

	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if !strings.Contains(r.URL.Path, "ClassAttendanceStudentCheckInSearch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(makeSessionDetailResponse([]StudentCheckInRow{
			{StudentID: "S1", StudentName: "Alice", StudentCheckIn: false},
			{StudentID: "S2", StudentName: "Bob", StudentCheckIn: true},
		}))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc, repo)
	client.baseURL = apiServer.URL

	// First call: cold cache → DB miss → Warwick fetch → cache
	detail1, err := client.GetSessionDetail("c1", "session1")
	require.NoError(t, err)
	require.NotNil(t, detail1)
	assert.Equal(t, 2, detail1.TotalStudents)
	assert.Equal(t, 1, detail1.CheckedInCount, "S2 is checked in")
	assert.Equal(t, "Alice", detail1.Students[0].Name)

	// Second call: should hit cache (no additional Warwick call)
	detail2, err := client.GetSessionDetail("c1", "session1")
	require.NoError(t, err)
	require.NotNil(t, detail2)
	assert.Equal(t, 2, detail2.TotalStudents)

	assert.Equal(t, 1, apiCalls, "should fetch from Warwick only once")
}

// --- Test 2: Read path without DB repo falls through to pool (existing behavior) ---

func TestClassroomClient_GetSessionDetail_WithoutDBRepo_Works(t *testing.T) {
	mc := cache.New()

	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.Header().Set("Content-Type", "application/json")
		w.Write(makeSessionDetailResponse([]StudentCheckInRow{
			{StudentID: "S1", StudentName: "Alice", StudentCheckIn: true},
		}))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc) // no checkinRepo
	client.baseURL = apiServer.URL

	detail, err := client.GetSessionDetail("c1", "session1")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, 1, detail.TotalStudents)
	assert.True(t, detail.Students[0].CheckedIn)
	assert.Equal(t, 1, apiCalls)
}

// --- Test 3: Toggle path writes to DB synchronously ---

func TestClassroomClient_ToggleCheckin_WithDBRepo_WritesToDB(t *testing.T) {
	mc := cache.New()
	repo := &mockCheckinRepo{
		students:     make(map[string][]domain.StudentCheckin),
		maxToggledAt: make(map[string]*time.Time),
	}

	toggleCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ToggleCheckin") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		toggleCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierInteractive, mc, repo)
	client.baseURL = apiServer.URL

	err = client.ToggleCheckin("c1", "session1", "S1", true)
	require.NoError(t, err)
	assert.True(t, toggleCalled, "should have called Warwick toggle endpoint")

	// Verify UpsertStudent was called on the mock repo
	repo.mu.Lock()
	students := repo.students["session1"]
	maxToggled := repo.maxToggledAt["session1"]
	repo.mu.Unlock()

	require.Len(t, students, 1)
	assert.Equal(t, "S1", students[0].StudentID)
	assert.True(t, students[0].CheckedIn, "student should be checked in after toggle")
	assert.NotNil(t, maxToggled, "maxToggledAt should be set after UpsertStudent")
}

// --- Test 4: Stale cache + DB fresh → repopulates from DB ---

func TestClassroomClient_GetSessionDetail_StaleCache_DBFresh_RepopulatesFromDB(t *testing.T) {
	now := time.Now()
	oldTime := now.Add(-1 * time.Hour)

	mc := cache.New()
	repo := &mockCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"session1": {
				{StudentID: "S1", Name: "Alice", CheckedIn: true},
				{StudentID: "S2", Name: "Bob", CheckedIn: false},
			},
		},
		maxToggledAt: map[string]*time.Time{
			"session1": &now,
		},
	}

	// Pre-populate stale cache with old MaxToggledAt
	staleSession := &CachedSession{
		Detail: &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID: "session1",
			},
			Students: []domain.StudentCheckin{},
		},
		MaxToggledAt: &oldTime,
		CachedAt:     time.Now().Add(-1 * time.Hour),
	}
	mc.Set("session:session1", staleSession, -1*time.Second)

	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc, repo)
	client.baseURL = apiServer.URL

	// Call — should detect DB is fresher and repopulate cache from DB
	detail, err := client.GetSessionDetail("c1", "session1")
	require.NoError(t, err)
	require.NotNil(t, detail)

	assert.Equal(t, 2, detail.TotalStudents, "should have 2 students from DB")
	assert.Equal(t, 1, detail.CheckedInCount, "S1 is checked in")
	assert.Equal(t, "Alice", detail.Students[0].Name)
	assert.Equal(t, "Bob", detail.Students[1].Name)

	// Should NOT have called Warwick
	assert.Equal(t, 0, apiCalls, "should not call Warwick when DB has fresh data")

	// Cache should now have fresh data populated from DB
	cached, ok := mc.Get("session:session1")
	require.True(t, ok, "cache should have fresh entry")
	freshSession, ok := cached.(*CachedSession)
	require.True(t, ok, "cache should hold CachedSession")
	assert.Equal(t, 2, freshSession.Detail.TotalStudents)
	assert.NotNil(t, freshSession.MaxToggledAt, "fresh cache entry should have MaxToggledAt")
	assert.True(t, freshSession.MaxToggledAt.Equal(now), "MaxToggledAt should match DB value")
}

// --- Test 5: Stale cache + DB same → serve stale + async refresh ---

func TestClassroomClient_GetSessionDetail_StaleCache_DBSame_ServesStale(t *testing.T) {
	now := time.Now()

	mc := cache.New()
	repo := &mockCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"session1": {
				{StudentID: "S1", Name: "Alice", CheckedIn: true}, // DB has toggled to true
			},
		},
		maxToggledAt: map[string]*time.Time{
			"session1": &now,
		},
	}

	// Pre-populate stale cache with SAME MaxToggledAt but stale checked_in = false
	staleSession := &CachedSession{
		Detail: &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session1",
				TotalStudents:  1,
				CheckedInCount: 0,
			},
			Students: []domain.StudentCheckin{
				{StudentID: "S1", Name: "Alice", CheckedIn: false},
			},
		},
		MaxToggledAt: &now, // same as DB
		CachedAt:     time.Now().Add(-1 * time.Hour),
	}
	mc.Set("session:session1", staleSession, -1*time.Second)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(makeSessionDetailResponse([]StudentCheckInRow{
			{StudentID: "S1", StudentName: "Alice", StudentCheckIn: true},
		}))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc, repo)
	client.baseURL = apiServer.URL

	// Call — should serve stale data synchronously (MaxToggledAt matches)
	detail, err := client.GetSessionDetail("c1", "session1")
	require.NoError(t, err)
	require.NotNil(t, detail)

	// Returns stale data: S1 shows as NOT checked in (cached false, DB true)
	assert.Equal(t, 1, detail.TotalStudents)
	assert.Equal(t, 0, detail.CheckedInCount, "should return stale checked_in count")
	assert.False(t, detail.Students[0].CheckedIn, "should return stale checked_in state")

	// An async refresh goroutine was spawned — it will update the cache eventually
	// but we don't synchronize with it. Just verify synchronous path is correct.
}
