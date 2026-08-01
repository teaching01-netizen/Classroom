package warwick

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"mime"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

const (
	profilePageSize        = 500
	profilePageConcurrency = 4
	maxProfileRecords      = 100_000
	errorBodyDrainLimit    = 4 << 10
	sessionAcquireBudget   = 5 * time.Second
	// roomSyncAcquireBudget bounds the session-pool wait for the active-room
	// check-in sync loop. It shares the teacher tier with the scraper and
	// toggles, so it must not hold a slot for long; on exhaustion the loop
	// simply skips the cycle.
	roomSyncAcquireBudget = time.Second
)

type ResponseMetadata struct {
	StatusCode   int
	ETag         string
	LastModified string
	CacheControl string
	RetryAfter   string
	ContentType  string
	RawBodyHash  string
}

type SnapshotFetchResult struct {
	Value     any
	Metadata  ResponseMetadata
	BytesRead int64

	// Pagination evidence used to build the completeness manifest.
	ReportedCount int
	ExpectedPages int
	FetchedPages  int
}

type SnapshotSource struct {
	client            *ClassroomClient
	responseBodyLimit int64
	responseGuard     *ResponseGuard
	now               func() time.Time
	traceSampleRate   float64
}

func (s *SnapshotSource) SetHTTPTraceSampleRate(rate float64) {
	if rate < 0 || rate > 1 {
		panic("SnapshotSource: HTTP trace sample rate must be between 0 and 1")
	}
	s.traceSampleRate = rate
}

func NewSnapshotSource(client *ClassroomClient, responseBodyLimit int64) *SnapshotSource {
	if client == nil {
		panic("SnapshotSource: client must not be nil")
	}
	if responseBodyLimit <= 0 {
		panic("SnapshotSource: response body limit must be positive")
	}
	return &SnapshotSource{
		client:            client,
		responseBodyLimit: responseBodyLimit,
		responseGuard:     NewResponseGuard(responseBodyLimit),
		now:               time.Now,
	}
}

func (s *SnapshotSource) Fetch(
	ctx context.Context,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	if !target.Ref.Kind.Valid() {
		return SnapshotFetchResult{}, fmt.Errorf("snapshot source: unknown kind %q", target.Ref.Kind)
	}
	ctx = s.withSampledHTTPTrace(ctx, target.Ref.Kind)
	if s.client.pool != nil {
		return s.fetchWithPool(ctx, target)
	}
	if s.client.auth == nil {
		return SnapshotFetchResult{}, errors.New("snapshot source: classroom client has no authentication source")
	}
	return s.fetchWithAuth(ctx, target)
}

// FetchConfirmation is an independent, unconditional fetch used to confirm a
// suspicious candidate. It never sends conditional headers, so a 304 cannot
// mask a changed dataset.
func (s *SnapshotSource) FetchConfirmation(
	ctx context.Context,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	target.Conditional = domain.ConditionalHeaders{}
	return s.Fetch(ctx, target)
}

func (s *SnapshotSource) withSampledHTTPTrace(
	ctx context.Context,
	kind domain.SnapshotKind,
) context.Context {
	if s.traceSampleRate <= 0 || rand.Float64() >= s.traceSampleRate {
		return ctx
	}
	startedAt := time.Now()
	var dnsStarted time.Time
	var connectStarted time.Time
	var tlsStarted time.Time
	var getConnStarted time.Time
	var requestWrittenAt time.Time
	var timingMu sync.Mutex
	trace := &httptrace.ClientTrace{
		GetConn: func(string) {
			timingMu.Lock()
			getConnStarted = time.Now()
			timingMu.Unlock()
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			timingMu.Lock()
			dnsStarted = time.Now()
			timingMu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			timingMu.Lock()
			started := dnsStarted
			timingMu.Unlock()
			if !started.IsZero() {
				slog.Debug(
					"warwick_scrape_httptrace_dns",
					"kind", kind,
					"duration", time.Since(started),
				)
			}
		},
		ConnectStart: func(_, _ string) {
			timingMu.Lock()
			connectStarted = time.Now()
			timingMu.Unlock()
		},
		ConnectDone: func(_, _ string, _ error) {
			timingMu.Lock()
			started := connectStarted
			timingMu.Unlock()
			if !started.IsZero() {
				slog.Debug(
					"warwick_scrape_httptrace_connect",
					"kind", kind,
					"duration", time.Since(started),
				)
			}
		},
		TLSHandshakeStart: func() {
			timingMu.Lock()
			tlsStarted = time.Now()
			timingMu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			timingMu.Lock()
			started := tlsStarted
			timingMu.Unlock()
			if !started.IsZero() {
				slog.Debug(
					"warwick_scrape_httptrace_tls",
					"kind", kind,
					"duration", time.Since(started),
				)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			timingMu.Lock()
			waitStarted := getConnStarted
			timingMu.Unlock()
			if !waitStarted.IsZero() {
				slog.Debug(
					"warwick_scrape_httptrace_pool_wait",
					"kind", kind,
					"duration", time.Since(waitStarted),
				)
			}
			slog.Debug(
				"warwick_scrape_httptrace_connection",
				"kind", kind,
				"reused", info.Reused,
				"was_idle", info.WasIdle,
			)
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			timingMu.Lock()
			requestWrittenAt = time.Now()
			timingMu.Unlock()
		},
		GotFirstResponseByte: func() {
			timingMu.Lock()
			writtenAt := requestWrittenAt
			timingMu.Unlock()
			if writtenAt.IsZero() {
				writtenAt = startedAt
			}
			slog.Debug(
				"warwick_scrape_httptrace_ttfb",
				"kind", kind,
				"duration", time.Since(writtenAt),
			)
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

func (s *SnapshotSource) fetchWithPool(
	ctx context.Context,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	return s.fetchWithPoolBudget(ctx, target, sessionAcquireBudget)
}

func (s *SnapshotSource) fetchWithPoolBudget(
	ctx context.Context,
	target domain.ScrapeTarget,
	acquireBudget time.Duration,
) (SnapshotFetchResult, error) {
	ref, err := s.client.pool.AcquireWithTimeoutContext(ctx, s.client.tier, acquireBudget)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return SnapshotFetchResult{}, domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return SnapshotFetchResult{}, domain.ErrPoolExhausted
		}
		if ctx.Err() != nil {
			return SnapshotFetchResult{}, ctx.Err()
		}
		return SnapshotFetchResult{}, domain.ErrAuthExpired
	}
	defer s.client.pool.Release(ref)

	var lastResult SnapshotFetchResult
	for attempt := 0; attempt < 2; attempt++ {
		result, fetchErr := s.fetchAuthenticated(ctx, ref.Cookie, target)
		lastResult = result
		if !errors.Is(fetchErr, domain.ErrAuthExpired) {
			return result, fetchErr
		}
		if attempt == 1 {
			return result, domain.ErrAuthExpired
		}
		if _, _, refreshErr := s.client.pool.ForceRefreshOnSessionContext(ctx, ref); refreshErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if errors.Is(refreshErr, ErrAuthConflict) {
				return result, domain.ErrAuthConflict
			}
			return result, domain.ErrAuthExpired
		}
	}
	return lastResult, domain.ErrAuthExpired
}

func (s *SnapshotSource) fetchWithAuth(
	ctx context.Context,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	cookie, _, err := s.client.auth.GetValidSessionContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return SnapshotFetchResult{}, ctx.Err()
		}
		return SnapshotFetchResult{}, domain.ErrAuthExpired
	}
	var lastResult SnapshotFetchResult
	for attempt := 0; attempt < 2; attempt++ {
		result, fetchErr := s.fetchAuthenticated(ctx, cookie, target)
		lastResult = result
		if !errors.Is(fetchErr, domain.ErrAuthExpired) {
			return result, fetchErr
		}
		if attempt == 1 {
			return result, domain.ErrAuthExpired
		}
		s.client.auth.sessionMu.Lock()
		if s.client.auth.session != nil && s.client.auth.session.cookieValue == cookie {
			s.client.auth.session = nil
		}
		s.client.auth.sessionMu.Unlock()
		cookie, _, err = s.client.auth.ForceRefreshContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, domain.ErrAuthExpired
		}
	}
	return lastResult, domain.ErrAuthExpired
}

func (s *SnapshotSource) fetchAuthenticated(
	ctx context.Context,
	cookie string,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	switch target.Ref.Kind {
	case domain.SnapshotCourseCatalog:
		return s.fetchCatalog(ctx, cookie, target)
	case domain.SnapshotCourseDetail:
		return s.fetchCourse(ctx, cookie, target)
	case domain.SnapshotSessionDetail:
		return s.fetchSession(ctx, cookie, target)
	case domain.SnapshotStudentProfiles:
		return s.fetchProfiles(ctx, cookie)
	default:
		return SnapshotFetchResult{}, fmt.Errorf("snapshot source: unknown kind %q", target.Ref.Kind)
	}
}

func (s *SnapshotSource) conditionalHeaders(target domain.ScrapeTarget) http.Header {
	if !target.HasCurrentSnapshot {
		return nil
	}
	headers := make(http.Header)
	if target.Conditional.ETag != "" {
		headers.Set("If-None-Match", target.Conditional.ETag)
	}
	if target.Conditional.LastModified != "" {
		headers.Set("If-Modified-Since", target.Conditional.LastModified)
	}
	return headers
}

func (s *SnapshotSource) fetchCatalog(
	ctx context.Context,
	cookie string,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	body := EncodeDataTablesBody(
		DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}),
		map[string]string{"keyword": "", "UserID": s.client.resolveUserID(ctx, cookie)},
	)
	var response ClassAttendanceSearchResponse
	metadata, bytesRead, err := s.requestJSON(
		ctx,
		cookie,
		"/admin/api/ClassAttendanceSearch",
		body,
		s.conditionalHeaders(target),
		&response,
	)
	result := SnapshotFetchResult{
		Metadata:      metadata,
		BytesRead:     bytesRead,
		ReportedCount: response.RecordsFiltered,
		ExpectedPages: 1,
		FetchedPages:  1,
	}
	if err != nil {
		return result, err
	}
	if err := validateCompleteDataTable(
		"ClassAttendanceSearch",
		response.RecordsTotal,
		response.RecordsFiltered,
		len(response.Data),
	); err != nil {
		return result, err
	}
	courses := make([]domain.CourseSummary, 0, len(response.Data))
	seenCourseIDs := make(map[string]struct{}, len(response.Data))
	for _, row := range response.Data {
		courseID, err := requiredDataTableIdentifier(
			row.ID,
			"ClassAttendanceSearch",
			"course",
		)
		if err != nil {
			return result, err
		}
		if err := recordUniqueIdentifier(
			seenCourseIDs,
			courseID,
			"ClassAttendanceSearch",
			"course",
		); err != nil {
			return result, err
		}
		startDate := normalizeWarwickDate(row.StartDate)
		endDate := normalizeWarwickDate(row.EndDate)
		enrolled := parseIntValue(row.Enrolled)
		status, statusErr := domain.GetCourseStatus(startDate, endDate)
		if statusErr != nil {
			status = domain.CourseStatusActive
		}
		courses = append(courses, domain.CourseSummary{
			CourseID:      courseID,
			Name:          row.CourseName,
			StartDate:     startDate,
			EndDate:       endDate,
			EnrolledCount: enrolled,
			Status:        status,
		})
	}
	result.Value = courses
	return result, nil
}

func normalizeWarwickDate(value any) string {
	text, ok := value.(string)
	if !ok || text == "" {
		return ""
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", text)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

func parseIntValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func (s *SnapshotSource) fetchCourse(
	ctx context.Context,
	cookie string,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	body := EncodeDataTablesBody(
		DefaultDataTablesRequest([]string{"dName", "dStatus"}),
		map[string]string{"keyword": "", "CouseID": target.Ref.ResourceKey},
	)
	var response ClassAttendanceDetailResponse
	metadata, bytesRead, err := s.requestJSON(
		ctx,
		cookie,
		"/admin/api/ClassAttendanceDetailSearch",
		body,
		s.conditionalHeaders(target),
		&response,
	)
	result := SnapshotFetchResult{
		Metadata:      metadata,
		BytesRead:     bytesRead,
		ReportedCount: response.RecordsFiltered,
		ExpectedPages: 1,
		FetchedPages:  1,
	}
	if err != nil {
		return result, err
	}
	if err := validateCompleteDataTable(
		"ClassAttendanceDetailSearch",
		response.RecordsTotal,
		response.RecordsFiltered,
		len(response.Data),
	); err != nil {
		return result, err
	}
	var attributes struct {
		CourseName string `json:"course_name"`
	}
	if len(target.Attributes) > 0 {
		_ = json.Unmarshal(target.Attributes, &attributes)
	}
	sessions := make([]domain.SessionSummary, 0, len(response.Data))
	seenSessionIDs := make(map[string]struct{}, len(response.Data))
	completed := 0
	for index, row := range response.Data {
		sessionID, err := requiredDataTableIdentifier(
			row.DID,
			"ClassAttendanceDetailSearch",
			"session",
		)
		if err != nil {
			return result, err
		}
		if err := recordUniqueIdentifier(
			seenSessionIDs,
			sessionID,
			"ClassAttendanceDetailSearch",
			"session",
		); err != nil {
			return result, err
		}
		status := sessionStatusFromWarwick(row.DStatus)
		if status == domain.SessionStatusDone {
			completed++
		}
		sessions = append(sessions, domain.SessionSummary{
			SessionID:     sessionID,
			SessionNumber: index + 1,
			Name:          row.DName,
			Status:        status,
		})
	}
	result.Value = &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID:          target.Ref.ResourceKey,
			Name:              attributes.CourseName,
			TotalSessions:     len(sessions),
			CompletedSessions: completed,
			Status:            domain.CourseStatusActive,
		},
		Sessions: sessions,
	}
	return result, nil
}

func (s *SnapshotSource) fetchSession(
	ctx context.Context,
	cookie string,
	target domain.ScrapeTarget,
) (SnapshotFetchResult, error) {
	body := EncodeDataTablesBody(
		DefaultDataTablesRequest([]string{
			"StudentImg",
			"StudentName",
			"StudentNickName",
			"StudentSchool",
			"StudentCheckIn",
			"StudentPPoint",
			"StudentGivePoint",
		}),
		map[string]string{"keyword": "", "CourseCampaignID": target.Ref.ResourceKey},
	)
	var response StudentCheckInSearchResponse
	metadata, bytesRead, err := s.requestJSON(
		ctx,
		cookie,
		"/admin/api/ClassAttendanceStudentCheckInSearch",
		body,
		s.conditionalHeaders(target),
		&response,
	)
	result := SnapshotFetchResult{
		Metadata:      metadata,
		BytesRead:     bytesRead,
		ReportedCount: response.RecordsFiltered,
		ExpectedPages: 1,
		FetchedPages:  1,
	}
	if err != nil {
		return result, err
	}
	if err := validateCompleteDataTable(
		"ClassAttendanceStudentCheckInSearch",
		response.RecordsTotal,
		response.RecordsFiltered,
		len(response.Data),
	); err != nil {
		return result, err
	}
	students := make([]domain.StudentCheckin, 0, len(response.Data))
	seenStudentIDs := make(map[string]struct{}, len(response.Data))
	checkedIn := 0
	for _, row := range response.Data {
		studentID := strings.TrimSpace(row.StudentID)
		if err := recordUniqueIdentifier(
			seenStudentIDs,
			studentID,
			"ClassAttendanceStudentCheckInSearch",
			"student",
		); err != nil {
			return result, err
		}
		if row.StudentCheckIn {
			checkedIn++
		}
		students = append(students, domain.StudentCheckin{
			StudentID:           studentID,
			Name:                row.StudentName,
			Nickname:            row.StudentNickName,
			School:              row.StudentSchool,
			AvatarURL:           row.StudentImg,
			CheckedIn:           row.StudentCheckIn,
			ParticipationPoints: row.StudentPPoint,
		})
	}
	result.Value = &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:      target.Ref.ResourceKey,
			TotalStudents:  len(students),
			CheckedInCount: checkedIn,
		},
		Students: students,
	}
	return result, nil
}

// FetchSessionCheckins returns the current per-student check-in state for a
// session, fetched unconditionally (no conditional headers) so the active-room
// sync loop can diff it against the previous observation. It uses the short
// room-sync acquire budget so a contended pool skips rather than queues.
func (s *SnapshotSource) FetchSessionCheckins(
	ctx context.Context,
	courseID, sessionID string,
) ([]domain.StudentCheckin, error) {
	if sessionID == "" {
		return nil, errors.New("snapshot source: session id is required")
	}
	if s.client.pool == nil {
		return nil, errors.New("snapshot source: session check-ins require a session pool")
	}
	target := domain.ScrapeTarget{
		Ref: domain.TargetRef{
			Kind:        domain.SnapshotSessionDetail,
			ResourceKey: sessionID,
			ParentKey:   courseID,
		},
	}
	result, err := s.fetchWithPoolBudget(ctx, target, roomSyncAcquireBudget)
	if err != nil {
		return nil, err
	}
	detail, ok := result.Value.(*domain.SessionDetail)
	if !ok || detail == nil {
		return nil, domain.NewInvalidPayloadError("snapshot source: unexpected session check-in payload")
	}
	return detail.Students, nil
}


func (s *SnapshotSource) fetchProfiles(
	ctx context.Context,
	cookie string,
) (SnapshotFetchResult, error) {
	fetchPage := func(callCtx context.Context, start int) (UserGroupSearchResponse, ResponseMetadata, int64, error) {
		request := DefaultDataTablesRequest([]string{"StudentID", "StudentGuid", "FullName", "School"})
		request.Start = start
		request.Length = profilePageSize
		body := EncodeDataTablesBody(request, map[string]string{"keyword": "", "IsActive": ""})
		var response UserGroupSearchResponse
		metadata, bytesRead, err := s.requestJSON(
			callCtx,
			cookie,
			"/admin/api/UserGroupSearch",
			body,
			nil,
			&response,
		)
		return response, metadata, bytesRead, err
	}

	profiles := make([]domain.StudentProfile, 0)
	seenStudentIDs := make(map[string]struct{})
	seenStudentGUIDs := make(map[string]struct{})
	var totalBytes int64
	var firstMetadata ResponseMetadata

	// Page 0 also reports the filtered total that sizes the rest of the set.
	page, metadata, bytesRead, err := fetchPage(ctx, 0)
	totalBytes += bytesRead
	firstMetadata = metadata
	firstMetadata.ETag = ""
	firstMetadata.LastModified = ""
	firstMetadata.CacheControl = ""
	if err != nil {
		return SnapshotFetchResult{Metadata: metadata, BytesRead: totalBytes}, err
	}
	if err := validateProfileRecordCounts(page); err != nil {
		return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
	}
	total := page.RecordsFiltered
	expectedPages := (total + profilePageSize - 1) / profilePageSize
	firstPageFingerprint := profilePageFingerprint(page.Data)
	if err := validateProfilePageRows(page, total, 0); err != nil {
		return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
	}
	profiles, err = mergeProfileRows(profiles, seenStudentIDs, seenStudentGUIDs, page.Data)
	if err != nil {
		return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
	}
	fetchedPages := 1

	if expectedPages > 1 {
		// The rest of the set runs concurrently, then page 0 is refetched to
		// prove the pagination set did not shift underneath us.
		remaining := expectedPages - 1
		fetchCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		starts := make(chan int, remaining)
		for pageIndex := 1; pageIndex < expectedPages; pageIndex++ {
			starts <- pageIndex * profilePageSize
		}
		close(starts)

		type pageResult struct {
			page     UserGroupSearchResponse
			metadata ResponseMetadata
			bytes    int64
		}
		results := make([]pageResult, remaining)
		var resultMu sync.Mutex
		var firstErr error
		var firstErrMetadata ResponseMetadata
		var pages sync.WaitGroup
		workers := profilePageConcurrency
		if workers > remaining {
			workers = remaining
		}
		for range workers {
			pages.Add(1)
			go func() {
				defer pages.Done()
				for start := range starts {
					if fetchCtx.Err() != nil {
						return
					}
					page, metadata, bytesRead, err := fetchPage(fetchCtx, start)
					resultMu.Lock()
					results[start/profilePageSize-1] = pageResult{
						page:     page,
						metadata: metadata,
						bytes:    bytesRead,
					}
					if firstErr == nil && err != nil {
						firstErr = err
						firstErrMetadata = metadata
						cancel()
					}
					resultMu.Unlock()
				}
			}()
		}
		pages.Wait()
		for _, result := range results {
			totalBytes += result.bytes
		}
		if firstErr != nil {
			return SnapshotFetchResult{Metadata: firstErrMetadata, BytesRead: totalBytes}, firstErr
		}
		for index, result := range results {
			if err := validateProfileRecordCounts(result.page); err != nil {
				return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
			}
			if result.page.RecordsFiltered != total {
				return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
					domain.NewInvalidPayloadError("UserGroupSearch record count changed during pagination")
			}
			start := (index + 1) * profilePageSize
			if err := validateProfilePageRows(result.page, total, start); err != nil {
				return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
			}
			profiles, err = mergeProfileRows(profiles, seenStudentIDs, seenStudentGUIDs, result.page.Data)
			if err != nil {
				return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
			}
		}
		fetchedPages += remaining

		// Refetch page 0: identical total plus an identical first-page
		// fingerprint prove the set was stable across the parallel window.
		refetch, refetchMetadata, refetchBytes, err := fetchPage(ctx, 0)
		totalBytes += refetchBytes
		if err != nil {
			return SnapshotFetchResult{Metadata: refetchMetadata, BytesRead: totalBytes}, err
		}
		if err := validateProfileRecordCounts(refetch); err != nil {
			return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes}, err
		}
		if refetch.RecordsFiltered != total ||
			len(refetch.Data) != expectedProfilePageRows(total, 0) ||
			profilePageFingerprint(refetch.Data) != firstPageFingerprint {
			return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
				domain.NewInvalidPayloadError("UserGroupSearch pagination set was unstable")
		}
		fetchedPages++
	}

	if len(profiles) != total {
		return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
			domain.NewInvalidPayloadError("UserGroupSearch parsed count differs from reported count")
	}
	return SnapshotFetchResult{
		Value:         profiles,
		Metadata:      firstMetadata,
		BytesRead:     totalBytes,
		ReportedCount: total,
		ExpectedPages: expectedPages,
		FetchedPages:  fetchedPages,
	}, nil
}

func expectedProfilePageRows(total int, start int) int {
	expectedRows := total - start
	if expectedRows > profilePageSize {
		expectedRows = profilePageSize
	}
	return expectedRows
}

func validateProfileRecordCounts(response UserGroupSearchResponse) error {
	if response.RecordsTotal < 0 ||
		response.RecordsFiltered < 0 ||
		response.RecordsFiltered > response.RecordsTotal ||
		response.RecordsFiltered > maxProfileRecords {
		return domain.NewInvalidPayloadError("UserGroupSearch record count outside supported range")
	}
	return nil
}

func validateProfilePageRows(response UserGroupSearchResponse, total int, start int) error {
	if expected := expectedProfilePageRows(total, start); expected < 0 || len(response.Data) != expected {
		return domain.NewInvalidPayloadError("UserGroupSearch returned an incomplete page")
	}
	return nil
}

// mergeProfileRows appends rows to profiles while enforcing the stable-key
// uniqueness contract (StudentID and StudentGuid) across the whole set.
func mergeProfileRows(
	profiles []domain.StudentProfile,
	seenStudentIDs map[string]struct{},
	seenStudentGUIDs map[string]struct{},
	rows []UserGroupRow,
) ([]domain.StudentProfile, error) {
	for _, row := range rows {
		studentID := strings.TrimSpace(row.StudentID)
		if err := recordUniqueIdentifier(
			seenStudentIDs,
			studentID,
			"UserGroupSearch",
			"student",
		); err != nil {
			return profiles, err
		}
		studentGUID := strings.TrimSpace(row.StudentGuid)
		if err := recordUniqueIdentifier(
			seenStudentGUIDs,
			studentGUID,
			"UserGroupSearch",
			"student GUID",
		); err != nil {
			return profiles, err
		}
		profiles = append(profiles, domain.StudentProfile{
			StudentID:   studentID,
			StudentGuid: studentGUID,
			FullName:    row.FullName,
			School:      row.School,
		})
	}
	return profiles, nil
}

// profilePageFingerprint hashes the page's identity pairs so a refetch of
// page 0 can prove the same rows came back, independent of row ordering.
func profilePageFingerprint(rows []UserGroupRow) string {
	identities := make([]string, 0, len(rows))
	for _, row := range rows {
		identities = append(identities,
			strings.TrimSpace(row.StudentID)+"|"+strings.TrimSpace(row.StudentGuid))
	}
	sort.Strings(identities)
	sum := sha256.Sum256([]byte(strings.Join(identities, "\n")))
	return hex.EncodeToString(sum[:])
}

func requiredDataTableIdentifier(value any, endpoint string, field string) (string, error) {
	var identifier string
	switch typed := value.(type) {
	case string:
		identifier = strings.TrimSpace(typed)
	case float64:
		if math.IsNaN(typed) ||
			math.IsInf(typed, 0) ||
			typed != math.Trunc(typed) {
			return "", domain.NewInvalidPayloadError(
				endpoint + " returned an invalid " + field + " identifier",
			)
		}
		identifier = strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return "", domain.NewInvalidPayloadError(
			endpoint + " returned an invalid " + field + " identifier",
		)
	}
	if identifier == "" {
		return "", domain.NewInvalidPayloadError(
			endpoint + " returned an empty " + field + " identifier",
		)
	}
	return identifier, nil
}

func recordUniqueIdentifier(
	seen map[string]struct{},
	identifier string,
	endpoint string,
	field string,
) error {
	if identifier == "" {
		return domain.NewInvalidPayloadError(
			endpoint + " returned an empty " + field + " identifier",
		)
	}
	if _, exists := seen[identifier]; exists {
		return domain.NewInvalidPayloadError(
			endpoint + " returned a duplicate " + field + " identifier",
		)
	}
	seen[identifier] = struct{}{}
	return nil
}

func validateCompleteDataTable(
	endpoint string,
	recordsTotal int,
	recordsFiltered int,
	rows int,
) error {
	if recordsTotal < 0 ||
		recordsFiltered < 0 ||
		recordsFiltered > recordsTotal ||
		rows != recordsFiltered {
		return domain.NewInvalidPayloadError(
			fmt.Sprintf(
				"%s returned an incomplete or inconsistent collection",
				endpoint,
			),
		)
	}
	return nil
}

func (s *SnapshotSource) requestJSON(
	ctx context.Context,
	cookie string,
	path string,
	body string,
	headers http.Header,
	destination any,
) (ResponseMetadata, int64, error) {
	response, err := s.client.doRequestWithHeaders(
		ctx,
		http.MethodPost,
		path,
		cookie,
		strings.NewReader(body),
		headers,
	)
	if err != nil {
		return ResponseMetadata{}, 0, requestError(ctx, err)
	}
	defer response.Body.Close()
	metadata := responseMetadata(response)
	if response.StatusCode == http.StatusNotModified {
		return metadata, 0, domain.ErrNotModified
	}
	if response.StatusCode == http.StatusFound ||
		response.StatusCode == http.StatusMovedPermanently ||
		response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden {
		drainErrorBody(response.Body)
		return metadata, 0, domain.ErrAuthExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		drainErrorBody(response.Body)
		return metadata, 0, &domain.UpstreamStatusError{
			StatusCode: response.StatusCode,
			RetryAfter: parseRetryAfter(metadata.RetryAfter, s.now().UTC()),
		}
	}

	contentType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if parseErr != nil || (contentType != "application/json" && !strings.HasSuffix(contentType, "+json")) {
		drainErrorBody(response.Body)
		return metadata, 0, domain.NewInvalidPayloadError("unsupported upstream content type")
	}
	metadata.ContentType = contentType
	limited := io.LimitReader(response.Body, s.responseBodyLimit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return metadata, int64(len(payload)), requestError(ctx, err)
	}
	bytesRead := int64(len(payload))
	if bytesRead > s.responseBodyLimit {
		return metadata, bytesRead, domain.NewInvalidPayloadError("upstream response body exceeds configured limit")
	}
	metadata.RawBodyHash = rawBodyHash(payload)
	if err := s.responseGuard.ValidateBody(payload, ResponseExpectation{RequireJSON: true}); err != nil {
		if errors.Is(err, ErrAuthenticationResponse) {
			return metadata, bytesRead, domain.ErrAuthExpired
		}
		return metadata, bytesRead, domain.NewInvalidPayloadError(fmt.Sprintf("response guard: %v", err))
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return metadata, bytesRead, domain.NewInvalidPayloadError(
			fmt.Sprintf("decode typed upstream response: %v", err),
		)
	}
	return metadata, bytesRead, nil
}

func responseMetadata(response *http.Response) ResponseMetadata {
	return ResponseMetadata{
		StatusCode:   response.StatusCode,
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		CacheControl: response.Header.Get("Cache-Control"),
		RetryAfter:   response.Header.Get("Retry-After"),
	}
}

func rawBodyHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func drainErrorBody(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, errorBodyDrainLimit))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	deadline, err := http.ParseTime(value)
	if err != nil || !deadline.After(now) {
		return 0
	}
	return deadline.Sub(now)
}
