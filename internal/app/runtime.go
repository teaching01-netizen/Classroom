package app

import (
	"context"
	"log/slog"
	"sync"

	"qr-command-center/internal/service"
)

// BackgroundRuntime groups lifecycle policy without coupling it to server wiring.
type BackgroundRuntime struct {
	Controller *service.ActivityController
	AlwaysOn   []service.ManagedWorker
}

// StartBackgroundRuntime selects demand-driven or legacy always-on execution.
func StartBackgroundRuntime(ctx context.Context, serverless bool, runtime BackgroundRuntime) <-chan struct{} {
	done := make(chan struct{})
	if serverless {
		if runtime.Controller != nil {
			go func() {
				defer close(done)
				runtime.Controller.Run(ctx)
			}()
		} else {
			close(done)
		}
		return done
	}
	var workers sync.WaitGroup
	for _, worker := range runtime.AlwaysOn {
		if worker == nil {
			continue
		}
		workers.Add(1)
		go func(worker service.ManagedWorker) {
			defer workers.Done()
			runBackgroundWorker(ctx, worker)
		}(worker)
	}
	go func() {
		workers.Wait()
		close(done)
	}()
	return done
}

func runBackgroundWorker(ctx context.Context, worker service.ManagedWorker) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("background_worker_panicked", "error", recovered)
		}
	}()
	worker.Run(ctx)
}
