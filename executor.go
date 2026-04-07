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
	// Run freezes the TaskFlow and begins asynchronous execution.
	Run(tf *TaskFlow) Executor
	// Wait blocks until all tasks (including subflows) have completed.
	Wait()
}

type executor struct {
	pool *pool
	wg   sync.WaitGroup // tracks all tasks across all graphs
}

// NewExecutor creates an Executor with the given concurrency limit.
// Concurrency controls the maximum number of goroutines executing tasks simultaneously.
func NewExecutor(concurrency uint) Executor {
	if concurrency == 0 {
		panic("executor concurrency cannot be zero")
	}
	return &executor{
		pool: newPool(concurrency),
	}
}

// Run freezes the TaskFlow (preventing further task additions) and begins execution.
func (e *executor) Run(tf *TaskFlow) Executor {
	tf.frozen = true
	e.scheduleGraph(nil, tf.graph)
	return e
}

// Wait blocks until every scheduled task has finished.
func (e *executor) Wait() {
	e.wg.Wait()
}

// scheduleGraph sets up a graph, dispatches its entry nodes, and blocks until
// all nodes in the graph complete. If the graph is canceled (due to a panic),
// the cancellation propagates to the parent graph.
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

// schedule submits a single node for execution via the worker pool.
// Skipped if the owning graph has been canceled.
func (e *executor) schedule(n *node) {
	if n.g.canceled.Load() {
		return
	}
	e.wg.Add(1)
	n.g.wg.Add(1)
	e.pool.Go(func() { e.invokeNode(n) })
}

// invokeNode dispatches to the appropriate handler based on node type.
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

// invokeStatic executes an Operator node (static task or user-defined Operator).
func (e *executor) invokeStatic(n *node, p *staticData) {
	defer e.staticFinished(n)

	if n.g.canceled.Load() {
		return
	}
	n.state.Store(int32(nodeRunning))
	e.safeCall(n, p.op.Compute)
	n.state.Store(int32(nodeFinished))
}

// invokeSubflow creates a fresh sub-graph, invokes the user callback to populate it,
// then recursively schedules and waits for the sub-graph to complete.
func (e *executor) invokeSubflow(n *node, p *subflowData) {
	defer e.staticFinished(n)

	if n.g.canceled.Load() {
		return
	}
	n.state.Store(int32(nodeRunning))

	sg := newGraph(n.name)
	sf := &Subflow{g: sg}
	e.safeCall(n, func() { p.handle(sf) })
	p.lastGraph = sg // retained for DOT visualization

	if !n.g.canceled.Load() {
		e.scheduleGraph(n.g, sg)
	}

	n.state.Store(int32(nodeFinished))
}

// invokeCondition runs the predicate and schedules the chosen successor.
// The node is rearmed BEFORE scheduling the successor to avoid a race:
// the successor's drop() must see a valid joinCounter on this node.
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

	n.rearm()
	n.state.Store(int32(nodeIdle))
	e.schedule(target)
}

// staticFinished is the deferred cleanup for static and subflow nodes.
// Order matters: drop -> schedule successors -> rearm -> reset state -> wg.Done.
func (e *executor) staticFinished(n *node) {
	n.drop()
	e.scheduleSuccessors(n)
	n.rearm()
	n.state.Store(int32(nodeIdle))
	n.g.wg.Done()
	e.wg.Done()
}

// conditionFinished is the deferred cleanup for condition nodes.
// Rearm and state reset are done in invokeCondition before scheduling the successor.
func (e *executor) conditionFinished(n *node) {
	n.g.wg.Done()
	e.wg.Done()
}

// scheduleSuccessors checks each successor and schedules those whose dependencies
// are all satisfied. CAS on state prevents double-scheduling when multiple
// predecessors complete concurrently.
func (e *executor) scheduleSuccessors(n *node) {
	candidates := make([]*node, 0, len(n.successors))
	for _, succ := range n.successors {
		if succ.recyclable() && succ.state.CompareAndSwap(int32(nodeIdle), int32(nodeWaiting)) {
			candidates = append(candidates, succ)
		}
	}
	sortByPriority(candidates)
	for _, c := range candidates {
		e.schedule(c)
	}
}

// safeCall executes f and recovers from any panic, marking the graph as canceled.
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

// safeCallCondition is like safeCall but returns the predicate's uint result.
// On panic, returns 0 (the caller checks canceled before using the result).
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

// sortByPriority sorts nodes by priority (lower value = higher priority).
func sortByPriority(nodes []*node) {
	if len(nodes) <= 1 {
		return
	}
	slices.SortFunc(nodes, func(a, b *node) int {
		return cmp.Compare(a.priority, b.priority)
	})
}
