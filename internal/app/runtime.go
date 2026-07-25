package app

import (
	"context"
	"log/slog"
	"sync"

	"qr-command-center/internal/service"
)

type managedWorkerFunc func(context.Context)

func (run managedWorkerFunc) Run(ctx context.Context) {
	run(ctx)
}

// BackgroundRuntime groups lifecycle policy without coupling it to server wiring.
type BackgroundRuntime struct {
	Controller *service.ActivityController
	Persistent []service.ManagedWorker
	AlwaysOn   []service.ManagedWorker
}

// StartBackgroundRuntime selects demand-driven or legacy always-on execution.
func StartBackgroundRuntime(ctx context.Context, serverless bool, runtime BackgroundRuntime) <-chan struct{} {
	done := make(chan struct{})
	var workers sync.WaitGroup
	startWorkers := func(items []service.ManagedWorker) {
		for _, worker := range items {
			if worker == nil {
				continue
			}
			workers.Add(1)
			go func(worker service.ManagedWorker) {
				defer workers.Done()
				runBackgroundWorker(ctx, worker)
			}(worker)
		}
	}
	startWorkers(runtime.Persistent)
	if serverless {
		if runtime.Controller != nil {
			workers.Add(1)
			go func() {
				defer workers.Done()
				runtime.Controller.Run(ctx)
			}()
		}
	} else {
		startWorkers(runtime.AlwaysOn)
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
