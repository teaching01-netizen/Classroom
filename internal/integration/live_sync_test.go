package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/warwick"
)

type liveFixture struct {
	client       *warwick.ClassroomClient
	courseReads  atomic.Int32
	detailReads  atomic.Int32
	sessionReads atomic.Int32
	profileReads atomic.Int32
	toggleReads  atomic.Int32
	reportState  atomic.Bool
	failCourses  atomic.Bool
	apiServer    *httptest.Server
	loginServer  *httptest.Server
}

func newLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	fixture := &liveFixture{}

	fixture.loginServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=integration-cookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(fixture.loginServer.Close)

	fixture.apiServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "ClassAttendanceSearch") && !strings.Contains(r.URL.Path, "Detail"):
			version := fixture.courseReads.Add(1)
			if fixture.failCourses.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = fmt.Fprintf(w, `{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Course %d","Cycle":"","Enrolled":10,"StartDate":"2020-01-01T00:00:00","EndDate":"2020-01-02T00:00:00"}]}`, version)
		case strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch"):
			version := fixture.detailReads.Add(1)
			_, _ = fmt.Fprintf(w, `{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"dID":"s1","dName":"Session %d","dStatus":"Finished"}]}`, version)
		case strings.Contains(r.URL.Path, "ClassAttendanceStudentCheckInSearch"):
			version := fixture.sessionReads.Add(1)
			checkedIn := fixture.reportState.Load()
			name := fmt.Sprintf("Student %d", version)
			_, _ = fmt.Fprintf(w, `{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentName":"%s","StudentNickName":"","StudentSchool":"Science","StudentCheckIn":%t,"StudentPPoint":0}]}`, name, checkedIn)
		case strings.Contains(r.URL.Path, "UserGroupSearch"):
			version := fixture.profileReads.Add(1)
			_, _ = fmt.Fprintf(w, `{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentGuid":"guid-a","FullName":"Profile %d","School":"Science"}]}`, version)
		case strings.Contains(r.URL.Path, "ToggleCheckin"):
			fixture.toggleReads.Add(1)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fixture.apiServer.Close)

	pool, err := warwick.NewSessionPool("test@test.com", "pass", fixture.loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	fixture.client = warwick.NewClassroomClientFromPool(pool, warwick.TierTeacher)
	fixture.client.SetBaseURL(fixture.apiServer.URL)
	fixture.client.SetUserID("integration-user")
	return fixture
}

func TestLiveSync_SequentialReadsReturnNewUpstreamVersions(t *testing.T) {
	fixture := newLiveFixture(t)

	courses, err := fixture.client.GetCourses(context.Background())
	require.NoError(t, err)
	coursesAgain, err := fixture.client.GetCourses(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Course 1", courses[0].Name)
	require.Equal(t, "Course 2", coursesAgain[0].Name)
	require.Equal(t, int32(2), fixture.courseReads.Load())

	detail, err := fixture.client.GetCourseDetail(context.Background(), "c1")
	require.NoError(t, err)
	detailAgain, err := fixture.client.GetCourseDetail(context.Background(), "c1")
	require.NoError(t, err)
	require.Equal(t, "Session 1", detail.Sessions[0].Name)
	require.Equal(t, "Session 2", detailAgain.Sessions[0].Name)
	require.Equal(t, int32(2), fixture.detailReads.Load())

	profiles, err := fixture.client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	profilesAgain, err := fixture.client.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Profile 1", profiles[0].FullName)
	require.Equal(t, "Profile 2", profilesAgain[0].FullName)
	require.Equal(t, int32(2), fixture.profileReads.Load())
}

func TestLiveSync_UpstreamErrorNeverReturnsPreviousCoursePayload(t *testing.T) {
	fixture := newLiveFixture(t)

	first, err := fixture.client.GetCourses(context.Background())
	require.NoError(t, err)
	fixture.failCourses.Store(true)
	second, err := fixture.client.GetCourses(context.Background())
	require.Error(t, err)
	require.Nil(t, second)
	require.Equal(t, "Course 1", first[0].Name)
	require.Equal(t, int32(2), fixture.courseReads.Load())
}

func TestLiveSync_ReportRecomputesAfterUpstreamChange(t *testing.T) {
	fixture := newLiveFixture(t)
	sessions := []domain.SessionSummary{{SessionID: "s1", SessionNumber: 1, Status: domain.SessionStatusDone}}

	first, err := fixture.client.GetCourseAttendanceReport(context.Background(), "c1", "Course 1", sessions, 4, fixture.client)
	require.NoError(t, err)
	fixture.reportState.Store(true)
	second, err := fixture.client.GetCourseAttendanceReport(context.Background(), "c1", "Course 1", sessions, 4, fixture.client)
	require.NoError(t, err)

	require.False(t, first.Stale)
	require.False(t, second.Stale)
	require.NotEqual(t, first.Students, second.Students)
	require.Equal(t, int32(2), fixture.sessionReads.Load())
}

func TestLiveSync_ToggleIsFollowedByLiveSessionRead(t *testing.T) {
	fixture := newLiveFixture(t)

	before, err := fixture.client.GetSessionDetail(context.Background(), "c1", "s1")
	require.NoError(t, err)
	require.False(t, before.Students[0].CheckedIn)

	require.NoError(t, fixture.client.ToggleCheckin(context.Background(), "c1", "s1", "STU001", true))
	fixture.reportState.Store(true)
	after, err := fixture.client.GetSessionDetail(context.Background(), "c1", "s1")
	require.NoError(t, err)
	require.True(t, after.Students[0].CheckedIn)
	require.Equal(t, int32(1), fixture.toggleReads.Load())
	require.Equal(t, int32(2), fixture.sessionReads.Load())
}
