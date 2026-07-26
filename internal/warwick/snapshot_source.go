package warwick

import (
	"context"
	"crypto/tls"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

const (
	profilePageSize      = 500
	maxProfileRecords    = 100_000
	errorBodyDrainLimit  = 4 << 10
	sessionAcquireBudget = 5 * time.Second
)

type ResponseMetadata struct {
	StatusCode   int
	ETag         string
	LastModified string
	CacheControl string
	RetryAfter   string
}

type SnapshotFetchResult struct {
	Value     any
	Metadata  ResponseMetadata
	BytesRead int64
}

type SnapshotSource struct {
	client            *ClassroomClient
	responseBodyLimit int64
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
	ref, err := s.client.pool.AcquireWithTimeoutContext(ctx, s.client.tier, sessionAcquireBudget)
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
		map[string]string{"keyword": "", "UserID": s.client.effectiveUserID()},
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
	result := SnapshotFetchResult{Metadata: metadata, BytesRead: bytesRead}
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
	result := SnapshotFetchResult{Metadata: metadata, BytesRead: bytesRead}
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
		status := domain.SessionStatusActive
		if row.DStatus == "Finished" {
			status = domain.SessionStatusDone
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
	result := SnapshotFetchResult{Metadata: metadata, BytesRead: bytesRead}
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

func (s *SnapshotSource) fetchProfiles(
	ctx context.Context,
	cookie string,
) (SnapshotFetchResult, error) {
	profiles := make([]domain.StudentProfile, 0)
	seenStudentIDs := make(map[string]struct{})
	seenStudentGUIDs := make(map[string]struct{})
	var totalBytes int64
	var firstMetadata ResponseMetadata
	total := -1
	for start := 0; total < 0 || start < total; start += profilePageSize {
		request := DefaultDataTablesRequest([]string{"StudentID", "StudentGuid", "FullName", "School"})
		request.Start = start
		request.Length = profilePageSize
		body := EncodeDataTablesBody(request, map[string]string{"keyword": "", "IsActive": ""})
		var response UserGroupSearchResponse
		metadata, bytesRead, err := s.requestJSON(
			ctx,
			cookie,
			"/admin/api/UserGroupSearch",
			body,
			nil,
			&response,
		)
		totalBytes += bytesRead
		if start == 0 {
			firstMetadata = metadata
			firstMetadata.ETag = ""
			firstMetadata.LastModified = ""
			firstMetadata.CacheControl = ""
		}
		if err != nil {
			return SnapshotFetchResult{Metadata: metadata, BytesRead: totalBytes}, err
		}
		if response.RecordsTotal < 0 ||
			response.RecordsFiltered < 0 ||
			response.RecordsFiltered > response.RecordsTotal ||
			response.RecordsFiltered > maxProfileRecords {
			return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
				domain.NewInvalidPayloadError("UserGroupSearch record count outside supported range")
		}
		if total < 0 {
			total = response.RecordsFiltered
		} else if response.RecordsFiltered != total {
			return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
				domain.NewInvalidPayloadError("UserGroupSearch record count changed during pagination")
		}
		expectedRows := total - start
		if expectedRows > profilePageSize {
			expectedRows = profilePageSize
		}
		if expectedRows < 0 || len(response.Data) != expectedRows {
			return SnapshotFetchResult{Metadata: firstMetadata, BytesRead: totalBytes},
				domain.NewInvalidPayloadError("UserGroupSearch returned an incomplete page")
		}
		for _, row := range response.Data {
			studentID := strings.TrimSpace(row.StudentID)
			if err := recordUniqueIdentifier(
				seenStudentIDs,
				studentID,
				"UserGroupSearch",
				"student",
			); err != nil {
				return SnapshotFetchResult{
					Metadata:  firstMetadata,
					BytesRead: totalBytes,
				}, err
			}
			studentGUID := strings.TrimSpace(row.StudentGuid)
			if err := recordUniqueIdentifier(
				seenStudentGUIDs,
				studentGUID,
				"UserGroupSearch",
				"student GUID",
			); err != nil {
				return SnapshotFetchResult{
					Metadata:  firstMetadata,
					BytesRead: totalBytes,
				}, err
			}
			profiles = append(profiles, domain.StudentProfile{
				StudentID:   studentID,
				StudentGuid: studentGUID,
				FullName:    row.FullName,
				School:      row.School,
			})
		}
	}
	return SnapshotFetchResult{
		Value: profiles, Metadata: firstMetadata, BytesRead: totalBytes,
	}, nil
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
	limited := io.LimitReader(response.Body, s.responseBodyLimit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return metadata, int64(len(payload)), requestError(ctx, err)
	}
	bytesRead := int64(len(payload))
	if bytesRead > s.responseBodyLimit {
		return metadata, bytesRead, domain.NewInvalidPayloadError("upstream response body exceeds configured limit")
	}
	if isLoginPage(string(payload)) {
		return metadata, bytesRead, domain.ErrAuthExpired
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
