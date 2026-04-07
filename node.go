package goflow

import (
	"strconv"
	"sync/atomic"
	"time"
)

// nodeState represents the lifecycle state of a node.
// Transitions are managed via atomic CAS to avoid per-node mutexes.
type nodeState int32

const (
	nodeIdle     nodeState = iota + 1 // ready to be scheduled
	nodeWaiting                       // claimed by scheduleSuccessors, pending dispatch
	nodeRunning                       // currently executing
	nodeFinished                      // execution complete, about to trigger successors
)

// nodeType distinguishes the three kinds of DAG nodes.
type nodeType int

const (
	nodeTypeStatic    nodeType = iota // static task or Operator
	nodeTypeSubflow                   // dynamic subflow
	nodeTypeCondition                 // conditional branching
)

// node is the internal execution unit of the DAG.
//
// Key concurrency invariants:
//   - state transitions use atomic CAS (no mutex)
//   - joinCounter tracks how many non-condition predecessors have not yet completed
//   - a node is schedulable when joinCounter == 0 ("recyclable") AND state == idle
type node struct {
	name        string
	typ         nodeType
	successors  []*node      // downstream nodes that depend on this node
	dependents  []*node      // upstream nodes that this node depends on
	ptr         any          // payload: *staticData | *subflowData | *conditionData
	state       atomic.Int32 // current nodeState
	joinCounter atomic.Int32 // pending non-condition predecessor count
	g           *graph       // owning graph
	priority    TaskPriority
}

func newNode(name string) *node {
	if len(name) == 0 {
		name = "N_" + strconv.Itoa(time.Now().Nanosecond())
	}
	n := &node{
		name:       name,
		successors: make([]*node, 0, 4),
		dependents: make([]*node, 0, 4),
		priority:   NORMAL,
	}
	n.state.Store(int32(nodeIdle))
	return n
}

// recyclable returns true when all non-condition predecessors have completed.
func (n *node) recyclable() bool {
	return n.joinCounter.Load() == 0
}

// setup initializes the node for a new execution round.
// It resets the state to idle and counts non-condition predecessors into joinCounter.
// Condition predecessors are excluded because they schedule successors directly
// rather than through the joinCounter mechanism.
func (n *node) setup() {
	n.state.Store(int32(nodeIdle))
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			continue
		}
		n.joinCounter.Add(1)
	}
}

// drop decrements each successor's joinCounter after this node completes.
// Condition nodes skip this because they select a single successor to schedule directly.
func (n *node) drop() {
	if n.typ == nodeTypeCondition {
		return
	}
	for _, succ := range n.successors {
		succ.joinCounter.Add(-1)
	}
}

// rearm re-establishes the joinCounter so the node can be re-executed in cyclic flows.
// Called after a node finishes to prepare for potential re-scheduling by a condition loop.
func (n *node) rearm() {
	var cnt int32
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			continue
		}
		cnt++
	}
	n.joinCounter.Store(cnt)
}

// precede creates a directed edge: n -> v (n must complete before v).
func (n *node) precede(v *node) {
	n.successors = append(n.successors, v)
	v.dependents = append(v.dependents, n)
}

// hasCondPredecessor reports whether any predecessor is a condition node.
func (n *node) hasCondPredecessor() bool {
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			return true
		}
	}
	return false
}
