package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ActivityRecorder is the request boundary used to signal business activity.
type ActivityRecorder interface {
	RecordActivity()
}

type ActivityLease interface {
	Finish(success bool)
}

type ActivityTracker interface {
	ActivityRecorder
	BeginActivity() ActivityLease
}

// ManagedWorker is a background loop whose lifetime is controlled by context.
type ManagedWorker interface {
	Run(context.Context)
}

// IdleHandler performs bounded cleanup after speculative workers stop.
type IdleHandler interface {
	HandleIdle(context.Context) error
}

// IdleHandlerFunc adapts a function to IdleHandler.
type IdleHandlerFunc func(context.Context) error

func (f IdleHandlerFunc) HandleIdle(ctx context.Context) error {
	return f(ctx)
}

type activityTimer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

type realActivityTimer struct {
	*time.Timer
}

func (t realActivityTimer) C() <-chan time.Time { return t.Timer.C }

// ActivityController owns the active/idle lifecycle of speculative workers.
type ActivityController struct {
	idleGrace    time.Duration
	workers      []ManagedWorker
	idleHandlers []IdleHandler
	activityCh   chan struct{}
	activitySeq  atomic.Uint64
	inFlight     atomic.Int64
	transitionMu sync.Mutex
	newTimer     func(time.Duration) activityTimer
}

type controllerActivityLease struct {
	controller *ActivityController
	once       sync.Once
}

func (l *controllerActivityLease) Finish(success bool) {
	l.once.Do(func() {
		l.controller.inFlight.Add(-1)
		if success {
			l.controller.RecordActivity()
		}
	})
}

func NewActivityController(idleGrace time.Duration, workers []ManagedWorker, idleHandlers []IdleHandler) *ActivityController {
	return &ActivityController{
		idleGrace:    idleGrace,
		workers:      append([]ManagedWorker(nil), workers...),
		idleHandlers: append([]IdleHandler(nil), idleHandlers...),
		activityCh:   make(chan struct{}, 1),
		newTimer: func(duration time.Duration) activityTimer {
			return realActivityTimer{Timer: time.NewTimer(duration)}
		},
	}
}

// RecordActivity coalesces notifications so request handling never blocks.
func (c *ActivityController) RecordActivity() {
	c.activitySeq.Add(1)
	select {
	case c.activityCh <- struct{}{}:
	default:
	}
}

func (c *ActivityController) BeginActivity() ActivityLease {
	c.transitionMu.Lock()
	c.inFlight.Add(1)
	c.transitionMu.Unlock()
	return &controllerActivityLease{controller: c}
}

// Run blocks until ctx is cancelled.
func (c *ActivityController) Run(ctx context.Context) {
	var cancelWorkers context.CancelFunc
	var workerWG sync.WaitGroup
	var idleTimer activityTimer
	var idleTimerC <-chan time.Time
	var handledActivity uint64
	var generation uint64

	stopWorkers := func() {
		if cancelWorkers == nil {
			return
		}
		cancelWorkers()
		workerWG.Wait()
		cancelWorkers = nil
	}
	defer func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		stopWorkers()
	}()

	resetIdleTimer := func() {
		if idleTimer == nil {
			idleTimer = c.newTimer(c.idleGrace)
			idleTimerC = idleTimer.C()
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C():
			default:
			}
		}
		idleTimer.Reset(c.idleGrace)
		idleTimerC = idleTimer.C()
	}

	runIdleHandlers := func(handlerCtx context.Context) {
		for index, handler := range c.idleHandlers {
			if handler == nil {
				continue
			}
			if err := handler.HandleIdle(handlerCtx); err != nil {
				slog.Warn("serverless_idle_handler_failed", "handler_index", index, "error", err)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			stopWorkers()
			runIdleHandlers(context.Background())
			return
		case <-c.activityCh:
			handledActivity = c.activitySeq.Load()
			resetIdleTimer()
			if cancelWorkers != nil {
				continue
			}
			workerCtx, workerCancel := context.WithCancel(ctx)
			cancelWorkers = workerCancel
			generation++
			for _, worker := range c.workers {
				if worker == nil {
					continue
				}
				workerWG.Add(1)
				go runManagedWorker(workerCtx, worker, &workerWG)
			}
			slog.Info("serverless_runtime_active", "generation", generation)
		case <-idleTimerC:
			expiringActivity := handledActivity
			idleTimerC = nil
			c.transitionMu.Lock()
			if c.inFlight.Load() > 0 || c.activitySeq.Load() != expiringActivity {
				c.transitionMu.Unlock()
				resetIdleTimer()
				continue
			}
			stopWorkers()
			if c.activitySeq.Load() != expiringActivity {
				c.transitionMu.Unlock()
				slog.Info("serverless_idle_aborted_new_activity")
				continue
			}
			runIdleHandlers(ctx)
			c.transitionMu.Unlock()
			slog.Info("serverless_runtime_idle", "last_activity_sequence", expiringActivity)
		}
	}
}

func runManagedWorker(ctx context.Context, worker ManagedWorker, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("serverless_worker_panicked", "error", recovered)
		}
	}()
	worker.Run(ctx)
}
