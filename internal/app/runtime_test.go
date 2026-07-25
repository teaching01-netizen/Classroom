package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/service"
)

type runtimeWorker struct {
	mu      sync.Mutex
	running bool
}

func (w *runtimeWorker) Run(ctx context.Context) {
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()
	<-ctx.Done()
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
}

func (w *runtimeWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

func TestStartBackgroundRuntime_ServerlessWaitsForActivity(t *testing.T) {
	worker := &runtimeWorker{}
	controller := service.NewActivityController(time.Minute, []service.ManagedWorker{worker}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartBackgroundRuntime(ctx, true, BackgroundRuntime{Controller: controller})
	require.Never(t, worker.IsRunning, 20*time.Millisecond, time.Millisecond)
	controller.RecordActivity()
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
}

func TestStartBackgroundRuntime_NormalModeStartsImmediately(t *testing.T) {
	worker := &runtimeWorker{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartBackgroundRuntime(ctx, false, BackgroundRuntime{AlwaysOn: []service.ManagedWorker{worker}})
	require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
}

func TestStartBackgroundRuntime_ServerlessStartsPersistentListenerOnly(t *testing.T) {
	listener := &runtimeWorker{}
	scheduler := &runtimeWorker{}
	ctx, cancel := context.WithCancel(context.Background())
	done := StartBackgroundRuntime(ctx, true, BackgroundRuntime{
		Persistent: []service.ManagedWorker{listener},
		AlwaysOn:   []service.ManagedWorker{scheduler},
	})

	require.Eventually(t, listener.IsRunning, time.Second, time.Millisecond)
	require.Never(t, scheduler.IsRunning, 20*time.Millisecond, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background runtime did not stop")
	}
	require.False(t, listener.IsRunning())
}

func TestStartBackgroundRuntimeRepeatedStartStopLeavesNoWorkersRunning(t *testing.T) {
	for iteration := range 25 {
		persistent := &runtimeWorker{}
		alwaysOn := &runtimeWorker{}
		ctx, cancel := context.WithCancel(context.Background())
		done := StartBackgroundRuntime(ctx, false, BackgroundRuntime{
			Persistent: []service.ManagedWorker{persistent},
			AlwaysOn:   []service.ManagedWorker{alwaysOn},
		})
		require.Eventuallyf(
			t,
			func() bool {
				return persistent.IsRunning() && alwaysOn.IsRunning()
			},
			time.Second,
			time.Millisecond,
			"workers did not start on iteration %d",
			iteration,
		)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("workers leaked on iteration %d", iteration)
		}
		require.False(t, persistent.IsRunning())
		require.False(t, alwaysOn.IsRunning())
	}
}
