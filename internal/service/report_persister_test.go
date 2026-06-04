package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

// stubAttendanceReportRepo records Upsert calls for assertion.
type stubAttendanceReportRepo struct {
	mu      sync.Mutex
	upserts []db.AttendanceReport
	err     error
}

func (r *stubAttendanceReportRepo) Upsert(_ context.Context, report *db.AttendanceReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.upserts = append(r.upserts, *report)
	return nil
}

func (r *stubAttendanceReportRepo) Get(_ context.Context, courseID string) (*db.AttendanceReport, error) {
	return nil, nil
}

func (r *stubAttendanceReportRepo) ListRecent(_ context.Context, limit int) ([]*db.AttendanceReport, error) {
	return nil, nil
}

func (r *stubAttendanceReportRepo) upsertCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.upserts)
}

func (r *stubAttendanceReportRepo) lastUpsert() *db.AttendanceReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.upserts) == 0 {
		return nil
	}
	return &r.upserts[len(r.upserts)-1]
}

// makeReport is a helper to build a minimal CourseAttendanceReport.
func makeReport(courseID string) *domain.CourseAttendanceReport {
	return &domain.CourseAttendanceReport{
		CourseID:   courseID,
		CourseName: "Test Course",
		Sessions:   []domain.SessionSummary{{SessionID: "s1", Status: domain.SessionStatusDone}},
		Students:   []domain.StudentAttendance{},
		Threshold:  4,
		ComputedAt: time.Now().UTC(),
		DurationMs: 42,
	}
}

// --- Enqueue tests ---

func TestReportPersister_Enqueue_Basic(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	p.Enqueue("c1", makeReport("c1"))

	assert.Equal(t, 1, p.QueueDepth(), "queue must contain exactly 1 job")
	assert.Equal(t, uint64(0), p.DropCount(), "no drops on a non-full queue")
}

func TestReportPersister_Enqueue_DropNewestWhenFull(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 2) // tiny queue

	p.Enqueue("c1", makeReport("c1"))
	p.Enqueue("c2", makeReport("c2"))
	p.Enqueue("c3", makeReport("c3")) // must be dropped

	assert.Equal(t, 2, p.QueueDepth(), "queue must cap at capacity")
	assert.Equal(t, uint64(1), p.DropCount(), "one drop when queue is full")
}

func TestReportPersister_Enqueue_NonBlocking(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 1)

	// Fill the queue.
	p.Enqueue("c1", makeReport("c1"))

	// This must not block even though the queue is full.
	done := make(chan struct{})
	go func() {
		p.Enqueue("c2", makeReport("c2"))
		close(done)
	}()

	select {
	case <-done:
		// good — Enqueue returned immediately
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Enqueue must be non-blocking even when queue is full")
	}
}

// --- Run tests ---

func TestReportPersister_Run_PersistsToDB(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	report := makeReport("c1")
	p.Enqueue("c1", report)

	// Give the consumer goroutine time to process.
	require.Eventually(t, func() bool {
		return repo.upsertCount() == 1
	}, 2*time.Second, 10*time.Millisecond, "persister must write to DB")

	cancel()
	<-done

	upsert := repo.lastUpsert()
	require.NotNil(t, upsert)
	assert.Equal(t, "c1", upsert.CourseID)
	assert.Equal(t, report.Threshold, upsert.Threshold)
	assert.Equal(t, report.DurationMs, upsert.DurationMs)
	// Payload must be valid JSON.
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(upsert.Payload, &raw))
}

func TestReportPersister_Run_ProcessesMultipleJobs(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	for i := 0; i < 5; i++ {
		p.Enqueue("c"+string(rune('0'+i)), makeReport("c"+string(rune('0'+i))))
	}

	require.Eventually(t, func() bool {
		return repo.upsertCount() == 5
	}, 2*time.Second, 10*time.Millisecond, "persister must process all 5 jobs")

	cancel()
	<-done
}

func TestReportPersister_Run_StopOnCancel(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run must return promptly after context cancel")
	}
}

// --- Flush tests ---

func TestReportPersister_Flush_DrainsAllJobs(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	// Enqueue 3 jobs without starting Run (so they sit in the queue).
	p.Enqueue("c1", makeReport("c1"))
	p.Enqueue("c2", makeReport("c2"))
	p.Enqueue("c3", makeReport("c3"))

	err := p.Flush(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, repo.upsertCount(), "Flush must persist all queued jobs")
	assert.Equal(t, 0, p.QueueDepth(), "queue must be empty after Flush")
}

func TestReportPersister_Flush_EmptyQueueReturnsNil(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	err := p.Flush(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, repo.upsertCount(), "Flush on empty queue must not call Upsert")
}

func TestReportPersister_Flush_TimeoutReturnsError(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	// Enqueue a job that will block (repo returns error, but that doesn't block).
	// Use a very short timeout to force a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // ensure timeout fires

	p.Enqueue("c1", makeReport("c1"))

	err := p.Flush(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Flush must return deadline exceeded on timeout")
}

// --- DropCount / QueueDepth ---

func TestReportPersister_DropCount_IncrementsOnOverflow(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 1)

	p.Enqueue("c1", makeReport("c1"))
	assert.Equal(t, uint64(0), p.DropCount())

	p.Enqueue("c2", makeReport("c2"))
	assert.Equal(t, uint64(1), p.DropCount())

	p.Enqueue("c3", makeReport("c3"))
	assert.Equal(t, uint64(2), p.DropCount())
}

func TestReportPersister_QueueDepth_Empty(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)
	assert.Equal(t, 0, p.QueueDepth())
}

func TestReportPersister_QueueDepth_AfterEnqueue(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 10)

	p.Enqueue("c1", makeReport("c1"))
	assert.Equal(t, 1, p.QueueDepth())

	p.Enqueue("c2", makeReport("c2"))
	assert.Equal(t, 2, p.QueueDepth())
}

// --- NewReportPersister defaults ---

func TestReportPersister_DefaultQueueSize(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), 0) // 0 must use default
	assert.Equal(t, 100, p.queueSize, "queueSize=0 must default to 100")
}

func TestReportPersister_NegativeQueueSize(t *testing.T) {
	repo := &stubAttendanceReportRepo{}
	p := NewReportPersister(repo, cache.New(), -5)
	assert.Equal(t, 100, p.queueSize, "negative queueSize must default to 100")
}
