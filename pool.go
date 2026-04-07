package goflow

import "sync"

// pool is a bounded goroutine pool with an unbounded FIFO task queue.
//
// Design properties:
//   - Go() never blocks the caller, preventing deadlocks when tasks submit sub-tasks
//   - Workers are created on demand (up to cap) and exit when the queue drains
//   - FIFO ordering preserves the priority sort done by the scheduler
type pool struct {
	mu      sync.Mutex
	queue   []func()
	workers uint // currently active worker goroutines
	cap     uint // maximum concurrent workers
}

func newPool(concurrency uint) *pool {
	return &pool{cap: concurrency}
}

// Go enqueues f for execution. If workers < cap, a new worker goroutine is spawned.
// The caller is never blocked, even if all workers are busy.
func (p *pool) Go(f func()) {
	p.mu.Lock()
	p.queue = append(p.queue, f)
	if p.workers < p.cap {
		p.workers++
		p.mu.Unlock()
		go p.run()
	} else {
		p.mu.Unlock()
	}
}

// run is the worker loop. It dequeues and executes tasks until the queue is empty,
// then decrements the worker count and exits.
func (p *pool) run() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.workers--
			p.mu.Unlock()
			return
		}
		f := p.queue[0]
		p.queue[0] = nil // avoid retaining closure references
		p.queue = p.queue[1:]
		p.mu.Unlock()
		f()
	}
}
