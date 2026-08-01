package scraper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/warwick"
)

// Fixture-driven coverage for T3.5: realistic sanitized upstream payloads are
// parsed through the real warwick fetch path and canonicalized. Tests assert
// the axes the canonicalizer normalizes (row order, timestamp timezone/offset,
// nil-vs-empty slices) and honestly document the axes it does not (Unicode
// normalization, nil-vs-empty-string pointers, raw numeric representation).

var fixtureDir = filepath.Join("..", "warwick", "testdata")

type fixtureCase struct {
	name        string
	fixtureFile string
	endpoint    string
	kind        domain.SnapshotKind
	target      func() domain.ScrapeTarget
	assertShape func(t *testing.T, value any)
}

func fixtureCases() []fixtureCase {
	return []fixtureCase{
		{
			name:        "course catalog",
			fixtureFile: "course_catalog.json",
			endpoint:    "/admin/api/ClassAttendanceSearch",
			kind:        domain.SnapshotCourseCatalog,
			target: func() domain.ScrapeTarget {
				return domain.ScrapeTarget{Ref: domain.TargetRef{
					Host: "warwick.humantix.cloud", Kind: domain.SnapshotCourseCatalog,
					ResourceKey: "catalog",
				}}
			},
			assertShape: func(t *testing.T, value any) {
				courses, ok := value.([]domain.CourseSummary)
				require.True(t, ok)
				require.Len(t, courses, 5)
				require.Equal(t, "CSE-2025-101", courses[0].CourseID)
				require.Equal(t, "Introduction to Python Programming", courses[0].Name)
				require.Equal(t, "2025-01-13", courses[0].StartDate)
				require.Equal(t, "2025-03-21", courses[0].EndDate)
				require.Equal(t, 42, courses[0].EnrolledCount)
				require.Equal(t, "MATH-2025-202", courses[1].CourseID)
				require.Equal(t, "CSE-2027-301", courses[2].CourseID)
				require.Equal(t, "DBS-2026-401", courses[3].CourseID)
				require.Equal(t, "NET-2026-501", courses[4].CourseID)
				require.Equal(t, 29, courses[4].EnrolledCount)
				// Statuses derive from time.Now() via domain.GetCourseStatus, so
				// the fixture dates are chosen with wide margins: courses 1-2 are
				// finished, course 3 is upcoming, courses 4-5 span 2026-2027. The
				// assertions hold while now is inside [2026-07, 2027-09); refresh
				// the fixture when it rots.
				require.Equal(t, domain.CourseStatusFinished, courses[0].Status)
				require.Equal(t, domain.CourseStatusFinished, courses[1].Status)
				require.Equal(t, domain.CourseStatusUpcoming, courses[2].Status)
				require.Equal(t, domain.CourseStatusActive, courses[3].Status)
				require.Equal(t, domain.CourseStatusActive, courses[4].Status)
			},
		},
		{
			name:        "course detail",
			fixtureFile: "course_detail.json",
			endpoint:    "/admin/api/ClassAttendanceDetailSearch",
			kind:        domain.SnapshotCourseDetail,
			target: func() domain.ScrapeTarget {
				return domain.ScrapeTarget{Ref: domain.TargetRef{
					Host: "warwick.humantix.cloud", Kind: domain.SnapshotCourseDetail,
					ResourceKey: "CSE-2026-101",
				}, Attributes: json.RawMessage(`{"course_name":"Introduction to Python Programming"}`)}
			},
			assertShape: func(t *testing.T, value any) {
				detail, ok := value.(*domain.CourseDetail)
				require.True(t, ok)
				require.Equal(t, "CSE-2026-101", detail.CourseID)
				require.Equal(t, "Introduction to Python Programming", detail.Name)
				require.Equal(t, 4, detail.TotalSessions)
				require.Equal(t, 1, detail.CompletedSessions)
				require.Equal(t, domain.CourseStatusActive, detail.Status)
				require.Len(t, detail.Sessions, 4)
				require.Equal(t, "1001", detail.Sessions[0].SessionID)
				require.Equal(t, 1, detail.Sessions[0].SessionNumber)
				require.Equal(t, "Week 1: Introduction and Setup", detail.Sessions[0].Name)
				require.Equal(t, domain.SessionStatusNotStarted, detail.Sessions[0].Status)
				require.Equal(t, "1002", detail.Sessions[1].SessionID)
				require.Equal(t, 2, detail.Sessions[1].SessionNumber)
				require.Equal(t, domain.SessionStatusActive, detail.Sessions[1].Status)
				require.Equal(t, domain.SessionStatusDone, detail.Sessions[2].Status)
				require.Equal(t, domain.SessionStatusActive, detail.Sessions[3].Status)
			},
		},
		{
			name:        "session detail",
			fixtureFile: "session_detail.json",
			endpoint:    "/admin/api/ClassAttendanceStudentCheckInSearch",
			kind:        domain.SnapshotSessionDetail,
			target: func() domain.ScrapeTarget {
				return domain.ScrapeTarget{Ref: domain.TargetRef{
					Host: "warwick.humantix.cloud", Kind: domain.SnapshotSessionDetail,
					ParentKey: "CSE-2026-101", ResourceKey: "1002",
				}}
			},
			assertShape: func(t *testing.T, value any) {
				detail, ok := value.(*domain.SessionDetail)
				require.True(t, ok)
				require.Equal(t, "1002", detail.SessionID)
				require.Equal(t, 5, detail.TotalStudents)
				require.Equal(t, 3, detail.CheckedInCount)
				require.Len(t, detail.Students, 5)
				require.Equal(t, "S20240001", detail.Students[0].StudentID)
				require.Equal(t, "Olivia Chen", detail.Students[0].Name)
				require.Equal(t, "Oli", detail.Students[0].Nickname)
				require.Equal(t, "School of Engineering", detail.Students[0].School)
				require.True(t, detail.Students[0].CheckedIn)
				require.Equal(t, 2, detail.Students[0].ParticipationPoints)
				require.False(t, detail.Students[2].CheckedIn)
				require.True(t, detail.Students[3].CheckedIn)
				// The snapshot parse path does not populate check-in timestamps
				// (CheckedInAt/QRExpiresAt stay nil); timestamp normalization is
				// exercised by perturbing the parsed value below.
				for _, student := range detail.Students {
					require.Nil(t, student.CheckedInAt)
				}
				require.Nil(t, detail.QRExpiresAt)
			},
		},
		{
			name:        "student profiles",
			fixtureFile: "profiles.json",
			endpoint:    "/admin/api/UserGroupSearch",
			kind:        domain.SnapshotStudentProfiles,
			target: func() domain.ScrapeTarget {
				return domain.ScrapeTarget{Ref: domain.TargetRef{
					Host: "warwick.humantix.cloud", Kind: domain.SnapshotStudentProfiles,
					ResourceKey: "profiles",
				}}
			},
			assertShape: func(t *testing.T, value any) {
				profiles, ok := value.([]domain.StudentProfile)
				require.True(t, ok)
				require.Len(t, profiles, 6)
				require.Equal(t, "W240001", profiles[0].StudentID)
				require.Equal(t, "9f2c1a7e-4b3d-4f8a-9e1c-2d5b6a7c8d9e", profiles[0].StudentGuid)
				require.Equal(t, "Olivia Chen", profiles[0].FullName)
				require.Equal(t, "School of Engineering", profiles[0].School)
				require.Equal(t, "W240006", profiles[5].StudentID)
				require.Equal(t, "Mia Williams", profiles[5].FullName)
			},
		},
	}
}

// loadFixtureBytes reads a fixture from internal/warwick/testdata relative to
// this package's directory (go test runs each package with its source dir as
// the working directory).
func loadFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(fixtureDir, name))
	require.NoError(t, err, "load fixture %s", name)
	return payload
}

// newFixtureSnapshotSource wires a real warwick.SnapshotSource against an
// httptest server that serves the login endpoint and a single data endpoint
// from the given body, mirroring snapshotTestClient in internal/warwick but
// using the real login flow (the test harness can reach unexported fields).
func newFixtureSnapshotSource(t *testing.T, endpoint string, body []byte) *warwick.SnapshotSource {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/":
			http.SetCookie(w, &http.Cookie{Name: "ASP.NET_SessionId", Value: "fixture-session"})
			w.WriteHeader(http.StatusFound)
		case endpoint:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	auth := warwick.NewWarwickAuth("teacher@example.com", "fixture-password", server.URL+"/admin/")
	client := warwick.NewClassroomClient(auth)
	client.SetBaseURL(server.URL)
	client.SetTransport(server.Client().Transport)
	client.SetUserID("user-1")
	return warwick.NewSnapshotSource(client, 1<<20)
}

// fetchFixture drives the warwick fetch path for a fixture body and returns the
// parsed domain value.
func fetchFixture(t *testing.T, fc fixtureCase, body []byte) any {
	t.Helper()
	source := newFixtureSnapshotSource(t, fc.endpoint, body)
	result, err := source.Fetch(context.Background(), fc.target())
	require.NoError(t, err)
	require.NotNil(t, result.Value)
	return result.Value
}

func canonicalHash(t *testing.T, kind domain.SnapshotKind, value any) [32]byte {
	t.Helper()
	_, hash, _, err := Canonicalize(kind, value, 1<<20)
	require.NoError(t, err)
	return hash
}

// reverseRows returns a copy of the parsed value with row order reversed. The
// canonicalizer must produce the same hash regardless of input row order.
func reverseRows(kind domain.SnapshotKind, value any) any {
	switch kind {
	case domain.SnapshotCourseCatalog:
		courses := append([]domain.CourseSummary(nil), value.([]domain.CourseSummary)...)
		slices.Reverse(courses)
		return courses
	case domain.SnapshotCourseDetail:
		detail := *value.(*domain.CourseDetail)
		detail.Sessions = append([]domain.SessionSummary(nil), detail.Sessions...)
		slices.Reverse(detail.Sessions)
		return &detail
	case domain.SnapshotSessionDetail:
		detail := *value.(*domain.SessionDetail)
		detail.Students = append([]domain.StudentCheckin(nil), detail.Students...)
		slices.Reverse(detail.Students)
		return &detail
	case domain.SnapshotStudentProfiles:
		profiles := append([]domain.StudentProfile(nil), value.([]domain.StudentProfile)...)
		slices.Reverse(profiles)
		return profiles
	default:
		panic("unhandled kind " + string(kind))
	}
}

func TestFixtureParsesToExpectedShape(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			fc.assertShape(t, fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile)))
		})
	}
}

func TestFixtureCanonicalizationRowOrderIndependent(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			value := fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile))
			reordered := reverseRows(fc.kind, value)

			baseJSON, baseHash, _, err := Canonicalize(fc.kind, value, 1<<20)
			require.NoError(t, err)
			reorderedJSON, reorderedHash, _, err := Canonicalize(fc.kind, reordered, 1<<20)
			require.NoError(t, err)
			require.Equal(t, baseJSON, reorderedJSON)
			require.Equal(t, baseHash, reorderedHash)
		})
	}
}

func TestFixtureCanonicalizationTimestampTimezoneNormalized(t *testing.T) {
	fc := fixtureCases()[2] // session detail
	value := fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile))
	detail := *value.(*domain.SessionDetail)

	// The snapshot parse path leaves CheckedInAt/QRExpiresAt nil, so set them
	// on the parsed value to exercise the canonicalizer's RFC3339Nano -> UTC
	// normalization. Both representations denote the same instants.
	utc := "2026-07-26T10:00:00Z"
	offset := "2026-07-26T17:00:00+07:00"
	qrUTC := "2026-07-26T12:30:00Z"
	qrOffset := "2026-07-26T19:30:00+07:00"

	// Variants share the parsed Students backing array unless copied, so each
	// variant gets its own element-level copy before mutating timestamps.
	first := detail
	first.Students = append([]domain.StudentCheckin(nil), first.Students...)
	first.Students[0].CheckedInAt = &utc
	first.Students[1].CheckedInAt = &utc
	first.QRExpiresAt = &qrUTC

	second := detail
	second.Students = append([]domain.StudentCheckin(nil), second.Students...)
	second.Students[0].CheckedInAt = &offset
	second.Students[1].CheckedInAt = &offset
	second.QRExpiresAt = &qrOffset

	firstJSON, firstHash, _, err := Canonicalize(domain.SnapshotSessionDetail, &first, 1<<20)
	require.NoError(t, err)
	secondJSON, secondHash, _, err := Canonicalize(domain.SnapshotSessionDetail, &second, 1<<20)
	require.NoError(t, err)
	require.Equal(t, firstJSON, secondJSON)
	require.Equal(t, firstHash, secondHash)

	// Documented non-normalization: a nil pointer and a pointer to the empty
	// string are distinct (nil marshals to null, "" to an empty string), and
	// the canonicalizer does not collapse them.
	nilVariant := detail
	emptyVariant := detail
	nilVariant.Students = append([]domain.StudentCheckin(nil), nilVariant.Students...)
	emptyVariant.Students = append([]domain.StudentCheckin(nil), emptyVariant.Students...)
	empty := ""
	emptyVariant.Students[0].CheckedInAt = &empty
	nilHash := canonicalHash(t, domain.SnapshotSessionDetail, &nilVariant)
	emptyHash := canonicalHash(t, domain.SnapshotSessionDetail, &emptyVariant)
	require.NotEqual(t, nilHash, emptyHash,
		"nil and empty-string timestamps are intentionally not normalized")
}

func TestFixtureCanonicalizationNilVersusEmptySlices(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			value := fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile))
			nilHash := canonicalHash(t, fc.kind, withNilSlice(fc.kind, value))
			emptyHash := canonicalHash(t, fc.kind, withEmptySlice(fc.kind, value))
			require.Equal(t, nilHash, emptyHash)
		})
	}
}

func withNilSlice(kind domain.SnapshotKind, value any) any {
	switch kind {
	case domain.SnapshotCourseCatalog:
		return []domain.CourseSummary(nil)
	case domain.SnapshotCourseDetail:
		detail := *value.(*domain.CourseDetail)
		detail.Sessions = nil
		return &detail
	case domain.SnapshotSessionDetail:
		detail := *value.(*domain.SessionDetail)
		detail.Students = nil
		return &detail
	case domain.SnapshotStudentProfiles:
		return []domain.StudentProfile(nil)
	default:
		panic("unhandled kind " + string(kind))
	}
}

func withEmptySlice(kind domain.SnapshotKind, value any) any {
	switch kind {
	case domain.SnapshotCourseCatalog:
		return []domain.CourseSummary{}
	case domain.SnapshotCourseDetail:
		detail := *value.(*domain.CourseDetail)
		detail.Sessions = []domain.SessionSummary{}
		return &detail
	case domain.SnapshotSessionDetail:
		detail := *value.(*domain.SessionDetail)
		detail.Students = []domain.StudentCheckin{}
		return &detail
	case domain.SnapshotStudentProfiles:
		return []domain.StudentProfile{}
	default:
		panic("unhandled kind " + string(kind))
	}
}

// withNameReplaced returns a copy of the parsed value with one string field
// replaced, used to probe string-encoding axes (Unicode normalization).
func withNameReplaced(kind domain.SnapshotKind, value any, name string) any {
	switch kind {
	case domain.SnapshotCourseCatalog:
		courses := append([]domain.CourseSummary(nil), value.([]domain.CourseSummary)...)
		courses[0].Name = name
		return courses
	case domain.SnapshotCourseDetail:
		detail := *value.(*domain.CourseDetail)
		detail.Sessions = append([]domain.SessionSummary(nil), detail.Sessions...)
		detail.Sessions[0].Name = name
		return &detail
	case domain.SnapshotSessionDetail:
		detail := *value.(*domain.SessionDetail)
		detail.Students = append([]domain.StudentCheckin(nil), detail.Students...)
		detail.Students[0].Name = name
		return &detail
	case domain.SnapshotStudentProfiles:
		profiles := append([]domain.StudentProfile(nil), value.([]domain.StudentProfile)...)
		profiles[0].FullName = name
		return profiles
	default:
		panic("unhandled kind " + string(kind))
	}
}

func TestFixtureCanonicalizationDoesNotNormalizeUnicode(t *testing.T) {
	// Documented non-normalization: canonicalize marshals strings byte-for-byte
	// and never applies Unicode normalization, so an NFC name and its NFD
	// equivalent hash differently. If Unicode normalization is ever added to
	// the canonicalizer, these assertions flip to require.Equal.
	const nfc = "José Almeida"
	const nfd = "Jose\u0301 Almeida"
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			value := fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile))
			nfcHash := canonicalHash(t, fc.kind, withNameReplaced(fc.kind, value, nfc))
			nfdHash := canonicalHash(t, fc.kind, withNameReplaced(fc.kind, value, nfd))
			require.NotEqual(t, nfcHash, nfdHash,
				"NFD and NFC spellings must produce different hashes today")
		})
	}
}

// optionalRowFields lists the per-row fields that tolerate null (vs absent)
// without changing the parsed domain value. Identifier fields are excluded:
// a null identifier is rejected by the parser.
func optionalRowFields(kind domain.SnapshotKind) []string {
	switch kind {
	case domain.SnapshotCourseCatalog:
		return []string{"CourseName", "Cycle", "StartDate", "EndDate", "Enrolled"}
	case domain.SnapshotCourseDetail:
		return []string{"dName", "dStatus"}
	case domain.SnapshotSessionDetail:
		return []string{
			"StudentName", "StudentNickName", "StudentSchool", "StudentImg",
			"StudentCheckIn", "StudentPPoint", "StudentGivePoint",
		}
	case domain.SnapshotStudentProfiles:
		return []string{
			"FullName", "School", "MobilePhone", "ParentPhone",
			"IsActive", "TerminateStatus", "ExpireDateStr",
		}
	default:
		panic("unhandled kind " + string(kind))
	}
}

// rawVariant re-marshals a fixture from a generic map, applying a per-row
// mutation. Re-marshaling also sorts object keys (Go's encoding/json sorts map
// keys), which is itself a map-key-order perturbation.
func rawVariant(t *testing.T, fixture []byte, mutate func(row map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(fixture, &doc))
	rows, ok := doc["data"].([]any)
	require.True(t, ok, "fixture has a data array")
	for _, rowAny := range rows {
		row, ok := rowAny.(map[string]any)
		require.True(t, ok, "fixture rows are objects")
		mutate(row)
	}
	variant, err := json.Marshal(doc)
	require.NoError(t, err)
	return variant
}

func TestFixtureNullVersusMissingOptionalFieldsParseIdentically(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			fixture := loadFixtureBytes(t, fc.fixtureFile)
			fields := optionalRowFields(fc.kind)
			nulls := rawVariant(t, fixture, func(row map[string]any) {
				for _, field := range fields {
					row[field] = nil
				}
			})
			missing := rawVariant(t, fixture, func(row map[string]any) {
				for _, field := range fields {
					delete(row, field)
				}
			})
			// The two raw payloads must genuinely differ for this to be a test.
			require.NotEqual(t, nulls, missing)

			nullsHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, nulls))
			missingHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, missing))
			require.Equal(t, nullsHash, missingHash,
				"null and absent optional fields must parse to the same canonical value")
		})
	}
}

func TestFixtureMapKeyOrderIrrelevant(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			fixture := loadFixtureBytes(t, fc.fixtureFile)
			sorted := rawVariant(t, fixture, func(map[string]any) {})
			require.NotEqual(t, fixture, sorted,
				"fixture must be authored with non-sorted keys for this to be a test")

			baseHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, fixture))
			sortedHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, sorted))
			require.Equal(t, baseHash, sortedHash)
		})
	}
}

func TestFixtureNumericRepresentationConvergesAtParse(t *testing.T) {
	t.Run("catalog enrolled count as string vs number", func(t *testing.T) {
		fc := fixtureCases()[0]
		fixture := loadFixtureBytes(t, fc.fixtureFile)
		strings := rawVariant(t, fixture, func(row map[string]any) {
			row["Enrolled"] = strconv.Itoa(int(row["Enrolled"].(float64)))
		})
		require.NotEqual(t, fixture, strings)
		numberHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, fixture))
		stringHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, strings))
		require.Equal(t, numberHash, stringHash,
			"Enrolled as 42 and as \"42\" must canonicalize identically")
	})

	t.Run("course detail session id as number vs string", func(t *testing.T) {
		fc := fixtureCases()[1]
		fixture := loadFixtureBytes(t, fc.fixtureFile)
		numbers := rawVariant(t, fixture, func(row map[string]any) {
			id, err := strconv.Atoi(row["dID"].(string))
			require.NoError(t, err)
			row["dID"] = float64(id)
		})
		require.NotEqual(t, fixture, numbers)
		stringHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, fixture))
		numberHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, numbers))
		require.Equal(t, stringHash, numberHash,
			"dID as 1001 and as \"1001\" must canonicalize identically")
	})
}

func TestFixtureEmptyVersusNullDataArray(t *testing.T) {
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			fixture := loadFixtureBytes(t, fc.fixtureFile)
			empty := rawVariant(t, fixture, func(map[string]any) {})
			empty = withEmptyData(t, empty)
			nulled := rawVariant(t, fixture, func(map[string]any) {})
			nulled = withNullData(t, nulled)
			require.NotEqual(t, empty, nulled)

			emptyHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, empty))
			nullHash := canonicalHash(t, fc.kind, fetchFixture(t, fc, nulled))
			require.Equal(t, emptyHash, nullHash,
				"an empty data array and a null data array must canonicalize identically")
		})
	}
}

func withEmptyData(t *testing.T, fixture []byte) []byte {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(fixture, &doc))
	doc["data"] = []any{}
	doc["recordsTotal"] = 0
	doc["recordsFiltered"] = 0
	variant, err := json.Marshal(doc)
	require.NoError(t, err)
	return variant
}

func withNullData(t *testing.T, fixture []byte) []byte {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(fixture, &doc))
	doc["data"] = nil
	doc["recordsTotal"] = 0
	doc["recordsFiltered"] = 0
	variant, err := json.Marshal(doc)
	require.NoError(t, err)
	return variant
}

func TestFixtureCanonicalizationDeterministicAcrossRuns(t *testing.T) {
	// >= 1000 canonicalization runs of the same parsed value must produce an
	// identical hash on every run (and an identical payload, since the hash is
	// derived from it).
	const runs = 1000
	for _, fc := range fixtureCases() {
		t.Run(fc.name, func(t *testing.T) {
			value := fetchFixture(t, fc, loadFixtureBytes(t, fc.fixtureFile))
			wantJSON, wantHash, wantCount, err := Canonicalize(fc.kind, value, 1<<20)
			require.NoError(t, err)
			for run := range runs {
				gotJSON, gotHash, gotCount, err := Canonicalize(fc.kind, value, 1<<20)
				require.NoError(t, err)
				require.Equal(t, wantJSON, gotJSON, "run %d changed the canonical payload", run)
				require.Equal(t, wantHash, gotHash, "run %d changed the canonical hash", run)
				require.Equal(t, wantCount, gotCount, "run %d changed the record count", run)
			}
		})
	}
}
