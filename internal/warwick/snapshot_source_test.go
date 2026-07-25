package warwick

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func snapshotTestClient(server *httptest.Server, bodyLimit int64) *SnapshotSource {
	auth := NewWarwickAuth("teacher@example.com", "password", server.URL+"/admin/")
	auth.session = &sessionState{
		cookieValue: "test-cookie",
		obtainedAt:  time.Now(),
		expiresAt:   time.Now().Add(time.Hour),
	}
	client := NewClassroomClient(auth)
	client.SetBaseURL(server.URL)
	client.SetTransport(server.Client().Transport)
	client.SetUserID("user-1")
	return NewSnapshotSource(client, bodyLimit)
}

func catalogTarget() domain.ScrapeTarget {
	return domain.ScrapeTarget{
		ID: 1,
		Ref: domain.TargetRef{
			Host:        "warwick.humantix.cloud",
			Kind:        domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
	}
}

func TestSnapshotSourceSendsConditionalHeadersAndReturnsNotModifiedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, `"catalog-v1"`, r.Header.Get("If-None-Match"))
		require.Equal(t, "Sun, 26 Jul 2026 10:00:00 GMT", r.Header.Get("If-Modified-Since"))
		w.Header().Set("ETag", `"catalog-v1"`)
		w.Header().Set("Last-Modified", "Sun, 26 Jul 2026 10:00:00 GMT")
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	source := snapshotTestClient(server, 1<<20)
	target := catalogTarget()
	target.HasCurrentSnapshot = true
	target.Conditional = domain.ConditionalHeaders{
		ETag: `"catalog-v1"`, LastModified: "Sun, 26 Jul 2026 10:00:00 GMT",
	}
	result, err := source.Fetch(context.Background(), target)
	require.ErrorIs(t, err, domain.ErrNotModified)
	require.Equal(t, http.StatusNotModified, result.Metadata.StatusCode)
	require.Equal(t, `"catalog-v1"`, result.Metadata.ETag)
	require.Equal(t, "private, max-age=60", result.Metadata.CacheControl)
	require.Zero(t, result.BytesRead)
}

func TestSnapshotSourceParsesTypedCatalogAndMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{
			"draw":1,
			"recordsTotal":1,
			"recordsFiltered":1,
			"data":[{
				"ID":"course-1",
				"CourseName":"Math",
				"Cycle":"",
				"Enrolled":10,
				"StartDate":"2026-07-01T00:00:00",
				"EndDate":"2026-08-01T00:00:00"
			}]
		}`))
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)

	result, err := source.Fetch(context.Background(), catalogTarget())
	require.NoError(t, err)
	courses, ok := result.Value.([]domain.CourseSummary)
	require.True(t, ok)
	require.Len(t, courses, 1)
	require.Equal(t, "course-1", courses[0].CourseID)
	require.Equal(t, `"v1"`, result.Metadata.ETag)
	require.Positive(t, result.BytesRead)
}

func TestSnapshotSourceParsesRetryAfterAndDoesNotRetryGenericFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		http.Error(w, "do not expose this body", http.StatusTooManyRequests)
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)

	_, err := source.Fetch(context.Background(), catalogTarget())
	var statusErr *domain.UpstreamStatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusTooManyRequests, statusErr.StatusCode)
	require.Equal(t, 2*time.Minute, statusErr.RetryAfter)
	require.Equal(t, int32(1), calls.Load())
	require.NotContains(t, err.Error(), "do not expose")
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	require.Equal(t, 5*time.Minute, parseRetryAfter("Sun, 26 Jul 2026 10:05:00 GMT", now))
	require.Zero(t, parseRetryAfter("invalid", now))
}

func TestSnapshotSourceRejectsUnsupportedContentTypeAndBodyLimit(t *testing.T) {
	t.Run("content type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(`{"data":[]}`))
		}))
		defer server.Close()
		source := snapshotTestClient(server, 1<<20)
		_, err := source.Fetch(context.Background(), catalogTarget())
		var fetchErr *domain.FetchError
		require.ErrorAs(t, err, &fetchErr)
		require.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
	})

	t.Run("body limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", 512) + `"}`))
		}))
		defer server.Close()
		source := snapshotTestClient(server, 64)
		_, err := source.Fetch(context.Background(), catalogTarget())
		var fetchErr *domain.FetchError
		require.ErrorAs(t, err, &fetchErr)
		require.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
	})
}

func TestSnapshotSourceRejectsUnknownKindBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	target := catalogTarget()
	target.Ref.Kind = "unknown"
	_, err := source.Fetch(context.Background(), target)
	require.Error(t, err)
	require.Zero(t, calls.Load())
}

func TestSnapshotSourceProfilesRemainUnconditional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("If-None-Match"))
		require.Empty(t, r.Header.Get("If-Modified-Since"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draw":1,"recordsTotal":1,"recordsFiltered":1,
			"data":[{"StudentID":"u1","StudentGuid":"g1","FullName":"Alice","School":"W"}]
		}`))
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	target := domain.ScrapeTarget{
		ID: 1, HasCurrentSnapshot: true,
		Ref: domain.TargetRef{
			Host: "warwick.humantix.cloud", Kind: domain.SnapshotStudentProfiles,
			ResourceKey: "profiles",
		},
		Conditional: domain.ConditionalHeaders{ETag: `"unsafe"`},
	}
	result, err := source.Fetch(context.Background(), target)
	require.NoError(t, err)
	profiles := result.Value.([]domain.StudentProfile)
	require.Len(t, profiles, 1)
	require.Empty(t, result.Metadata.ETag)
}

func TestSnapshotSourceRenewsAuthenticationOnce(t *testing.T) {
	var loginCalls atomic.Int32
	var fetchCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/" {
			loginCalls.Add(1)
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "fresh-cookie"})
			w.WriteHeader(http.StatusFound)
			return
		}
		fetchCalls.Add(1)
		cookie, _ := r.Cookie(sessionCookieName)
		if cookie == nil || cookie.Value != "fresh-cookie" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`))
	}))
	defer server.Close()

	source := snapshotTestClient(server, 1<<20)
	_, err := source.Fetch(context.Background(), catalogTarget())
	require.NoError(t, err)
	require.Equal(t, int32(1), loginCalls.Load())
	require.Equal(t, int32(2), fetchCalls.Load())
}

func TestSnapshotSourceReturnsTypedServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret body", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	_, err := source.Fetch(context.Background(), catalogTarget())
	var statusErr *domain.UpstreamStatusError
	require.True(t, errors.As(err, &statusErr))
	require.Equal(t, http.StatusServiceUnavailable, statusErr.StatusCode)
	require.NotContains(t, err.Error(), "secret body")
}
