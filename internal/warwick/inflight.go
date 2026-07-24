package warwick

import (
	"context"
	"fmt"
	"sync"
)

// inflightGroup coalesces identical work only for the lifetime of a producer.
// It deliberately has no completed-result storage: every call after a
// producer exits starts a new upstream read.
type inflightGroup struct {
	mu    sync.Mutex
	calls map[string]*inflightCall
}

type inflightCall struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	value any
	err   error

	waiters   int
	completed bool
}

// Do runs fn once for a key while at least one caller is waiting. The shared
// result is returned to all waiters. A caller that cancels receives its own
// context error; when the last waiter leaves, the producer is cancelled.
func (g *inflightGroup) Do(
	ctx context.Context,
	key string,
	fn func(context.Context) (any, error),
) (any, error, bool) {
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}

	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*inflightCall)
	}
	call, shared := g.calls[key]
	if !shared {
		producerCtx, cancel := context.WithCancel(context.Background())
		call = &inflightCall{
			ctx:    producerCtx,
			cancel: cancel,
			done:   make(chan struct{}),
		}
		g.calls[key] = call
	}
	call.waiters++
	g.mu.Unlock()

	if !shared {
		go g.run(key, call, fn)
	}

	select {
	case <-ctx.Done():
		g.leave(key, call)
		return nil, ctx.Err(), shared
	case <-call.done:
		return call.value, call.err, shared
	}
}

func (g *inflightGroup) run(key string, call *inflightCall, fn func(context.Context) (any, error)) {
	var value any
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("in-flight producer panic: %v", recovered)
			}
		}()
		value, err = fn(call.ctx)
	}()

	g.mu.Lock()
	call.value = value
	call.err = err
	call.completed = true
	if current := g.calls[key]; current == call {
		delete(g.calls, key)
	}
	close(call.done)
	g.mu.Unlock()

	call.cancel()
}

func (g *inflightGroup) leave(key string, call *inflightCall) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if call.completed || call.waiters == 0 {
		return
	}
	call.waiters--
	if call.waiters == 0 {
		if current := g.calls[key]; current == call {
			delete(g.calls, key)
		}
		call.cancel()
	}
}
