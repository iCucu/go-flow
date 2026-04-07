package goflow

import "sync"

// pool is a goroutine pool with an unbounded FIFO task queue.
// Workers are created on demand up to the concurrency cap and exit when the queue is empty.
// FIFO ordering ensures priority-sorted tasks are dequeued in priority order.
type pool struct {
	mu      sync.Mutex
	queue   []func()
	workers uint
	cap     uint
}

func newPool(concurrency uint) *pool {
	return &pool{cap: concurrency}
}

// Go enqueues f for execution. Never blocks the caller.
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

func (p *pool) run() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.workers--
			p.mu.Unlock()
			return
		}
		f := p.queue[0]
		p.queue[0] = nil // avoid retaining references
		p.queue = p.queue[1:]
		p.mu.Unlock()
		f()
	}
}
