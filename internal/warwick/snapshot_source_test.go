package warwick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type snapshotRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn snapshotRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type trackingResponseBody struct {
	io.Reader
	closed atomic.Bool
}

func (body *trackingResponseBody) Close() error {
	body.closed.Store(true)
	return nil
}

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

func TestSnapshotSourceCourseDetailEmptyStatusIsNotStarted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draw":1,
			"recordsTotal":2,
			"recordsFiltered":2,
			"data":[
				{"dID":"session-1","dName":"Pending","dStatus":""},
				{"dID":"session-2","dName":"Current","dStatus":"Active"}
			]
		}`))
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	target := domain.ScrapeTarget{Ref: domain.TargetRef{
		Host:        "warwick.humantix.cloud",
		Kind:        domain.SnapshotCourseDetail,
		ResourceKey: "course-1",
	}}

	result, err := source.Fetch(context.Background(), target)

	require.NoError(t, err)
	detail, ok := result.Value.(*domain.CourseDetail)
	require.True(t, ok)
	require.Equal(t, domain.CourseStatusActive, detail.Status)
	require.Equal(t, domain.SessionStatusNotStarted, detail.Sessions[0].Status)
	require.Equal(t, domain.SessionStatusActive, detail.Sessions[1].Status)
}

func TestSnapshotSourceRejectsDuplicateIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		target domain.ScrapeTarget
		body   string
	}{
		{
			name:   "course catalog",
			target: catalogTarget(),
			body: `{
				"recordsTotal":2,"recordsFiltered":2,
				"data":[
					{"ID":"course-1","CourseName":"First"},
					{"ID":"course-1","CourseName":"Duplicate"}
				]
			}`,
		},
		{
			name: "course sessions",
			target: domain.ScrapeTarget{Ref: domain.TargetRef{
				Host:        "warwick.humantix.cloud",
				Kind:        domain.SnapshotCourseDetail,
				ResourceKey: "course-1",
			}},
			body: `{
				"recordsTotal":2,"recordsFiltered":2,
				"data":[
					{"dID":"session-1","dName":"First"},
					{"dID":"session-1","dName":"Duplicate"}
				]
			}`,
		},
		{
			name: "session students",
			target: domain.ScrapeTarget{Ref: domain.TargetRef{
				Host:        "warwick.humantix.cloud",
				Kind:        domain.SnapshotSessionDetail,
				ParentKey:   "course-1",
				ResourceKey: "session-1",
			}},
			body: `{
				"recordsTotal":2,"recordsFiltered":2,
				"data":[
					{"StudentID":"student-1","StudentName":"First"},
					{"StudentID":"student-1","StudentName":"Duplicate"}
				]
			}`,
		},
		{
			name: "student profiles",
			target: domain.ScrapeTarget{Ref: domain.TargetRef{
				Host:        "warwick.humantix.cloud",
				Kind:        domain.SnapshotStudentProfiles,
				ResourceKey: "profiles",
			}},
			body: `{
				"recordsTotal":2,"recordsFiltered":2,
				"data":[
					{"StudentID":"W1","StudentGuid":"guid-1","FullName":"First"},
					{"StudentID":"W2","StudentGuid":"guid-1","FullName":"Duplicate"}
				]
			}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				w http.ResponseWriter,
				_ *http.Request,
			) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			source := snapshotTestClient(server, 1<<20)

			_, err := source.Fetch(context.Background(), test.target)

			var fetchErr *domain.FetchError
			require.ErrorAs(t, err, &fetchErr)
			require.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
		})
	}
}

func TestSnapshotSourceSampledTraceRecordsPoolWaitTTFBAndReuseState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"recordsTotal":0,"recordsFiltered":0,"data":[]}`,
		))
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	source.SetHTTPTraceSampleRate(1)
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(
		&logs,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)))
	defer slog.SetDefault(previousLogger)

	_, err := source.Fetch(context.Background(), catalogTarget())

	require.NoError(t, err)
	require.Contains(t, logs.String(), "warwick_scrape_httptrace_pool_wait")
	require.Contains(t, logs.String(), "warwick_scrape_httptrace_connection")
	require.Contains(t, logs.String(), "warwick_scrape_httptrace_ttfb")
	require.Contains(t, logs.String(), "kind=course_catalog")
}

func TestSnapshotSourceRejectsIncompleteSingleResponseRepresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"unsafe-partial"`)
		_, _ = w.Write([]byte(`{
			"draw":1,
			"recordsTotal":501,
			"recordsFiltered":501,
			"data":[{
				"ID":"course-1",
				"CourseName":"Only the first row",
				"Cycle":"",
				"Enrolled":1,
				"StartDate":"2026-07-01T00:00:00",
				"EndDate":"2026-08-01T00:00:00"
			}]
		}`))
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)

	_, err := source.Fetch(context.Background(), catalogTarget())
	var fetchErr *domain.FetchError
	require.ErrorAs(t, err, &fetchErr)
	require.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
	require.NotContains(t, err.Error(), "Only the first row")
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

func TestSnapshotSourceProfilesRequireCompleteConsistentPagination(t *testing.T) {
	t.Run("complete two-page aggregate", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			require.Empty(t, r.Header.Get("If-None-Match"))
			start := r.Form.Get("start")
			count := profilePageSize
			if start == "500" {
				count = 1
			}
			rows := make([]UserGroupRow, count)
			for index := range rows {
				rows[index] = UserGroupRow{
					StudentID:   start + "-" + strconv.Itoa(index),
					StudentGuid: start + "-guid-" + strconv.Itoa(index),
					FullName:    "Student",
					School:      "School",
				}
			}
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(UserGroupSearchResponse{
				Draw:            1,
				RecordsTotal:    501,
				RecordsFiltered: 501,
				Data:            rows,
			}))
		}))
		defer server.Close()
		source := snapshotTestClient(server, 1<<20)
		target := domain.ScrapeTarget{
			ID: 1,
			Ref: domain.TargetRef{
				Host:        "warwick.humantix.cloud",
				Kind:        domain.SnapshotStudentProfiles,
				ResourceKey: "profiles",
			},
		}

		result, err := source.Fetch(context.Background(), target)
		require.NoError(t, err)
		require.Len(t, result.Value.([]domain.StudentProfile), 501)
		require.Equal(t, int32(2), calls.Load())
	})

	t.Run("short non-final page", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"draw":1,
				"recordsTotal":501,
				"recordsFiltered":501,
				"data":[{"StudentID":"one","StudentGuid":"one","FullName":"One","School":"W"}]
			}`))
		}))
		defer server.Close()
		source := snapshotTestClient(server, 1<<20)
		target := domain.ScrapeTarget{
			ID: 1,
			Ref: domain.TargetRef{
				Host:        "warwick.humantix.cloud",
				Kind:        domain.SnapshotStudentProfiles,
				ResourceKey: "profiles",
			},
		}

		_, err := source.Fetch(context.Background(), target)
		var fetchErr *domain.FetchError
		require.ErrorAs(t, err, &fetchErr)
		require.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
	})
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

func TestSnapshotSourceConcurrentAuthRenewalIsCoalescedPerSession(t *testing.T) {
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
		_, _ = w.Write([]byte(
			`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`,
		))
	}))
	defer server.Close()

	pool, err := NewSessionPool(
		"teacher@example.com",
		"password",
		server.URL+"/admin/",
		1,
		1,
		1,
		server.Client().Transport.(*http.Transport),
	)
	require.NoError(t, err)
	teacherSession := pool.sessions[1]
	teacherSession.cookie = "stale-cookie"
	teacherSession.obtainedAt = time.Now().Add(-2 * time.Hour)
	teacherSession.expiresAt = time.Now().Add(time.Hour)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.SetBaseURL(server.URL)
	client.SetTransport(server.Client().Transport)
	client.SetUserID("user-1")
	source := NewSnapshotSource(client, 1<<20)

	start := make(chan struct{})
	results := make(chan error, 2)
	var callers sync.WaitGroup
	for range 2 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			_, fetchErr := source.Fetch(context.Background(), catalogTarget())
			results <- fetchErr
		}()
	}
	close(start)
	callers.Wait()
	close(results)
	for fetchErr := range results {
		require.NoError(t, fetchErr)
	}
	require.Equal(t, int32(1), loginCalls.Load())
	require.Equal(t, int32(3), fetchCalls.Load(),
		"one stale request plus two successful reads are expected")
}

func TestSnapshotSourceClosesResponseBodiesOnEveryPath(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		bodyLimit   int64
	}{
		{"not modified", http.StatusNotModified, "", "", 1 << 20},
		{"authentication", http.StatusUnauthorized, "text/plain", "ignored", 1 << 20},
		{"upstream error", http.StatusBadGateway, "text/plain", "ignored", 1 << 20},
		{"content type", http.StatusOK, "text/plain", `{}`, 1 << 20},
		{"body limit", http.StatusOK, "application/json", strings.Repeat("x", 32), 8},
		{"decode", http.StatusOK, "application/json", `{`, 1 << 20},
		{
			"success",
			http.StatusOK,
			"application/json",
			`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`,
			1 << 20,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingResponseBody{Reader: strings.NewReader(test.body)}
			server := httptest.NewServer(http.NotFoundHandler())
			defer server.Close()
			source := snapshotTestClient(server, test.bodyLimit)
			source.client.SetTransport(snapshotRoundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				if test.contentType != "" {
					header.Set("Content-Type", test.contentType)
				}
				return &http.Response{
					StatusCode: test.status,
					Header:     header,
					Body:       body,
				}, nil
			}))

			_, _ = source.Fetch(context.Background(), catalogTarget())
			require.True(t, body.closed.Load(), "response body must always close")
		})
	}
}

func TestSnapshotSourceDoesNotRetryNetworkOrStatusFailures(t *testing.T) {
	t.Run("network", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()
		source := snapshotTestClient(server, 1<<20)
		source.client.SetTransport(snapshotRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("fixture network failure")
		}))

		_, err := source.Fetch(context.Background(), catalogTarget())
		require.Error(t, err)
		require.Equal(t, int32(1), calls.Load())
	})

	for _, status := range []int{
		http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusNotFound,
		http.StatusGone,
		http.StatusUnprocessableEntity,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			source := snapshotTestClient(server, 1<<20)

			_, err := source.Fetch(context.Background(), catalogTarget())
			var statusErr *domain.UpstreamStatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, status, statusErr.StatusCode)
			require.Equal(t, int32(1), calls.Load())
		})
	}
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
