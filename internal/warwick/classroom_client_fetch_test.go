package warwick

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestFetchCourses_Success(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	assert.Empty(t, courses)
}

func TestFetchCourses_WarwickReturnsLoginPage(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	_, err = client.GetCourses(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAuthExpired)
}

func TestFetchCourses_EnrichmentPopulatesSessionCounts(t *testing.T) {

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
					{"ID": "c1", "CourseName": "Math 101", "Cycle": "", "Enrolled": 30, "StartDate": "2020-05-27T09:00:00", "EndDate": "2099-07-03T17:00:00"}
				]
			}`))
		}
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	require.Len(t, courses, 1)
	assert.Equal(t, 3, courses[0].TotalSessions)
	assert.Equal(t, 2, courses[0].CompletedSessions)
}

func TestFetchCourses_RecordsTotalPositiveButDataEmpty(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	assert.Empty(t, courses, "should return empty when Warwick returns empty data array even if recordsTotal > 0")
}

func TestFetchCourses_NilDataField(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	assert.Empty(t, courses)
}

func TestGetCourses_AlwaysFetchesUpstream(t *testing.T) {
	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.Header().Set("Content-Type", "application/json")
		courseName := "Course A"
		if apiCalls == 2 {
			courseName = "Course B"
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"` + courseName + `","Cycle":"","Enrolled":10,"StartDate":"2020-01-01T00:00:00","EndDate":"2020-06-30T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	first, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	second, err := client.GetCourses(context.Background())
	require.NoError(t, err)

	require.Equal(t, "Course A", first[0].Name)
	require.Equal(t, "Course B", second[0].Name)
	require.Equal(t, 2, apiCalls)
}

func TestGetCourseDetail_AlwaysFetchesUpstream(t *testing.T) {
	detailCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math"}]}`))
			return
		}
		detailCalls++
		name := "Week 1"
		if detailCalls == 2 {
			name = "Week 2"
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"dID":"s1","dName":"` + name + `","dStatus":"Finished"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	first, err := client.GetCourseDetail(context.Background(), "c1")
	require.NoError(t, err)
	second, err := client.GetCourseDetail(context.Background(), "c1")
	require.NoError(t, err)

	require.Equal(t, "Week 1", first.Sessions[0].Name)
	require.Equal(t, "Week 2", second.Sessions[0].Name)
	require.Equal(t, 2, detailCalls)
}

func TestGetSessionDetail_AlwaysFetchesUpstream(t *testing.T) {
	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.Header().Set("Content-Type", "application/json")
		name := "Alice"
		if apiCalls == 2 {
			name = "Bob"
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentName":"` + name + `","StudentNickName":"","StudentSchool":"Science","StudentCheckIn":true,"StudentPPoint":0}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	first, err := client.GetSessionDetail(context.Background(), "c1", "s1")
	require.NoError(t, err)
	second, err := client.GetSessionDetail(context.Background(), "c1", "s1")
	require.NoError(t, err)

	require.Equal(t, "Alice", first.Students[0].Name)
	require.Equal(t, "Bob", second.Students[0].Name)
	require.Equal(t, 2, apiCalls)
}

func TestLiveReadError_DoesNotReturnPreviousPayload(t *testing.T) {
	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if apiCalls == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Course A","Cycle":"","Enrolled":10,"StartDate":"2020-01-01T00:00:00","EndDate":"2020-06-30T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	_, err = client.GetCourses(context.Background())
	require.NoError(t, err)
	second, err := client.GetCourses(context.Background())
	require.Error(t, err)
	require.Nil(t, second)
	require.Equal(t, 2, apiCalls)
}

func TestFetchCourses_CourseStatusComputation(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	courses, err := client.GetCourses(context.Background())
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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("my-custom-user-id")

	courses, err := client.GetCourses(context.Background())
	require.NoError(t, err)
	require.Len(t, courses, 1)
	require.NotEmpty(t, capturedBody, "mock server should have received a courses request body")
	vals, err := url.ParseQuery(capturedBody)
	require.NoError(t, err)
	assert.Equal(t, "my-custom-user-id", vals.Get("UserID"),
		"Warwick request should use the configured UserID")
}

func TestFetchCourses_UsesDefaultUserIDWhenNotConfigured(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	_, err = client.GetCourses(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, capturedBody, "mock server should have received a courses request body")
	vals, err := url.ParseQuery(capturedBody)
	require.NoError(t, err)
	assert.Equal(t, defaultUserID, vals.Get("UserID"),
		"Warwick request should use defaultUserID when not configured")
}

func TestCourseCatalog_UserIDDetectionRunsOnceAfterFailure(t *testing.T) {
	var pageCalls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			pageCalls++
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math","Cycle":"","Enrolled":1,"StartDate":"2026-01-01T00:00:00","EndDate":"2099-01-01T00:00:00"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	_, err = client.GetCourseCatalog(context.Background())
	require.NoError(t, err)
	_, err = client.GetCourseCatalog(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 3, pageCalls, "failed UserID detection should not repeat on every catalog read")
}

func TestUserIDDetectionRetriesAfterCancellation(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("d.UserID = '11111111-2222-3333-4444-555555555555'"))
	}))
	t.Cleanup(apiServer.Close)

	client := NewClassroomClient(nil)
	client.baseURL = apiServer.URL

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		client.resolveUserID(ctx, "cookie")
		close(firstDone)
	}()
	<-started
	cancel()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("canceled UserID discovery did not return")
	}

	assert.Equal(t, "11111111-2222-3333-4444-555555555555", client.resolveUserID(context.Background(), "cookie"))
}

type countingRoundTripper struct {
	delegate http.RoundTripper
	calls    atomic.Int32
}

func (transport *countingRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	transport.calls.Add(1)
	return transport.delegate.RoundTrip(request)
}

func TestUserIDDetectionReusesConfiguredTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"d.UserID = '11111111-2222-3333-4444-555555555555'",
		))
	}))
	defer server.Close()
	transport := &countingRoundTripper{
		delegate: server.Client().Transport,
	}
	client := NewClassroomClient(NewWarwickAuth("", "", ""))
	client.SetBaseURL(server.URL)
	client.SetTransport(transport)

	require.Equal(
		t,
		"11111111-2222-3333-4444-555555555555",
		client.detectUserIDFromPage(context.Background(), "fixture-cookie"),
	)
	require.Equal(t, int32(1), transport.calls.Load())
}

func TestUserIDDetectionRejectsCrossOriginRedirectWithoutForwardingCookie(t *testing.T) {
	var redirectedCalls atomic.Int32
	var redirectedCookie atomic.Value
	redirected := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		redirectedCalls.Add(1)
		redirectedCookie.Store(request.Header.Get("Cookie"))
		_, _ = w.Write([]byte(
			"d.UserID = '11111111-2222-3333-4444-555555555555'",
		))
	}))
	defer redirected.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		http.Redirect(w, request, redirected.URL+"/capture", http.StatusFound)
	}))
	defer origin.Close()
	client := NewClassroomClient(NewWarwickAuth("", "", ""))
	client.SetBaseURL(origin.URL)
	client.SetTransport(origin.Client().Transport)

	require.Empty(
		t,
		client.detectUserIDFromPage(context.Background(), "fixture-cookie"),
	)
	require.Zero(t, redirectedCalls.Load())
	require.Nil(t, redirectedCookie.Load())
}

func TestFetchStudentProfiles_Success(t *testing.T) {

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

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles(context.Background())
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
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestFetchStudentProfiles_RejectsUnsafeRecordCount(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":-1,"recordsFiltered":-1,"data":[]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles(context.Background())
	require.Error(t, err)
	assert.Nil(t, profiles)
	var fetchErr *domain.FetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
}

func TestFetchStudentProfiles_Pagination(t *testing.T) {
	var requestCount int
	var requestedStarts []int

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "UserGroupSearch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		r.ParseForm()
		start, _ := strconv.Atoi(r.FormValue("start"))
		requestedStarts = append(requestedStarts, start)
		requestCount++

		w.Header().Set("Content-Type", "application/json")

		// Total is 1200, page size is 500, so we need 3 requests: start=0 (500), start=500 (500), start=1000 (200).
		switch start {
		case 0:
			w.Write([]byte(`{
				"draw": 1,
				"recordsTotal": 1200,
				"recordsFiltered": 1200,
				"data": [
					{"StudentID": "STU001", "StudentGuid": "guid-001", "FullName": "Student 1", "School": "A"},
					{"StudentID": "STU002", "StudentGuid": "guid-002", "FullName": "Student 2", "School": "B"}
				]
			}`))
		case 500:
			w.Write([]byte(`{
				"draw": 1,
				"recordsTotal": 1200,
				"recordsFiltered": 1200,
				"data": [
					{"StudentID": "STU501", "StudentGuid": "guid-501", "FullName": "Student 501", "School": "A"},
					{"StudentID": "STU502", "StudentGuid": "guid-502", "FullName": "Student 502", "School": "B"}
				]
			}`))
		case 1000:
			w.Write([]byte(`{
				"draw": 1,
				"recordsTotal": 1200,
				"recordsFiltered": 1200,
				"data": [
					{"StudentID": "STU1001", "StudentGuid": "guid-1001", "FullName": "Student 1001", "School": "A"}
				]
			}`))
		}
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	profiles, err := client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	assert.Len(t, profiles, 5, "should fetch all 5 profiles across 3 pages")
	assert.Equal(t, "STU001", profiles[0].StudentID)
	assert.Equal(t, "STU002", profiles[1].StudentID)
	assert.Equal(t, "STU501", profiles[2].StudentID)
	assert.Equal(t, "STU502", profiles[3].StudentID)
	assert.Equal(t, "STU1001", profiles[4].StudentID)
	assert.Equal(t, 3, requestCount, "should make exactly 3 requests")
	assert.Equal(t, []int{0, 500, 1000}, requestedStarts, "should request starts 0, 500, 1000")
}

func TestFetchStudentProfiles_AlwaysFetchesUpstream(t *testing.T) {
	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.Header().Set("Content-Type", "application/json")
		name := "Alice"
		if apiCalls == 2 {
			name = "Bob"
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentGuid":"guid-a","FullName":"` + name + `","School":"Science"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	first, err := client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	second, err := client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)

	require.Equal(t, "Alice", first[0].FullName)
	require.Equal(t, "Bob", second[0].FullName)
	require.Equal(t, 2, apiCalls)
}

func newTestLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server
}
