# Testing Patterns

**Analysis Date:** 2026-06-04

## Test Framework

**Runner:**
- Go: Standard `go test` package
- React: Vitest or Jest (based on test file patterns)

**Assertion Library:**
- Go: Standard testing package
- React: Expect/Jest matchers

**Run Commands:**
```bash
go test ./...                    # Run all Go tests
go test ./internal/warwick/...   # Run Warwick client tests
cd web && npm test               # Run frontend tests
```

## Test File Organization

**Location:**
- Go: Co-located with source files (e.g., `cache_test.go` next to `cache.go`)
- React: Separate `__tests__` directory (`web/src/__tests__/`)

**Naming:**
- Go: `*_test.go` (e.g., `auth_test.go`, `session_pool_test.go`)
- React: `*.test.js` or `*.test.jsx` (e.g., `useAbsenceDashboard.test.js`)

**Structure:**
```
internal/
├── warwick/
│   ├── auth.go
│   └── auth_test.go           # Co-located tests
└── db/
    ├── repository.go
    └── attendance_report_repository_test.go

web/src/
├── hooks/
│   └── useCourses.js
├── __tests__/
│   └── useAbsenceDashboard.test.js  # Separate test directory
```

## Test Structure

**Go Tests:**
```go
func TestGetCourses(t *testing.T) {
    // Arrange
    client := NewClassroomClient(auth, cache)
    
    // Act
    courses, err := client.GetCourses()
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(courses) == 0 {
        t.Error("expected courses, got none")
    }
}
```

**React Tests:**
```javascript
describe('useCourses', () => {
    it('fetches courses on mount', async () => {
        // Arrange
        renderHook(() => useCourses());
        
        // Act
        await waitFor(() => {
            expect(result.current.courses).toBeDefined();
        });
        
        // Assert
        expect(result.current.courses).toHaveLength(0);
    });
});
```

## Mocking

**Framework:**
- Go: Manual mocking via interfaces
- React: Jest mocks (implied by test patterns)

**Patterns:**
```go
// Go: Interface-based mocking
type MockSessionCheckinRepository struct {
    students map[string][]domain.StudentCheckin
}

func (m *MockSessionCheckinRepository) GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error) {
    if students, ok := m.students[sessionID]; ok {
        return students, nil
    }
    return nil, nil
}
```

**What to Mock:**
- External API calls (Warwick)
- Database operations
- Time-dependent functions

**What NOT to Mock:**
- Domain models
- Pure functions
- Cache operations (test with real cache)

## Fixtures and Factories

**Test Data:**
```go
func createTestCourse() domain.CourseSummary {
    return domain.CourseSummary{
        CourseID:      "test-course-1",
        Name:          "Test Course",
        StartDate:     "2026-01-01",
        EndDate:       "2026-06-30",
        EnrolledCount: 25,
        Status:        domain.CourseStatusActive,
    }
}
```

**Location:**
- Go: Test files or `testdata/` directory
- React: Mock data in test files

## Coverage

**Requirements:** None enforced

**View Coverage:**
```bash
go test -cover ./...              # Go coverage
cd web && npm test -- --coverage  # React coverage
```

## Test Types

**Unit Tests:**
- Scope: Individual functions and methods
- Approach: Test with mocked dependencies
- Examples: `internal/warwick/auth_test.go`, `internal/cache/cache_test.go`

**Integration Tests:**
- Scope: Multiple components working together
- Approach: Test with real database (if available)
- Examples: `internal/db/*_test.go`

**E2E Tests:**
- Framework: Not detected
- Scope: Full application workflows

## Common Patterns

**Async Testing:**
```go
func TestCacheRefresh(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    // Test async operation
    result, err := client.GetCoursesWithContext(ctx)
    if err != nil {
        t.Fatalf("async operation failed: %v", err)
    }
    // Assert result
}
```

**Error Testing:**
```go
func TestAuthExpired(t *testing.T) {
    // Arrange: Mock expired auth
    auth := &MockAuth{expired: true}
    client := NewClassroomClient(auth, nil)
    
    // Act
    _, err := client.GetCourses()
    
    // Assert
    if !errors.Is(err, domain.ErrAuthExpired) {
        t.Errorf("expected ErrAuthExpired, got %v", err)
    }
}
```

## Test Data Patterns

**Course Data:**
```go
var testCourses = []domain.CourseSummary{
    {CourseID: "1", Name: "Active Course", Status: domain.CourseStatusActive},
    {CourseID: "2", Name: "Finished Course", Status: domain.CourseStatusFinished},
    {CourseID: "3", Name: "Upcoming Course", Status: domain.CourseStatusUpcoming},
}
```

**Session Data:**
```go
var testSessions = []domain.SessionSummary{
    {SessionID: "s1", SessionNumber: 1, Name: "Week 1", Status: domain.SessionStatusDone},
    {SessionID: "s2", SessionNumber: 2, Name: "Week 2", Status: domain.SessionStatusActive},
}
```

**Student Data:**
```go
var testStudents = []domain.StudentCheckin{
    {StudentID: "st1", Name: "Alice", CheckedIn: true},
    {StudentID: "st2", Name: "Bob", CheckedIn: false},
}
```

---

*Testing analysis: 2026-06-04*
