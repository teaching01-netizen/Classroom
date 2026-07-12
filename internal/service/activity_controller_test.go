package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleWorker struct {
	mu      sync.Mutex
	runs    int
	running int
}

type observingIdleHandler struct {
	worker *lifecycleWorker
	called chan bool
}

type manualActivityTimer struct {
	ch    chan time.Time
	reset chan struct{}
}

func newManualActivityTimer() *manualActivityTimer {
	return &manualActivityTimer{ch: make(chan time.Time, 1), reset: make(chan struct{}, 10)}
}

func (t *manualActivityTimer) C() <-chan time.Time { return t.ch }
func (t *manualActivityTimer) Stop() bool          { return true }
func (t *manualActivityTimer) Reset(time.Duration) bool {
	t.reset <- struct{}{}
	return true
}
func (t *manualActivityTimer) Fire() { t.ch <- time.Now() }

type blockingStopWorker struct {
	lifecycleWorker
	stopRequested chan struct{}
	releaseStop   chan struct{}
	stopOnce      sync.Once
}

func (w *blockingStopWorker) Run(ctx context.Context) {
	w.mu.Lock()
	w.runs++
	w.running++
	w.mu.Unlock()

	<-ctx.Done()
	w.stopOnce.Do(func() { close(w.stopRequested) })
	<-w.releaseStop

	w.mu.Lock()
	w.running--
	w.mu.Unlock()
}

func (h *observingIdleHandler) HandleIdle(context.Context) error {
	h.called <- !h.worker.IsRunning()
	return nil
}

func (w *lifecycleWorker) Run(ctx context.Context) {
	w.mu.Lock()
	w.runs++
	w.running++
	w.mu.Unlock()

	<-ctx.Done()

	w.mu.Lock()
	w.running--
	w.mu.Unlock()
}

func (w *lifecycleWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running > 0
}

func (w *lifecycleWorker) RunCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runs
}

func TestActivityController_FirstActivityStartsWorkers(t *testing.T) {
	worker := &lifecycleWorker{}
	controller := NewActivityController(time.Minute, []ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	require.Never(t, worker.IsRunning, 20*time.Millisecond, time.Millisecond)
	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	require.Equal(t, 1, worker.RunCount())
}

func TestActivityController_InactivityStopsWorkers(t *testing.T) {
	worker := &lifecycleWorker{}
	controller := NewActivityController(25*time.Millisecond, []ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return !worker.IsRunning() }, time.Second, time.Millisecond)
}

func TestActivityController_RepeatedActivityExtendsOneGeneration(t *testing.T) {
	worker := &lifecycleWorker{}
	controller := NewActivityController(80*time.Millisecond, []ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	controller.RecordActivity()
	time.Sleep(45 * time.Millisecond)

	require.True(t, worker.IsRunning(), "activity must extend the original idle deadline")
	require.Equal(t, 1, worker.RunCount(), "activity must not duplicate a running generation")
	require.Eventually(t, func() bool { return !worker.IsRunning() }, time.Second, time.Millisecond)
}

func TestActivityController_ActivityAfterIdleStartsNewGeneration(t *testing.T) {
	worker := &lifecycleWorker{}
	controller := NewActivityController(25*time.Millisecond, []ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return !worker.IsRunning() }, time.Second, time.Millisecond)

	controller.RecordActivity()
	require.Eventually(t, func() bool {
		return worker.IsRunning() && worker.RunCount() == 2
	}, time.Second, time.Millisecond)
}

func TestActivityController_ShutdownStopsWorkersWithoutRestart(t *testing.T) {
	worker := &lifecycleWorker{}
	controller := NewActivityController(time.Minute, []ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.Run(ctx)
	}()

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	cancel()
	require.Eventually(t, func() bool { return !worker.IsRunning() }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.Equal(t, 1, worker.RunCount())
}

func TestActivityController_StopsWorkersBeforeIdleHandlers(t *testing.T) {
	worker := &lifecycleWorker{}
	handler := &observingIdleHandler{worker: worker, called: make(chan bool, 1)}
	controller := NewActivityController(25*time.Millisecond, []ManagedWorker{worker}, []IdleHandler{handler})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	require.True(t, <-handler.called)
}

func TestActivityController_NewActivityAbortsDestructiveIdleCleanup(t *testing.T) {
	timer := newManualActivityTimer()
	worker := &blockingStopWorker{
		stopRequested: make(chan struct{}),
		releaseStop:   make(chan struct{}),
	}
	idleCalled := make(chan struct{}, 1)
	controller := NewActivityController(time.Minute, []ManagedWorker{worker}, []IdleHandler{
		IdleHandlerFunc(func(context.Context) error {
			idleCalled <- struct{}{}
			return nil
		}),
	})
	controller.newTimer = func(time.Duration) activityTimer { return timer }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
	timer.Fire()
	require.Eventually(t, func() bool {
		select {
		case <-worker.stopRequested:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	controller.RecordActivity()
	close(worker.releaseStop)
	require.Eventually(t, func() bool { return worker.RunCount() == 2 }, time.Second, time.Millisecond)
	select {
	case <-idleCalled:
		t.Fatal("newer activity must abort idle cleanup")
	default:
	}
}

func TestActivityController_InFlightRequestDefersIdleCleanup(t *testing.T) {
	timer := newManualActivityTimer()
	idleCalled := make(chan struct{}, 1)
	controller := NewActivityController(time.Minute, nil, []IdleHandler{
		IdleHandlerFunc(func(context.Context) error {
			idleCalled <- struct{}{}
			return nil
		}),
	})
	controller.newTimer = func(time.Duration) activityTimer { return timer }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	controller.RecordActivity()
	lease := controller.BeginActivity()
	timer.Fire()
	require.Eventually(t, func() bool {
		select {
		case <-timer.reset:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	select {
	case <-idleCalled:
		t.Fatal("idle cleanup must not run during an admitted request")
	default:
	}

	lease.Finish(false)
	timer.Fire()
	require.Eventually(t, func() bool {
		select {
		case <-idleCalled:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}
