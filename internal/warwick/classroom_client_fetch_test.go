package warwick

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/domain"
)

func TestFetchCourses_Success(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ClassAttendanceSearch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 2,
			"recordsFiltered": 2,
			"data": [
				{"ID": "c1", "CourseName": "Math 101", "Cycle": "27 May 2026 - 03 Jul 2026", "Enrolled": 30, "StartDate": "2026-05-27T09:00:00", "EndDate": "2026-07-03T17:00:00"},
				{"ID": "c2", "CourseName": "Physics 201", "Cycle": "27 May 2026 - 03 Jul 2026", "Enrolled": 25, "StartDate": "2026-05-27T09:00:00", "EndDate": "2026-07-03T17:00:00"}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 2)
	assert.Equal(t, "c1", courses[0].CourseID)
	assert.Equal(t, "Math 101", courses[0].Name)
	assert.Equal(t, "2026-05-27", courses[0].StartDate)
	assert.Equal(t, "2026-07-03", courses[0].EndDate)
	assert.Equal(t, 30, courses[0].EnrolledCount)
	assert.Equal(t, "c2", courses[1].CourseID)
	assert.Equal(t, "Physics 201", courses[1].Name)
}

func TestFetchCourses_EmptyData(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 0,
			"recordsFiltered": 0,
			"data": []
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	assert.Empty(t, courses)
}

func TestFetchCourses_WarwickReturnsLoginPage(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Login</title></head><body>
			<div class="idg-box-login-primary">
				<input name="password" />
				<a href="/admin/SignIn/ForgotPassword">Forgot Password?</a>
			</div>
		</body></html>`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	_, err = client.GetCourses()
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAuthExpired)
}

func TestFetchCourses_CacheHitSkipsWarwick(t *testing.T) {
	mc := cache.New()

	mc.Set("courses", []domain.CourseSummary{
		{CourseID: "cached-1", Name: "Cached Course"},
	}, 5*time.Minute)

	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 1)
	assert.Equal(t, "Cached Course", courses[0].Name)
	assert.Equal(t, 0, apiCalls, "should not call Warwick when cache is warm")
}

func TestFetchCourses_StaleCacheReturnsStaleAndRefreshes(t *testing.T) {
	mc := cache.New()

	mc.Set("courses", []domain.CourseSummary{
		{CourseID: "stale-1", Name: "Stale Course"},
	}, -1*time.Second)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 1,
			"recordsFiltered": 1,
			"data": [
				{"ID": "fresh-1", "CourseName": "Fresh Course", "Cycle": "", "Enrolled": 10}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 1)
	assert.Equal(t, "Stale Course", courses[0].Name)

	time.Sleep(200 * time.Millisecond)

	cached, ok := mc.Get("courses")
	require.True(t, ok)
	freshCourses := cached.([]domain.CourseSummary)
	require.Len(t, freshCourses, 1)
	assert.Equal(t, "Fresh Course", freshCourses[0].Name)
}

func TestFetchCourses_EnrichmentPopulatesSessionCounts(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			w.Write([]byte(`{
				"draw": 1,
				"recordsTotal": 3,
				"recordsFiltered": 3,
				"data": [
					{"dID": "s1", "dName": "Week 1", "dStatus": "Finished"},
					{"dID": "s2", "dName": "Week 2", "dStatus": "Finished"},
					{"dID": "s3", "dName": "Week 3", "dStatus": "Active"}
				]
			}`))
		} else {
			w.Write([]byte(`{
				"draw": 1,
				"recordsTotal": 1,
				"recordsFiltered": 1,
				"data": [
					{"ID": "c1", "CourseName": "Math 101", "Cycle": "", "Enrolled": 30, "StartDate": "2026-05-27T09:00:00", "EndDate": "2026-07-03T17:00:00"}
				]
			}`))
		}
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 1)
	assert.Equal(t, 3, courses[0].TotalSessions)
	assert.Equal(t, 2, courses[0].CompletedSessions)
}

func TestFetchCourses_RecordsTotalPositiveButDataEmpty(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 5,
			"recordsFiltered": 5,
			"data": []
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	assert.Empty(t, courses, "should return empty when Warwick returns empty data array even if recordsTotal > 0")
}

func TestFetchCourses_NilDataField(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 0,
			"recordsFiltered": 0
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	assert.Empty(t, courses)
}

func TestFetchCourses_CourseStatusComputation(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 3,
			"recordsFiltered": 3,
			"data": [
				{"ID": "c1", "CourseName": "Active Course", "Cycle": "", "Enrolled": 20, "StartDate": "2020-01-01T00:00:00", "EndDate": "2099-12-31T23:59:59"},
				{"ID": "c2", "CourseName": "Finished Course", "Cycle": "", "Enrolled": 15, "StartDate": "2020-01-01T00:00:00", "EndDate": "2020-06-30T23:59:59"},
				{"ID": "c3", "CourseName": "Upcoming Course", "Cycle": "", "Enrolled": 25, "StartDate": "2099-01-01T00:00:00", "EndDate": "2099-06-30T23:59:59"}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 3)

	assert.Equal(t, domain.CourseStatusActive, courses[0].Status, "Active Course should be active")
	assert.Equal(t, domain.CourseStatusFinished, courses[1].Status, "Finished Course should be finished")
	assert.Equal(t, domain.CourseStatusUpcoming, courses[2].Status, "Upcoming Course should be upcoming")
}

// --- UserID configurability tests ---

func TestEffectiveUserID_DefaultWhenEmpty(t *testing.T) {
	c := &ClassroomClient{}
	assert.Equal(t, defaultUserID, c.effectiveUserID(),
		"should fall back to defaultUserID when userID is empty")
}

func TestEffectiveUserID_UsesConfiguredValue(t *testing.T) {
	c := &ClassroomClient{userID: "custom-user-123"}
	assert.Equal(t, "custom-user-123", c.effectiveUserID(),
		"should use configured userID when set")
}

func TestSetUserID(t *testing.T) {
	c := &ClassroomClient{}
	assert.Equal(t, defaultUserID, c.effectiveUserID())

	c.SetUserID("new-user-id")
	assert.Equal(t, "new-user-id", c.effectiveUserID())
}

func TestFetchCourses_UsesConfiguredUserID(t *testing.T) {
	mc := cache.New()

	var capturedBody string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		if strings.Contains(r.URL.Path, "ClassAttendanceSearch") && !strings.Contains(r.URL.Path, "Detail") {
			capturedBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 1,
			"recordsFiltered": 1,
			"data": [
				{"ID": "c1", "CourseName": "Test Course", "Cycle": "", "Enrolled": 10}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL
	client.SetUserID("my-custom-user-id")

	courses, err := client.GetCourses()
	require.NoError(t, err)
	require.Len(t, courses, 1)
	require.NotEmpty(t, capturedBody, "mock server should have received a courses request body")
	vals, err := url.ParseQuery(capturedBody)
	require.NoError(t, err)
	assert.Equal(t, "my-custom-user-id", vals.Get("UserID"),
		"Warwick request should use the configured UserID")
}

func TestFetchCourses_UsesDefaultUserIDWhenNotConfigured(t *testing.T) {
	mc := cache.New()

	var capturedBody string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		if strings.Contains(r.URL.Path, "ClassAttendanceSearch") && !strings.Contains(r.URL.Path, "Detail") {
			capturedBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 1,
			"recordsFiltered": 1,
			"data": [
				{"ID": "c1", "CourseName": "Test Course", "Cycle": "", "Enrolled": 10}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	_, err = client.GetCourses()
	require.NoError(t, err)
	require.NotEmpty(t, capturedBody, "mock server should have received a courses request body")
	vals, err := url.ParseQuery(capturedBody)
	require.NoError(t, err)
	assert.Equal(t, defaultUserID, vals.Get("UserID"),
		"Warwick request should use defaultUserID when not configured")
}

func TestFetchStudentProfiles_Success(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "UserGroupSearch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"draw": 1,
			"recordsTotal": 2,
			"recordsFiltered": 2,
			"data": [
				{"StudentID": "STU001", "StudentGuid": "guid-a", "FullName": "Alice", "School": "Science", "MobilePhone": "", "ParentPhone": "", "IsActive": true, "TerminateStatus": "", "ExpireDateStr": ""},
				{"StudentID": "STU002", "StudentGuid": "guid-b", "FullName": "Bob", "School": "Math", "MobilePhone": "", "ParentPhone": "", "IsActive": true, "TerminateStatus": "", "ExpireDateStr": ""}
			]
		}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles()
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	assert.Equal(t, "STU001", profiles[0].StudentID)
	assert.Equal(t, "Alice", profiles[0].FullName)
	assert.Equal(t, "STU002", profiles[1].StudentID)
	assert.Equal(t, "Bob", profiles[1].FullName)
	assert.Equal(t, "guid-a", profiles[0].StudentGuid)
	assert.Equal(t, "Science", profiles[0].School)
}

func TestFetchStudentProfiles_Empty(t *testing.T) {
	mc := cache.New()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles()
	require.NoError(t, err)
	assert.Empty(t, profiles)
}
