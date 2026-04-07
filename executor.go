package goflow

import (
	"cmp"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"
)

// Executor schedules and executes a TaskFlow.
type Executor interface {
	Run(tf *TaskFlow) Executor
	Wait()
}

type executor struct {
	pool *pool
	wg   sync.WaitGroup
}

// NewExecutor creates an Executor with the given concurrency limit.
func NewExecutor(concurrency uint) Executor {
	if concurrency == 0 {
		panic("executor concurrency cannot be zero")
	}
	return &executor{
		pool: newPool(concurrency),
	}
}

func (e *executor) Run(tf *TaskFlow) Executor {
	tf.frozen = true
	e.scheduleGraph(nil, tf.graph)
	return e
}

func (e *executor) Wait() {
	e.wg.Wait()
}

func (e *executor) scheduleGraph(parent *graph, g *graph) {
	g.setup()
	sortByPriority(g.entries)

	for _, n := range g.entries {
		e.schedule(n)
	}

	g.wg.Wait()

	if g.canceled.Load() && parent != nil {
		parent.canceled.Store(true)
	}
}

func (e *executor) schedule(n *node) {
	if n.g.canceled.Load() {
		return
	}
	e.wg.Add(1)
	n.g.wg.Add(1)
	e.pool.Go(func() { e.invokeNode(n) })
}

func (e *executor) invokeNode(n *node) {
	switch p := n.ptr.(type) {
	case *staticData:
		e.invokeStatic(n, p)
	case *subflowData:
		e.invokeSubflow(n, p)
	case *conditionData:
		e.invokeCondition(n, p)
	default:
		panic("unsupported node type")
	}
}

func (e *executor) invokeStatic(n *node, p *staticData) {
	defer e.staticFinished(n)

	if n.g.canceled.Load() {
		return
	}
	n.state.Store(int32(nodeRunning))
	e.safeCall(n, p.op.Compute)
	n.state.Store(int32(nodeFinished))
}

func (e *executor) invokeSubflow(n *node, p *subflowData) {
	defer e.staticFinished(n)

	if n.g.canceled.Load() {
		return
	}
	n.state.Store(int32(nodeRunning))

	sg := newGraph(n.name)
	sf := &Subflow{g: sg}
	e.safeCall(n, func() { p.handle(sf) })
	p.lastGraph = sg

	if !n.g.canceled.Load() {
		e.scheduleGraph(n.g, sg)
	}

	n.state.Store(int32(nodeFinished))
}

func (e *executor) invokeCondition(n *node, p *conditionData) {
	defer e.conditionFinished(n)

	if n.g.canceled.Load() {
		return
	}
	n.state.Store(int32(nodeRunning))
	choice := e.safeCallCondition(n, p.handle)
	n.state.Store(int32(nodeFinished))

	if n.g.canceled.Load() {
		return
	}

	target, ok := p.mapper[choice]
	if !ok {
		panic(fmt.Sprintf("condition %q returned %d but only %d successors mapped", n.name, choice, len(p.mapper)))
	}

	// Re-arm self BEFORE scheduling successor so that the successor's
	// drop() sees a valid joinCounter when decrementing this node.
	n.rearm()
	n.state.Store(int32(nodeIdle))
	e.schedule(target)
}

// staticFinished is the defer handler for static and subflow nodes.
func (e *executor) staticFinished(n *node) {
	n.drop()
	e.scheduleSuccessors(n)
	n.rearm()
	n.state.Store(int32(nodeIdle))
	n.g.wg.Done()
	e.wg.Done()
}

// conditionFinished is the defer handler for condition nodes.
func (e *executor) conditionFinished(n *node) {
	// Re-arm and state reset already done before schedule in invokeCondition,
	// or done here if the condition was canceled / panicked.
	n.g.wg.Done()
	e.wg.Done()
}

func (e *executor) scheduleSuccessors(n *node) {
	candidates := make([]*node, 0, len(n.successors))
	for _, succ := range n.successors {
		// Use CAS to atomically claim the right to schedule this successor.
		// This prevents double-scheduling when multiple predecessors finish concurrently.
		if succ.recyclable() && succ.state.CompareAndSwap(int32(nodeIdle), int32(nodeWaiting)) {
			candidates = append(candidates, succ)
		}
	}
	sortByPriority(candidates)
	for _, c := range candidates {
		e.schedule(c)
	}
}

func (e *executor) safeCall(n *node, f func()) {
	defer func() {
		if r := recover(); r != nil {
			n.g.canceled.Store(true)
			log.Printf("[go-flow] graph %q canceled: task %q panicked: %v\n%s",
				n.g.name, n.name, r, debug.Stack())
		}
	}()
	f()
}

func (e *executor) safeCallCondition(n *node, f func() uint) uint {
	var result uint
	func() {
		defer func() {
			if r := recover(); r != nil {
				n.g.canceled.Store(true)
				log.Printf("[go-flow] graph %q canceled: condition %q panicked: %v\n%s",
					n.g.name, n.name, r, debug.Stack())
			}
		}()
		result = f()
	}()
	return result
}

func sortByPriority(nodes []*node) {
	if len(nodes) <= 1 {
		return
	}
	slices.SortFunc(nodes, func(a, b *node) int {
		return cmp.Compare(a.priority, b.priority)
	})
}
