package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

// persistJob is a single report waiting to be written to the DB.
type persistJob struct {
	CourseID string
	Report   *domain.CourseAttendanceReport
}

// ReportPersister writes attendance reports to memory first, then
// asynchronously persists them to the DB. On overflow (queue full),
// the newest report is dropped — it's already in memory and will be
// persisted on the next tick.
//
// Concurrency model:
//   - Enqueue is non-blocking and safe for concurrent callers.
//   - Run starts a single consumer goroutine (one DB write at a time).
//   - Flush drains the queue on shutdown with a caller-supplied timeout.
type ReportPersister struct {
	repo      db.AttendanceReportRepository
	cache     *cache.Cache
	queue     chan persistJob
	queueSize int

	dropCount atomic.Uint64
}

// NewReportPersister creates a persister with the given queue capacity.
// queueSize=100 is the production default (handles ~5s of burst at 20 reports/s).
func NewReportPersister(repo db.AttendanceReportRepository, c *cache.Cache, queueSize int) *ReportPersister {
	if queueSize <= 0 {
		queueSize = 100
	}
	return &ReportPersister{
		repo:      repo,
		cache:     c,
		queue:     make(chan persistJob, queueSize),
		queueSize: queueSize,
	}
}

// Enqueue adds a report to the persist queue. Non-blocking: if the queue
// is full, the report is silently dropped (it's already in memory). This
// is the "drop-newest" strategy — the caller already has the report in
// the cache and will serve it from memory until the next successful persist.
func (p *ReportPersister) Enqueue(courseID string, report *domain.CourseAttendanceReport) {
	select {
	case p.queue <- persistJob{CourseID: courseID, Report: report}:
	default:
		p.dropCount.Add(1)
		slog.Debug("report_persister_queue_full_dropped", "course_id", courseID)
	}
}

// Run starts the single consumer goroutine. Blocks until ctx is cancelled.
func (p *ReportPersister) Run(ctx context.Context) {
	slog.Info("report_persister_started", "queue_size", p.queueSize)

	for {
		select {
		case <-ctx.Done():
			slog.Info("report_persister_stopped", "dropped", p.dropCount.Load())
			return
		case job := <-p.queue:
			p.persist(job)
		}
	}
}

// persist writes one report to the DB.
func (p *ReportPersister) persist(job persistJob) {
	payload, err := json.Marshal(job.Report)
	if err != nil {
		slog.Warn("report_persister_marshal_failed",
			"course_id", job.CourseID, "error", err)
		return
	}

	rep := &db.AttendanceReport{
		CourseID:   job.CourseID,
		ComputedAt: job.Report.ComputedAt,
		Threshold:  job.Report.Threshold,
		DurationMs: job.Report.DurationMs,
		Payload:    payload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := p.repo.Upsert(ctx, rep); err != nil {
		slog.Warn("report_persister_upsert_failed",
			"course_id", job.CourseID, "error", err)
		return
	}
	slog.Debug("report_persister_persisted", "course_id", job.CourseID)
}

// Flush drains the remaining queue entries. Called during shutdown with
// a timeout. Returns an error if the timeout is exceeded before the
// queue is fully drained.
func (p *ReportPersister) Flush(ctx context.Context) error {
	slog.Info("report_persister_flushing", "remaining", len(p.queue))

	for {
		select {
		case job := <-p.queue:
			p.persist(job)
		case <-ctx.Done():
			slog.Warn("report_persister_flush_timeout",
				"remaining", len(p.queue))
			return ctx.Err()
		default:
			slog.Info("report_persister_flush_complete")
			return nil
		}
	}
}

// DropCount returns the number of reports dropped due to a full queue.
// Exposed for Prometheus metrics.
func (p *ReportPersister) DropCount() uint64 {
	return p.dropCount.Load()
}

// QueueDepth returns the current number of pending jobs in the queue.
// Exposed for Prometheus metrics.
func (p *ReportPersister) QueueDepth() int {
	return len(p.queue)
}
