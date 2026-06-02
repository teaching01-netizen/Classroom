package warwick

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DataTablesRequest represents the server-side DataTables protocol parameters.
type DataTablesRequest struct {
	Draw    int               `json:"draw"`
	Start   int               `json:"start"`
	Length  int               `json:"length"`
	Search  DataTablesSearch  `json:"search"`
	Order   []DataTablesOrder `json:"order"`
	Columns []DataTablesColumn `json:"columns"`
}

// DataTablesColumn represents a column definition in a DataTables request.
type DataTablesColumn struct {
	Data       string          `json:"data"`
	Name       string          `json:"name"`
	Searchable bool            `json:"searchable"`
	Orderable  bool            `json:"orderable"`
	Search     DataTablesSearch `json:"search"`
}

// DataTablesSearch represents the search parameters in a DataTables request.
type DataTablesSearch struct {
	Value string `json:"value"`
	Regex bool   `json:"regex"`
}

// DataTablesOrder represents a single ordering column in a DataTables request.
type DataTablesOrder struct {
	Column int    `json:"column"`
	Dir    string `json:"dir"`
}

// ClassAttendanceSearchResponse is the DataTables response for the ClassAttendance search endpoint.
type ClassAttendanceSearchResponse struct {
	Draw            int                  `json:"draw"`
	RecordsTotal    int                  `json:"recordsTotal"`
	RecordsFiltered int                  `json:"recordsFiltered"`
	Data            []ClassAttendanceRow `json:"data"`
}

// ClassAttendanceRow represents a single course in the ClassAttendance search results.
type ClassAttendanceRow struct {
	ID         interface{} `json:"ID"`
	CourseName string      `json:"CourseName"`
	Cycle      string      `json:"Cycle"`
	Enrolled   interface{} `json:"Enrolled"`
	StartDate  interface{} `json:"StartDate"`
	EndDate    interface{} `json:"EndDate"`
}

// ClassAttendanceDetailResponse is the DataTables response for the ClassAttendance detail search endpoint.
type ClassAttendanceDetailResponse struct {
	Draw            int                       `json:"draw"`
	RecordsTotal    int                       `json:"recordsTotal"`
	RecordsFiltered int                       `json:"recordsFiltered"`
	Data            []ClassAttendanceDetailRow `json:"data"`
}

// ClassAttendanceDetailRow represents a single session in the ClassAttendance detail results.
type ClassAttendanceDetailRow struct {
	DID     interface{} `json:"dID"`
	DName   string      `json:"dName"`
	DStatus string      `json:"dStatus"`
}

// StudentCheckInSearchResponse is the DataTables response for the student check-in search endpoint.
type StudentCheckInSearchResponse struct {
	Draw            int                 `json:"draw"`
	RecordsTotal    int                 `json:"recordsTotal"`
	RecordsFiltered int                 `json:"recordsFiltered"`
	Data            []StudentCheckInRow `json:"data"`
}

// StudentCheckInRow represents a single student in the check-in search results.
type StudentCheckInRow struct {
	StudentID        string `json:"StudentID"`
	StudentName      string `json:"StudentName"`
	StudentNickName  string `json:"StudentNickName"`
	StudentSchool    string `json:"StudentSchool"`
	StudentImg       string `json:"StudentImg"`
	StudentCheckIn   bool   `json:"StudentCheckIn"`
	StudentPPoint    int    `json:"StudentPPoint"`
	StudentGivePoint bool   `json:"StudentGivePoint"`
}

// GivePointResponse represents Warwick's C# Tuple response for the AddParticipationPoint endpoint.
type GivePointResponse struct {
	Item1 bool   `json:"Item1"` // success indicator
	Item2 string `json:"Item2"` // message or error description
}

// DefaultDataTablesRequest returns a DataTablesRequest with sensible defaults for initial queries.
func DefaultDataTablesRequest(columns []string) DataTablesRequest {
	cols := make([]DataTablesColumn, len(columns))
	for i, name := range columns {
		cols[i] = DataTablesColumn{
			Data:       name,
			Name:       "",
			Searchable: true,
			Orderable:  true,
			Search:     DataTablesSearch{Value: "", Regex: false},
		}
	}
	return DataTablesRequest{
		Draw:    1,
		Start:   0,
		Length:  500, // large page size — Warwick's ASP.NET DataTables treats -1 as TOP 0
		Search:  DataTablesSearch{Value: "", Regex: false},
		Order:   []DataTablesOrder{{Column: 0, Dir: "asc"}},
		Columns: cols,
	}
}

// ParseCycle parses a cycle date range string like '27 May 2026 - 03 Jul 2026' into start and end times.
func ParseCycle(cycle string) (start, end time.Time, err error) {
	parts := strings.SplitN(cycle, " - ", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid cycle format: %q", cycle)
	}
	layout := "2 January 2006"
	start, err = time.Parse(layout, strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse cycle start: %w", err)
	}
	end, err = time.Parse(layout, strings.TrimSpace(parts[1]))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse cycle end: %w", err)
	}
	return start, end, nil
}

// EncodeDataTablesBody encodes a DataTables request plus extra parameters as a URL-encoded form body.
func EncodeDataTablesBody(d DataTablesRequest, extra map[string]string) string {
	v := url.Values{}
	v.Set("draw", fmt.Sprintf("%d", d.Draw))
	v.Set("start", fmt.Sprintf("%d", d.Start))
	v.Set("length", fmt.Sprintf("%d", d.Length))
	v.Set("search[value]", d.Search.Value)
	v.Set("search[regex]", fmt.Sprintf("%t", d.Search.Regex))
	for i, col := range d.Columns {
		v.Set(fmt.Sprintf("columns[%d][data]", i), col.Data)
		v.Set(fmt.Sprintf("columns[%d][name]", i), col.Name)
		v.Set(fmt.Sprintf("columns[%d][searchable]", i), fmt.Sprintf("%t", col.Searchable))
		v.Set(fmt.Sprintf("columns[%d][orderable]", i), fmt.Sprintf("%t", col.Orderable))
		v.Set(fmt.Sprintf("columns[%d][search][value]", i), col.Search.Value)
		v.Set(fmt.Sprintf("columns[%d][search][regex]", i), fmt.Sprintf("%t", col.Search.Regex))
	}
	for _, o := range d.Order {
		v.Set(fmt.Sprintf("order[%d][column]", o.Column), fmt.Sprintf("%d", o.Column))
		v.Set(fmt.Sprintf("order[%d][dir]", o.Column), o.Dir)
	}
	for k, val := range extra {
		v.Set(k, val)
	}
	return v.Encode()
}
