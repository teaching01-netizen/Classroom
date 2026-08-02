package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

type attendanceExportServiceStub struct {
	result    *service.AttendanceExportResult
	err       error
	threshold int
}

func (s *attendanceExportServiceStub) GetFreshAttendanceExport(
	_ context.Context,
	_ string,
	threshold int,
) (*service.AttendanceExportResult, error) {
	s.threshold = threshold
	return s.result, s.err
}

func attendanceExportFixture() *service.AttendanceExportResult {
	exportedAt := time.Date(2026, 8, 2, 12, 30, 0, 0, time.UTC)
	return &service.AttendanceExportResult{
		Source:            "validated-snapshot",
		ExportedAt:        exportedAt,
		SourceValidatedAt: exportedAt.Add(-time.Minute),
		Report: &domain.CourseAttendanceReport{
			CourseID:   "course/1",
			CourseName: "Unicode วิชา",
			Threshold:  1,
			ComputedAt: exportedAt,
			Sessions: []domain.SessionSummary{{
				SessionID:     "session-1",
				SessionNumber: 1,
				Name:          "Week, \"One\"",
				Status:        domain.SessionStatusDone,
			}},
			Students: []domain.StudentAttendance{{
				StudentID:        "00123",
				Name:             "=SUM(A1:A2)",
				Nickname:         "ชื่อนักเรียน, \"หนึ่ง\"",
				School:           "Engineering",
				AttendedSessions: 1,
				TotalSessions:    1,
				AttendanceRate:   1,
				AtRisk:           false,
				PerSession: []domain.SessionCell{{
					SessionID: "session-1",
					CheckedIn: true,
					Status:    "ok",
				}},
			}},
		},
	}
}

func TestAttendanceExportCSV_usesCanonicalSafeRectangularSchema(t *testing.T) {
	// Given
	fixture := attendanceExportFixture()

	// When
	payload, err := buildAttendanceCSV(fixture)

	// Then
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(payload, []byte{0xef, 0xbb, 0xbf}))
	rows, err := csv.NewReader(bytes.NewReader(payload[3:])).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, []string{
		"course_id", "course_name", "exported_at", "source_validated_at",
		"student_id", "student_name", "nickname", "school",
		"attended_sessions", "total_sessions", "attendance_rate", "at_risk", "Session 1",
	}, rows[0])
	assert.Equal(t, []string{
		"course/1", "Unicode วิชา", "2026-08-02T12:30:00Z", "2026-08-02T12:29:00Z",
		"00123", "'=SUM(A1:A2)", "ชื่อนักเรียน, \"หนึ่ง\"", "Engineering",
		"1", "1", "100.00%", "false", "Present",
	}, rows[1])
}

func TestAttendanceExportXLSX_usesDedicatedTypedLayoutAndMetadata(t *testing.T) {
	// Given
	fixture := attendanceExportFixture()

	// When
	payload, err := buildAttendanceXLSX(fixture)

	// Then
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(payload))
	require.NoError(t, err)
	defer func() { require.NoError(t, workbook.Close()) }()
	assert.Equal(t, []string{"Attendance", "Metadata"}, workbook.GetSheetList())
	headers, err := workbook.GetRows("Attendance")
	require.NoError(t, err)
	require.NotEmpty(t, headers)
	assert.Equal(t, []string{
		"Student ID", "Student Name", "Nickname", "School", "Attended Sessions",
		"Total Sessions", "Attendance Rate", "At Risk", "Session 1",
	}, headers[0])
	studentID, err := workbook.GetCellValue("Attendance", "A2")
	require.NoError(t, err)
	assert.Equal(t, "00123", studentID)
	name, err := workbook.GetCellValue("Attendance", "B2")
	require.NoError(t, err)
	assert.Equal(t, "'=SUM(A1:A2)", name)
	attendanceRate, err := workbook.GetCellValue("Attendance", "G2")
	require.NoError(t, err)
	assert.Equal(t, "100.00%", attendanceRate)
	for cell, expected := range map[string]string{
		"B1": "course/1",
		"B2": "Unicode วิชา",
		"B3": "2026-08-02T12:30:00Z",
		"B4": "2026-08-02T12:29:00Z",
		"B5": "1",
		"B6": "1",
		"B7": "validated-snapshot",
	} {
		value, valueErr := workbook.GetCellValue("Metadata", cell)
		require.NoError(t, valueErr)
		assert.Equal(t, expected, value)
	}
	for _, cell := range []string{"E2", "F2", "G2"} {
		cellType, typeErr := workbook.GetCellType("Attendance", cell)
		require.NoError(t, typeErr)
		// XLSX numeric cells use the default numeric type without a string type attribute.
		assert.Equal(t, excelize.CellTypeUnset, cellType)
	}

	sheetXML := zipEntry(t, payload, "xl/worksheets/sheet1.xml")
	assert.Contains(t, sheetXML, `state="frozen"`)
	assert.Contains(t, sheetXML, `<autoFilter ref="$A$1:$I$2"`)
	stylesXML := zipEntry(t, payload, "xl/styles.xml")
	assert.Contains(t, stylesXML, `numFmtId="10"`)
}

func TestAttendanceExportPayloads_includeRefreshedPresentInsteadOfStaleAbsent(t *testing.T) {
	for _, format := range []string{"csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			// Given
			fixture := attendanceExportFixture()
			fixture.Report.Students[0].PerSession[0].CheckedIn = true

			// When
			var payload []byte
			var err error
			if format == "csv" {
				payload, err = buildAttendanceCSV(fixture)
			} else {
				payload, err = buildAttendanceXLSX(fixture)
			}

			// Then
			require.NoError(t, err)
			if format == "csv" {
				assert.Contains(t, string(payload), "Present")
				assert.NotContains(t, string(payload), "Absent")
				return
			}
			workbook, openErr := excelize.OpenReader(bytes.NewReader(payload))
			require.NoError(t, openErr)
			defer func() { require.NoError(t, workbook.Close()) }()
			status, valueErr := workbook.GetCellValue("Attendance", "I2")
			require.NoError(t, valueErr)
			assert.Equal(t, "Present", status)
		})
	}
}

func TestAttendanceExportHandler_downloadsBothFormatsWithValidatedSnapshotHeaders(t *testing.T) {
	for _, format := range []string{"csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			// Given
			stub := &attendanceExportServiceStub{result: attendanceExportFixture()}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/teacher/courses/course%2F1/attendance-report/export",
				strings.NewReader(`{"format":"`+format+`","threshold":2}`),
			)
			request.SetPathValue("courseId", "course/1")
			recorder := httptest.NewRecorder()

			// When
			attendanceExportHandler(stub).ServeHTTP(recorder, request)

			// Then
			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, 2, stub.threshold)
			assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "validated-snapshot", recorder.Header().Get("X-Attendance-Source"))
			assert.Equal(t, "2026-08-02T12:30:00Z", recorder.Header().Get("X-Attendance-Exported-At"))
			assert.Equal(t, "2026-08-02T12:29:00Z", recorder.Header().Get("X-Attendance-Validated-At"))
			assert.Empty(t, recorder.Header().Get("X-Attendance-Generated-At"))
			assert.Contains(t, recorder.Header().Get("Content-Disposition"), `attachment; filename="attendance-unicode-2026-08-02.`+format+`"`)
			assert.NotEmpty(t, recorder.Body.Bytes())
			if evidenceDir := os.Getenv("ATTENDANCE_EXPORT_EVIDENCE_DIR"); evidenceDir != "" {
				require.NoError(t, os.MkdirAll(evidenceDir, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(evidenceDir, "attendance-course-1."+format),
					recorder.Body.Bytes(),
					0o644,
				))
			}
		})
	}
}

func TestAttendanceExportHandler_MapsValidationFreshnessAndDeadlineErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		err     error
		status  int
		message string
	}{
		{name: "invalid format", body: `{"format":"pdf","threshold":0}`, status: http.StatusBadRequest, message: "format must be csv or xlsx"},
		{name: "negative threshold", body: `{"format":"csv","threshold":-1}`, status: http.StatusBadRequest, message: "threshold must be non-negative"},
		{name: "freshness failure", body: `{"format":"csv","threshold":0}`, err: errors.Join(service.ErrAttendanceExportFreshness, errors.New("secret upstream detail")), status: http.StatusServiceUnavailable, message: "Latest attendance data could not be validated. Please try again."},
		{name: "deadline", body: `{"format":"csv","threshold":0}`, err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, message: "attendance export timed out"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			stub := &attendanceExportServiceStub{result: attendanceExportFixture(), err: test.err}
			request := httptest.NewRequest(http.MethodPost, "/export", strings.NewReader(test.body))
			request.SetPathValue("courseId", "course-1")
			recorder := httptest.NewRecorder()

			// When
			attendanceExportHandler(stub).ServeHTTP(recorder, request)

			// Then
			assert.Equal(t, test.status, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.message)
			assert.NotContains(t, recorder.Body.String(), "secret upstream detail")
		})
	}
}

func zipEntry(t *testing.T, payload []byte, name string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		return string(content)
	}
	t.Fatalf("zip entry %s not found", name)
	return ""
}
