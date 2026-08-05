package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
	"qr-command-center/internal/service"
)

const maxAttendanceExportBodyBytes = 64 << 10

type attendanceExportService interface {
	GetFreshAttendanceExport(context.Context, string, int) (*service.AttendanceExportResult, error)
}

type attendanceExportRequest struct {
	Format    string `json:"format"`
	Threshold int    `json:"threshold"`
}

func attendanceExportHandler(exporter attendanceExportService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		format := "unknown"
		status := "success"
		defer func() {
			metrics.AttendanceExportRequestsTotal.WithLabelValues(format, status).Inc()
			metrics.AttendanceExportDurationSeconds.WithLabelValues(format).Observe(time.Since(started).Seconds())
		}()

		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			courseID = r.PathValue("courseId")
		}
		if courseID == "" {
			status = "failure"
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId is required"))
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxAttendanceExportBodyBytes)
		var request attendanceExportRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			status = "failure"
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				metrics.AttendanceExportFailuresTotal.WithLabelValues("too_large").Inc()
				writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse("export request too large"))
				return
			}
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		format = request.Format
		if request.Format != "csv" && request.Format != "xlsx" {
			status = "failure"
			writeJSON(w, http.StatusBadRequest, errorResponse("format must be csv or xlsx"))
			return
		}
		if request.Threshold < 0 {
			status = "failure"
			writeJSON(w, http.StatusBadRequest, errorResponse("threshold must be non-negative"))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		export, err := exporter.GetFreshAttendanceExport(ctx, courseID, request.Threshold)
		if err != nil {
			status = "failure"
			metrics.AttendanceExportFailuresTotal.WithLabelValues(exportFailureReason(err)).Inc()
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				writeJSON(w, http.StatusGatewayTimeout, errorResponse("attendance export timed out"))
			case errors.Is(err, service.ErrAttendanceExportCourseNotFound):
				writeJSON(w, http.StatusNotFound, errorResponse("course not found"))
			case errors.Is(err, service.ErrAttendanceExportTooLarge):
				writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse("attendance report too large"))
			case errors.Is(err, context.Canceled):
				return
			case errors.Is(err, service.ErrAttendanceExportLiveFailure):
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("attendance data is temporarily unavailable; please try again"))
			case errors.Is(err, service.ErrAttendanceExportFreshness):
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Latest attendance data could not be validated. Please try again."))
			default:
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Latest attendance data could not be validated. Please try again."))
			}
			return
		}
		if export == nil || export.Report == nil {
			status = "failure"
			metrics.AttendanceExportFailuresTotal.WithLabelValues("report_error").Inc()
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Latest attendance data could not be validated. Please try again."))
			return
		}

		var payload []byte
		var contentType string
		generationStarted := time.Now()
		if request.Format == "csv" {
			payload, err = buildAttendanceCSV(export)
			contentType = "text/csv; charset=utf-8"
		} else {
			payload, err = buildAttendanceXLSX(export)
			contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
		metrics.AttendanceExportGenerationDurationSeconds.WithLabelValues(format).Observe(time.Since(generationStarted).Seconds())
		if err != nil {
			status = "failure"
			metrics.AttendanceExportFailuresTotal.WithLabelValues("generation_failed").Inc()
			writeJSON(w, http.StatusInternalServerError, errorResponse("attendance export generation failed"))
			return
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			status = "failure"
			metrics.AttendanceExportFailuresTotal.WithLabelValues(exportFailureReason(ctxErr)).Inc()
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				writeJSON(w, http.StatusGatewayTimeout, errorResponse("attendance export timed out"))
			}
			return
		}

		filename := attendanceExportFilename(export.Report.CourseName, export.ExportedAt, request.Format)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Attendance-Source", export.Source)
		w.Header().Set("X-Attendance-Exported-At", export.ExportedAt.UTC().Format(time.RFC3339))
		w.Header().Set("X-Attendance-Validated-At", export.SourceValidatedAt.UTC().Format(time.RFC3339))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		metrics.AttendanceExportBytesTotal.WithLabelValues(format).Add(float64(len(payload)))
		metrics.AttendanceExportRefreshRestartsTotal.Add(float64(export.RestartCount))
		metrics.AttendanceExportFreshnessDurationSeconds.Observe(float64(export.FreshnessDurationMs) / 1000.0)
	}
}

// exportFailureReason maps an export error to a bounded, low-cardinality
// metric reason. The mapping is deterministic: every error resolves to exactly
// one of the allowed label values.
func exportFailureReason(err error) string {
	switch {
	case err == nil:
		return "report_error"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, service.ErrAttendanceExportCourseNotFound):
		return "course_not_found"
	case errors.Is(err, service.ErrAttendanceExportTooLarge):
		return "too_large"
	case errors.Is(err, service.ErrAttendanceExportFreshness):
		message := err.Error()
		if strings.Contains(message, "incomplete") {
			return "incomplete"
		}
		if strings.Contains(message, "overdue") || strings.Contains(message, "not verified fresh") {
			return "stale"
		}
		return "refresh_failed"
	default:
		return "report_error"
	}
}

func buildAttendanceCSV(export *service.AttendanceExportResult) ([]byte, error) {
	var output bytes.Buffer
	output.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&output)
	if err := writer.Write(csvAttendanceHeaders(len(export.Report.Sessions))); err != nil {
		return nil, fmt.Errorf("write CSV header: %w", err)
	}
	for _, student := range export.Report.Students {
		if err := writer.Write(csvAttendanceRow(export, student)); err != nil {
			return nil, fmt.Errorf("write CSV row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush CSV: %w", err)
	}
	return output.Bytes(), nil
}

func buildAttendanceXLSX(export *service.AttendanceExportResult) ([]byte, error) {
	workbook := excelize.NewFile()
	defer func() { _ = workbook.Close() }()
	if err := workbook.SetSheetName("Sheet1", "Attendance"); err != nil {
		return nil, fmt.Errorf("name attendance sheet: %w", err)
	}
	if _, err := workbook.NewSheet("Metadata"); err != nil {
		return nil, fmt.Errorf("create metadata sheet: %w", err)
	}

	headers := xlsxAttendanceHeaders(len(export.Report.Sessions))
	for column, value := range headers {
		cell, err := excelize.CoordinatesToCellName(column+1, 1)
		if err != nil {
			return nil, fmt.Errorf("attendance header cell: %w", err)
		}
		if err := workbook.SetCellStr("Attendance", cell, value); err != nil {
			return nil, fmt.Errorf("write attendance header: %w", err)
		}
	}
	for rowIndex, student := range export.Report.Students {
		for column, value := range xlsxAttendanceTextValues(student, len(export.Report.Sessions)) {
			cell, err := excelize.CoordinatesToCellName(column+1, rowIndex+2)
			if err != nil {
				return nil, fmt.Errorf("attendance cell: %w", err)
			}
			if err := workbook.SetCellStr("Attendance", cell, value); err != nil {
				return nil, fmt.Errorf("write attendance text: %w", err)
			}
		}
		row := strconv.Itoa(rowIndex + 2)
		if err := workbook.SetCellValue("Attendance", "E"+row, student.AttendedSessions); err != nil {
			return nil, fmt.Errorf("write attended sessions: %w", err)
		}
		if err := workbook.SetCellValue("Attendance", "F"+row, student.TotalSessions); err != nil {
			return nil, fmt.Errorf("write total sessions: %w", err)
		}
		if err := workbook.SetCellValue("Attendance", "G"+row, student.AttendanceRate); err != nil {
			return nil, fmt.Errorf("write attendance rate: %w", err)
		}
		if err := workbook.SetCellValue("Attendance", "H"+row, student.AtRisk); err != nil {
			return nil, fmt.Errorf("write at risk: %w", err)
		}
	}
	lastColumn, err := excelize.ColumnNumberToName(len(headers))
	if err != nil {
		return nil, fmt.Errorf("attendance last column: %w", err)
	}
	lastRow := max(2, len(export.Report.Students)+1)
	if err := workbook.AutoFilter("Attendance", "A1:"+lastColumn+strconv.Itoa(lastRow), nil); err != nil {
		return nil, fmt.Errorf("set attendance filter: %w", err)
	}
	if err := workbook.SetPanes("Attendance", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, fmt.Errorf("freeze attendance header: %w", err)
	}
	percentageStyle, err := workbook.NewStyle(&excelize.Style{NumFmt: 10})
	if err != nil {
		return nil, fmt.Errorf("create percentage style: %w", err)
	}
	if err := workbook.SetCellStyle("Attendance", "G2", "G"+strconv.Itoa(lastRow), percentageStyle); err != nil {
		return nil, fmt.Errorf("format percentages: %w", err)
	}

	metadata := [][2]string{
		{"Course ID", safeSpreadsheetText(export.Report.CourseID)},
		{"Course Name", safeSpreadsheetText(export.Report.CourseName)},
		{"Exported At", export.ExportedAt.UTC().Format(time.RFC3339)},
		{"Source Validated At", export.SourceValidatedAt.UTC().Format(time.RFC3339)},
		{"Threshold", strconv.Itoa(export.Report.Threshold)},
		{"Session Count", strconv.Itoa(len(export.Report.Sessions))},
		{"Source Type", safeSpreadsheetText(export.Source)},
	}
	for index, item := range metadata {
		row := strconv.Itoa(index + 1)
		if err := workbook.SetCellStr("Metadata", "A"+row, item[0]); err != nil {
			return nil, fmt.Errorf("write metadata label: %w", err)
		}
		if err := workbook.SetCellStr("Metadata", "B"+row, item[1]); err != nil {
			return nil, fmt.Errorf("write metadata value: %w", err)
		}
	}

	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("serialize workbook: %w", err)
	}
	return buffer.Bytes(), nil
}

func csvAttendanceHeaders(sessionCount int) []string {
	headers := []string{
		"course_id", "course_name", "exported_at", "source_validated_at",
		"student_id", "student_name", "nickname", "school",
		"attended_sessions", "total_sessions", "attendance_rate", "at_risk",
	}
	return append(headers, attendanceSessionHeaders(sessionCount)...)
}

func csvAttendanceRow(export *service.AttendanceExportResult, student domain.StudentAttendance) []string {
	row := []string{
		safeSpreadsheetText(export.Report.CourseID),
		safeSpreadsheetText(export.Report.CourseName),
		export.ExportedAt.UTC().Format(time.RFC3339),
		export.SourceValidatedAt.UTC().Format(time.RFC3339),
	}
	return append(row, csvStudentAttendanceValues(student, len(export.Report.Sessions))...)
}

func xlsxAttendanceHeaders(sessionCount int) []string {
	headers := []string{
		"Student ID", "Student Name", "Nickname", "School", "Attended Sessions",
		"Total Sessions", "Attendance Rate", "At Risk",
	}
	return append(headers, attendanceSessionHeaders(sessionCount)...)
}

func attendanceSessionHeaders(sessionCount int) []string {
	headers := make([]string, sessionCount)
	for index := range sessionCount {
		headers[index] = "Session " + strconv.Itoa(index+1)
	}
	return headers
}

func xlsxAttendanceTextValues(student domain.StudentAttendance, sessionCount int) []string {
	row := []string{
		safeSpreadsheetText(student.StudentID),
		safeSpreadsheetText(student.Name),
		safeSpreadsheetText(student.Nickname),
		safeSpreadsheetText(student.School),
		"", "", "", "",
	}
	for index := range sessionCount {
		row = append(row, attendanceSessionValue(student, index))
	}
	return row
}

func csvStudentAttendanceValues(student domain.StudentAttendance, sessionCount int) []string {
	row := []string{
		safeSpreadsheetText(student.StudentID),
		safeSpreadsheetText(student.Name),
		safeSpreadsheetText(student.Nickname),
		safeSpreadsheetText(student.School),
		strconv.Itoa(student.AttendedSessions),
		strconv.Itoa(student.TotalSessions),
		strconv.FormatFloat(student.AttendanceRate*100, 'f', 2, 64) + "%",
		strconv.FormatBool(student.AtRisk),
	}
	for index := range sessionCount {
		row = append(row, attendanceSessionValue(student, index))
	}
	return row
}

func attendanceSessionValue(student domain.StudentAttendance, index int) string {
	if index >= len(student.PerSession) {
		return "Error"
	}
	cell := student.PerSession[index]
	switch {
	case cell.Status == "error":
		return "Error"
	case cell.SessionStatus == domain.SessionStatusActive || cell.SessionStatus == domain.SessionStatusNotStarted:
		return "N/A"
	case cell.CheckedIn:
		return "Present"
	default:
		return "Absent"
	}
}

func safeSpreadsheetText(value string) string {
	if value == "" {
		return value
	}
	first := value[0]
	if first == '=' || first == '+' || first == '-' || first == '@' || first == '\t' || first == '\r' {
		return "'" + value
	}
	return value
}

func attendanceExportFilename(courseName string, exportedAt time.Time, format string) string {
	var safe strings.Builder
	lastHyphen := false
	for _, char := range strings.ToLower(courseName) {
		if char <= unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char)) {
			safe.WriteRune(char)
			lastHyphen = false
		} else if !lastHyphen && safe.Len() > 0 {
			safe.WriteByte('-')
			lastHyphen = true
		}
		if safe.Len() >= 48 {
			break
		}
	}
	slug := strings.Trim(safe.String(), "-")
	if slug == "" {
		slug = "course"
	}
	return "attendance-" + slug + "-" + exportedAt.UTC().Format("2006-01-02") + "." + format
}
